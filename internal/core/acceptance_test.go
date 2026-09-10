package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"pi-popchat/internal/agent/pi"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pi-popchat/internal/agent"
)

// These scenarios drive public Engine actions and independently constructed RPC
// sequences. The fake does not reproduce Engine scheduling or persistence logic.
type acceptanceFactory struct {
	history []any
	starts  chan *acceptanceClient
	gate    <-chan struct{}
}
type acceptanceClient struct {
	modelRequests atomic.Int32
	modelError    atomic.Bool
	history       []any
	cfg           agent.Config
	events        chan map[string]any
	calls         chan map[string]any
	once          sync.Once
}

func (f *acceptanceFactory) Start(ctx context.Context, cfg agent.Config) (agent.Client, error) {
	c := &acceptanceClient{history: f.history, cfg: cfg, events: make(chan map[string]any, 256), calls: make(chan map[string]any, 256)}
	f.starts <- c
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c, nil
}
func (c *acceptanceClient) Request(ctx context.Context, cmd map[string]any) (map[string]any, error) {
	typ := cmd["type"]
	data := map[string]any{}
	switch typ {
	case "get_state":
		data = map[string]any{"sessionFile": filepath.Join(c.cfg.SessionDir, "fixture.jsonl"), "model": map[string]any{"id": "test-model", "provider": "test"}}
	case "get_available_models":
		c.modelRequests.Add(1)
		if c.modelError.Load() {
			return nil, errors.New("model discovery unavailable")
		}
		data = map[string]any{"models": []any{map[string]any{"id": "test-model", "provider": "test", "name": "Test"}}}
	case "get_messages":
		data = map[string]any{"messages": c.history}
	case "get_commands":
		data = map[string]any{"commands": []any{map[string]any{"name": "test-command", "description": "test", "source": "extension"}}}
	default:
		select {
		case c.calls <- cmd:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return map[string]any{"success": true, "data": data}, nil
}
func (c *acceptanceClient) Events() <-chan map[string]any { return c.events }
func (c *acceptanceClient) Close() error                  { c.once.Do(func() { close(c.events) }); return nil }
func (c *acceptanceClient) emit(typ string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["type"] = typ
	c.events <- fields
}
func acceptanceEngine(t *testing.T) (*Engine, *acceptanceFactory) {
	t.Helper()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &acceptanceFactory{starts: make(chan *acceptanceClient, 32)}
	e, err := New(s, f, "test-pi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e, f
}
func acceptanceEventually(t *testing.T, fn func() bool, why string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(why)
}
func acceptanceStart(t *testing.T, e *Engine, f *acceptanceFactory, view string) (string, *acceptanceClient) {
	t.Helper()
	sid, err := e.NewSession(view, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Send(sid, "first "+view, "first-"+sid, nil); err != nil {
		t.Fatal(err)
	}
	var c *acceptanceClient
	select {
	case c = <-f.starts:
	case <-time.After(3 * time.Second):
		t.Fatal("no agent process")
	}
	acceptanceCall(t, c, "prompt")
	c.emit("agent_start", nil)
	acceptanceEventually(t, func() bool { return e.Snapshot(view).Current.Status == "running" }, "agent did not become running")
	return sid, c
}
func acceptanceCall(t *testing.T, c *acceptanceClient, typ string) map[string]any {
	t.Helper()
	select {
	case cmd := <-c.calls:
		if cmd["type"] != typ {
			t.Fatalf("expected %s, got %v", typ, cmd)
		}
		return cmd
	case <-time.After(3 * time.Second):
		t.Fatalf("no %s delivery", typ)
	}
	return nil
}
func acceptanceNoCall(t *testing.T, c *acceptanceClient) {
	t.Helper()
	select {
	case cmd := <-c.calls:
		t.Fatalf("unexpected agent delivery: %v", cmd)
	case <-time.After(80 * time.Millisecond):
	}
}
func acceptanceText(c *acceptanceClient, text, reason string) {
	c.emit("message_start", map[string]any{"message": map[string]any{"role": "assistant", "content": []any{}}})
	c.emit("message_update", map[string]any{"assistantMessageEvent": map[string]any{"type": "text_delta", "delta": text}})
	c.emit("message_end", map[string]any{"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}, "stopReason": reason, "errorMessage": "synthetic model failure"}})
}

func TestAcceptancePanelTimeoutAndIndependentSelection(t *testing.T) {
	e, f := acceptanceEngine(t)
	if err := e.PanelShown(); err != nil {
		t.Fatal(err)
	}
	panel, client := acceptanceStart(t, e, f, "panel")
	client.emit("agent_settled", nil)
	acceptanceEventually(t, func() bool { return e.Snapshot("panel").Current.Status == "idle" }, "panel did not settle")
	main, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	if e.CurrentID("panel") != panel || e.CurrentID("main") != main {
		t.Fatal("main selection changed panel")
	}
	e.PanelHidden()
	if err = e.PanelShown(); err != nil {
		t.Fatal(err)
	}
	if e.CurrentID("panel") != panel {
		t.Fatal("unexpired panel replaced")
	}
	e.mu.Lock()
	e.hiddenAt = time.Now().Add(-31 * time.Minute)
	e.mu.Unlock()
	if err = e.PanelShown(); err != nil {
		t.Fatal(err)
	}
	if e.CurrentID("panel") == panel {
		t.Fatal("expired panel not replaced")
	}
	if len(e.Snapshot("main").Sessions) != 1 {
		t.Fatal("expired history disappeared")
	}
	if err = e.Transfer(); err != nil {
		t.Fatal(err)
	}
	if e.CurrentID("main") != e.CurrentID("panel") {
		t.Fatal("transfer copied instead of selecting same session")
	}
	for _, status := range []string{"running", "waiting"} {
		e.mu.Lock()
		e.hiddenAt = time.Now().Add(-time.Hour)
		e.sessions[e.selected["panel"]].Status = status
		e.mu.Unlock()
		previous := e.CurrentID("panel")
		if err = e.PanelShown(); err != nil {
			t.Fatal(err)
		}
		if e.CurrentID("panel") != previous {
			t.Fatalf("expired %s task replaced", status)
		}
	}
}
func TestAcceptanceConcurrentSessionsQueueAndPromotion(t *testing.T) {
	e, f := acceptanceEngine(t)
	a, ca := acceptanceStart(t, e, f, "panel")
	b, cb := acceptanceStart(t, e, f, "main")
	if e.ActiveCount() != 2 {
		t.Fatal("concurrent task count is wrong")
	}
	if err := e.Send(a, "promoted", "q1", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.Send(a, "remaining", "q2", nil); err != nil {
		t.Fatal(err)
	}
	acceptanceNoCall(t, ca)
	if err := e.QueueAction(a, "insert", "q1"); err != nil {
		t.Fatal(err)
	}
	if cmd := acceptanceCall(t, ca, "prompt"); cmd["message"] != "promoted" || cmd["streamingBehavior"] != "steer" {
		t.Fatal(cmd)
	}
	acceptanceText(cb, "reply B", "stop")
	cb.emit("agent_settled", nil)
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "idle" }, "session B did not finish")
	if e.Snapshot("panel").Current.ID != a || e.Snapshot("main").Current.ID != b {
		t.Fatal("selection mixed")
	}
	acceptanceText(ca, "reply A", "stop")
	ca.emit("agent_settled", nil)
	if cmd := acceptanceCall(t, ca, "prompt"); cmd["message"] != "remaining" {
		t.Fatal(cmd)
	}
	if err := e.Send(a, "remaining", "q2", nil); err != nil {
		t.Fatal(err)
	}
	acceptanceNoCall(t, ca)
	aSnap, bSnap := e.Snapshot("panel").Current, e.Snapshot("main").Current
	for _, m := range aSnap.Messages {
		if m.Text == "reply B" {
			t.Fatal("cross-session response leak")
		}
	}
	for _, m := range bSnap.Messages {
		if m.Text == "reply A" {
			t.Fatal("cross-session response leak")
		}
	}
}
func TestAcceptanceInteractionRoutingAndStop(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, c := acceptanceStart(t, e, f, "main")
	c.emit("extension_ui_request", map[string]any{"id": "dialog-1", "method": "confirm", "title": "Confirm", "message": "Continue?"})
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "waiting" }, "no interaction state")
	if err := e.Respond(sid, "wrong", true, false); err == nil {
		t.Fatal("accepted stale interaction id")
	}
	if err := e.Respond(sid, "dialog-1", true, false); err != nil {
		t.Fatal(err)
	}
	if cmd := acceptanceCall(t, c, "extension_ui_response"); cmd["id"] != "dialog-1" || cmd["confirmed"] != true {
		t.Fatal(cmd)
	}
	if err := e.Send(sid, "wait for next", "queued", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.Stop(sid); err != nil {
		t.Fatal(err)
	}
	acceptanceCall(t, c, "abort")
	c.emit("agent_settled", nil)
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "stopped" }, "stopped task marked complete")
	if !e.Snapshot("main").Current.QueuePaused || e.ActiveCount() != 0 {
		t.Fatal("stop did not pause queue")
	}
	acceptanceNoCall(t, c)
}
func TestAcceptanceRetryAndFailureNotifications(t *testing.T) {
	e, f := acceptanceEngine(t)
	notifications := make(chan string, 10)
	e.Notify = func(_ string, title, _ string) { notifications <- title }
	_, c := acceptanceStart(t, e, f, "main")
	acceptanceText(c, "", "error")
	c.emit("agent_end", nil)
	c.emit("auto_retry_start", nil)
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "retrying" }, "retry not displayed")
	select {
	case n := <-notifications:
		t.Fatalf("premature completion notification: %s", n)
	default:
	}
	c.emit("auto_retry_end", map[string]any{"success": true})
	acceptanceText(c, "recovered", "stop")
	c.emit("agent_settled", nil)
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "idle" }, "successful retry stayed failed")
	select {
	case n := <-notifications:
		if n != "回复已完成" {
			t.Fatal(n)
		}
	case <-time.After(time.Second):
		t.Fatal("missing final notification")
	}
	c.emit("agent_start", nil)
	acceptanceText(c, "", "error")
	c.emit("agent_settled", nil)
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "failed" }, "final model error marked success")
	select {
	case n := <-notifications:
		if n == "回复已完成" {
			t.Fatal("failed model notified success")
		}
	case <-time.After(time.Second):
		t.Fatal("missing failure notification")
	}
}
func TestAcceptanceHistoryCrashRecoveryAndDraftCAS(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, c := acceptanceStart(t, e, f, "main")
	if err := e.Rename(sid, "Needle title"); err != nil {
		t.Fatal(err)
	}
	if err := e.Pin(sid, true); err != nil {
		t.Fatal(err)
	}
	original := ""
	results := make(chan error, 2)
	go func() { results <- e.Draft(sid, "draft A", &original) }()
	go func() { results <- e.Draft(sid, "draft B", &original) }()
	successes := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("draft CAS accepted %d writers", successes)
	}
	if err := e.Send(sid, "never auto resend", "queued", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.Delete(sid); err == nil {
		t.Fatal("deleted running session")
	}
	c.emit("process_exit", map[string]any{"expected": false})
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "interrupted" }, "crash not marked interrupted")
	s := e.Snapshot("main").Current
	if !s.QueuePaused || !s.Pinned || !strings.Contains(s.SearchableText, "Needle title") {
		t.Fatal("history metadata or queue lost")
	}
	root := e.Root()
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	nextFactory := &acceptanceFactory{starts: make(chan *acceptanceClient, 10)}
	restored, err := New(store, nextFactory, "test-pi")
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restoredState := restored.Snapshot("main").Current
	if restoredState.ID != sid || !restoredState.QueuePaused || len(restoredState.Queue) != 1 || restoredState.Queue[0].Status != "paused" {
		t.Fatal("restart lost queue or auto-resumed")
	}
	select {
	case <-nextFactory.starts:
		t.Fatal("restart automatically started execution")
	default:
	}
	if err = restored.Refresh(sid); err != nil {
		t.Fatal(err)
	}
	resumedClient := <-nextFactory.starts
	if resumedClient.cfg.SessionFile != s.SessionFile || resumedClient.cfg.CWD != s.CWD {
		t.Fatal("resume discarded Pi session file or working directory")
	}
	acceptanceNoCall(t, resumedClient)
	if err = restored.Delete(sid); err != nil {
		t.Fatal(err)
	}
	if len(restored.Snapshot("main").Sessions) != 0 {
		t.Fatal("deleted session still indexed")
	}
}
func TestAcceptanceStopDuringAgentStartupPreventsPrompt(t *testing.T) {
	e, f := acceptanceEngine(t)
	gate := make(chan struct{})
	f.gate = gate
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Send(sid, "side effect must not happen", "stop-before-start", nil); err != nil {
		t.Fatal(err)
	}
	c := <-f.starts
	if err = e.Stop(sid); err != nil {
		t.Fatal(err)
	}
	close(gate)
	acceptanceNoCall(t, c)
}
func TestAcceptanceFailedPersistenceAllowsSameMessageRetry(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.store.db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	err = e.Send(sid, "must deliver after retry", "persistent-id", nil)
	if err == nil {
		t.Fatal("readonly database did not reject send")
	}
	if _, err = e.store.db.Exec("PRAGMA query_only=OFF"); err != nil {
		t.Fatal(err)
	}
	if err = e.Send(sid, "must deliver after retry", "persistent-id", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-f.starts:
		acceptanceCall(t, c, "prompt")
	case <-time.After(time.Second):
		t.Fatal("retry acknowledged but never delivered after failed persistence")
	}
}
func TestAcceptanceQueueDispatchRequiresDurableSave(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, c := acceptanceStart(t, e, f, "main")
	if err := e.Send(sid, "not durable yet", "queued", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	defer e.store.db.Exec("PRAGMA query_only=OFF")
	c.emit("agent_settled", nil)
	acceptanceNoCall(t, c)
}
func TestAcceptanceUnknownQueueActionDoesNotDeliver(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, c := acceptanceStart(t, e, f, "main")
	if err := e.Send(sid, "queued", "queued", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.QueueAction(sid, "typoAction", "queued"); err == nil {
		t.Fatal("unknown action accepted")
	}
	acceptanceNoCall(t, c)
}

var _ agent.Factory = (*acceptanceFactory)(nil)

// Hold the first settled event after the real Pi has finished, so the UI still
// considers it running. This creates the exact promote-versus-settled race.
type acceptanceRealFactory struct{ started chan *acceptanceRealClient }
type acceptanceRealClient struct {
	agent.Client
	forwarded        chan map[string]any
	settledReached   chan struct{}
	releaseSettled   chan struct{}
	promotedAccepted chan struct{}
}

func (f *acceptanceRealFactory) Start(ctx context.Context, cfg agent.Config) (agent.Client, error) {
	cfg.ExtraArgs = append(cfg.ExtraArgs, "--offline", "--no-tools", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-context-files", "--thinking", "off")
	underlying, err := (pi.Factory{}).Start(ctx, cfg)
	if err != nil {
		return nil, err
	}
	c := &acceptanceRealClient{Client: underlying, forwarded: make(chan map[string]any, 256), settledReached: make(chan struct{}), releaseSettled: make(chan struct{}), promotedAccepted: make(chan struct{}, 1)}
	go func() {
		defer close(c.forwarded)
		held := false
		for ev := range underlying.Events() {
			if ev["type"] == "agent_settled" && !held {
				held = true
				close(c.settledReached)
				<-c.releaseSettled
			}
			c.forwarded <- ev
		}
	}()
	f.started <- c
	return c, nil
}
func (c *acceptanceRealClient) Events() <-chan map[string]any { return c.forwarded }
func (c *acceptanceRealClient) Request(ctx context.Context, cmd map[string]any) (map[string]any, error) {
	r, err := c.Client.Request(ctx, cmd)
	if cmd["streamingBehavior"] == "steer" {
		select {
		case c.promotedAccepted <- struct{}{}:
		default:
		}
	}
	return r, err
}
func TestAcceptanceRealPiPromotionAfterActualSettlement(t *testing.T) {
	if os.Getenv("PI_POPCHAT_REAL_TEST") != "1" {
		t.Skip("explicit real model opt-in required")
	}
	executable, err := pi.Locate()
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	factory := &acceptanceRealFactory{started: make(chan *acceptanceRealClient, 1)}
	e, err := New(store, factory, executable)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Send(sid, "Reply only with READY.", "initial", nil); err != nil {
		t.Fatal(err)
	}
	c := <-factory.started
	var release sync.Once
	defer release.Do(func() { close(c.releaseSettled) })
	select {
	case <-c.settledReached:
	case <-time.After(90 * time.Second):
		t.Fatal("real Pi did not settle")
	}
	marker := "PROMOTION_VERIFIED_728419"
	if err = e.Send(sid, "Reply only with "+marker, "promoted", nil); err != nil {
		t.Fatal(err)
	}
	if err = e.QueueAction(sid, "insert", "promoted"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.promotedAccepted:
	case <-time.After(20 * time.Second):
		t.Fatal("promotion was not atomically handed to Pi")
	}
	release.Do(func() { close(c.releaseSettled) })
	deadline := time.Now().Add(90 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		snapshot := e.Snapshot("main").Current
		for _, m := range snapshot.Messages {
			if m.Role == "assistant" && strings.Contains(m.Text, marker) && m.Status == "complete" {
				found = true
			}
		}
		if found && snapshot.Status == "idle" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Fatal("promotion stranded after underlying Pi became idle")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := c.Client.Request(ctx, map[string]any{"type": "get_messages"})
	if err != nil {
		t.Fatal(err)
	}
	data := obj(r["data"])
	users := 0
	for _, raw := range anySlice(data["messages"]) {
		m := obj(raw)
		b, _ := json.Marshal(m["content"])
		if m["role"] == "user" && strings.Contains(string(b), marker) {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("promotion appeared %d times in real Pi history", users)
	}
	t.Log("PASS: actual Pi settled before promotion, app still running; promoted request executed exactly once and streamed to the correct session")
}

type acceptanceFactoryFunc func(context.Context, agent.Config) (agent.Client, error)

func (f acceptanceFactoryFunc) Start(ctx context.Context, cfg agent.Config) (agent.Client, error) {
	return f(ctx, cfg)
}
func TestAcceptanceRealPiUIOnlyCommandBecomesIdle(t *testing.T) {
	if os.Getenv("PI_POPCHAT_REAL_TEST") != "1" {
		t.Skip("explicit installed Pi opt-in required")
	}
	executable, err := pi.Locate()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	extension := filepath.Join(root, "ui-only.mjs")
	source := `export default function(pi){pi.registerCommand("popchat-ui-only",{description:"test",handler:async(_,ctx)=>{await ctx.ui.confirm("Only a dialog","Continue?");}});}`
	if err = os.WriteFile(extension, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(root, "app"))
	if err != nil {
		t.Fatal(err)
	}
	factory := acceptanceFactoryFunc(func(ctx context.Context, cfg agent.Config) (agent.Client, error) {
		cfg.ExtraArgs = []string{"--offline", "--no-tools", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-context-files", "-e", extension}
		return (pi.Factory{}).Start(ctx, cfg)
	})
	e, err := New(store, factory, executable)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Send(sid, "/popchat-ui-only", "ui-command", nil); err != nil {
		t.Fatal(err)
	}
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Interaction != nil }, "real UI-only command did not request confirmation")
	interaction := e.Snapshot("main").Current.Interaction
	if err = e.Respond(sid, interaction.ID, true, false); err != nil {
		t.Fatal(err)
	}
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "idle" }, "pure extension command remained busy without agent_settled")
	if e.ActiveCount() != 0 {
		t.Fatal("UI-only command still counted as running")
	}
	t.Log("PASS: real Pi command without an agent loop returns to idle after confirmation")
}

// Observe the real Pi tool event without giving core any synthetic execution.
type acceptanceObservedClient struct {
	agent.Client
	forwarded chan map[string]any
	readSeen  *atomic.Bool
}

func acceptanceObserve(c agent.Client, seen *atomic.Bool) agent.Client {
	wrapped := &acceptanceObservedClient{Client: c, forwarded: make(chan map[string]any, 256), readSeen: seen}
	go func() {
		defer close(wrapped.forwarded)
		for ev := range c.Events() {
			if ev["type"] == "tool_execution_end" && ev["toolName"] == "read" && ev["isError"] != true {
				seen.Store(true)
			}
			wrapped.forwarded <- ev
		}
	}()
	return wrapped
}
func (c *acceptanceObservedClient) Events() <-chan map[string]any { return c.forwarded }
func acceptanceAwaitRealAnswer(t *testing.T, e *Engine, expected string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		s := e.Snapshot("main").Current
		if s.Status == "failed" || s.Status == "interrupted" {
			t.Fatalf("real execution failed: %s", s.Error)
		}
		if s.Status == "idle" {
			for _, m := range s.Messages {
				if m.Role == "assistant" && strings.Contains(strings.ToUpper(m.Text), strings.ToUpper(expected)) {
					return
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("real model did not produce expected completed answer")
}
func TestAcceptanceRealPiManagedTextAttachment(t *testing.T) {
	if os.Getenv("PI_POPCHAT_REAL_TEST") != "1" {
		t.Skip("explicit real model/tool opt-in required")
	}
	executable, err := pi.Locate()
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var readSeen atomic.Bool
	factory := acceptanceFactoryFunc(func(ctx context.Context, cfg agent.Config) (agent.Client, error) {
		cfg.ExtraArgs = []string{"--offline", "--tools", "read", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-context-files", "--thinking", "off"}
		c, err := (pi.Factory{}).Start(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return acceptanceObserve(c, &readSeen), nil
	})
	e, err := New(store, factory, executable)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	marker := "ATTACHMENT_FILE_CONTENT_584032791"
	a, err := e.SaveAttachment("proof.txt", "text/plain", base64.StdEncoding.EncodeToString([]byte(marker)))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Send(sid, "Use the read tool to read the attached text file. Reply only with its exact complete contents.", "attachment-proof", []Attachment{a}); err != nil {
		t.Fatal(err)
	}
	acceptanceAwaitRealAnswer(t, e, marker)
	if !readSeen.Load() {
		t.Fatal("model answer did not include a verified real Pi read-tool execution")
	}
	t.Log("PASS: SaveAttachment -> managed file path -> real Pi read tool -> real model returns unique file contents")
}
func TestAcceptanceRealPiManagedImageAttachment(t *testing.T) {
	if os.Getenv("PI_POPCHAT_REAL_TEST") != "1" {
		t.Skip("explicit real vision-model opt-in required")
	}
	executable, err := pi.Locate()
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var client agent.Client
	factory := acceptanceFactoryFunc(func(ctx context.Context, cfg agent.Config) (agent.Client, error) {
		cfg.ExtraArgs = []string{"--offline", "--no-tools", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-context-files", "--thinking", "off"}
		c, err := (pi.Factory{}).Start(ctx, cfg)
		client = c
		return c, err
	})
	e, err := New(store, factory, executable)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Refresh(sid); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	state, err := client.Request(ctx, map[string]any{"type": "get_state"})
	if err != nil {
		t.Fatal(err)
	}
	model := obj(obj(state["data"])["model"])
	inputs := anySlice(model["input"])
	supportsImage := false
	for _, v := range inputs {
		if v == "image" {
			supportsImage = true
		}
	}
	if !supportsImage {
		t.Skip("configured Pi model does not advertise image input; image-provider acceptance is NOT verified")
	}
	img := image.NewRGBA(image.Rect(0, 0, 48, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			img.Set(x, y, color.RGBA{R: 245, G: 5, B: 5, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err = png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	a, err := e.SaveAttachment("color.png", "image/png", base64.StdEncoding.EncodeToString(buffer.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if a.MIME != "image/png" || a.Preview == "" {
		t.Fatal("managed PNG was not classified for native image input")
	}
	if err = e.Send(sid, "What is the dominant color in the attached image? Reply with exactly one of RED, GREEN, BLUE. Do not use tools.", "image-proof", []Attachment{a}); err != nil {
		t.Fatal(err)
	}
	acceptanceAwaitRealAnswer(t, e, "RED")
	t.Log("PASS: synthesized PNG -> SaveAttachment -> native Pi images field -> actual vision model recognized RED")
}
func TestAcceptanceRealPiSelectInputEditorThroughEngine(t *testing.T) {
	if os.Getenv("PI_POPCHAT_REAL_TEST") != "1" {
		t.Skip("explicit installed Pi opt-in required")
	}
	executable, err := pi.Locate()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	extension := filepath.Join(root, "all-dialogs.mjs")
	resultPath := filepath.Join(root, "answers.json")
	quoted, _ := json.Marshal(resultPath)
	source := `import {writeFileSync} from 'node:fs'; export default function(pi){pi.registerCommand("popchat-all-dialogs",{description:"test",handler:async(_,ctx)=>{const s=await ctx.ui.select("Select",["A","B"]);const i=await ctx.ui.input("Input","type here");const e=await ctx.ui.editor("Editor","initial");writeFileSync(` + string(quoted) + `,JSON.stringify({s,i,e}));}});}`
	if err = os.WriteFile(extension, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(root, "app"))
	if err != nil {
		t.Fatal(err)
	}
	factory := acceptanceFactoryFunc(func(ctx context.Context, cfg agent.Config) (agent.Client, error) {
		cfg.ExtraArgs = []string{"--offline", "--no-tools", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-context-files", "-e", extension}
		return (pi.Factory{}).Start(ctx, cfg)
	})
	e, err := New(store, factory, executable)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Send(sid, "/popchat-all-dialogs", "all-dialogs", nil); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct{ method, value string }{{"select", "B"}, {"input", "typed"}, {"editor", "edited\nsecond line"}} {
		acceptanceEventually(t, func() bool {
			s := e.Snapshot("main").Current
			return s.Interaction != nil && s.Interaction.Method == step.method
		}, "missing real "+step.method+" interaction")
		interaction := e.Snapshot("main").Current.Interaction
		if step.method == "select" && strings.Join(interaction.Options, ",") != "A,B" {
			t.Fatal("select options corrupted")
		}
		if err = e.Respond(sid, interaction.ID, step.value, false); err != nil {
			t.Fatal(err)
		}
	}
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "idle" }, "all-dialog command stayed busy")
	b, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"s":"B","i":"typed","e":"edited\nsecond line"}` {
		t.Fatalf("Pi extension received incorrect responses: %s", b)
	}
	t.Log("PASS: real select/input/editor requests rendered as core interactions and Engine.Respond delivered exact values, including multiline text")
}

func TestAcceptanceManagedAttachmentContainment(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := e.SaveAttachment("sample.txt", "text/plain", base64.StdEncoding.EncodeToString([]byte("sample")))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Send(sid, "read this", "attachment-valid", []Attachment{a}); err != nil {
		t.Fatalf("application rejected its own managed attachment: %v", err)
	}
	c := <-f.starts
	cmd := acceptanceCall(t, c, "prompt")
	if !strings.Contains(str(cmd["message"]), a.Path) {
		t.Fatal("file path was not handed to Agent")
	}
	external := filepath.Join(t.TempDir(), "external.txt")
	if err = os.WriteFile(external, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	forged := a
	forged.Path = external
	if err = e.Send(sid, "reject external", "external", []Attachment{forged}); err == nil {
		t.Fatal("unmanaged path accepted as saved attachment")
	}
	link := filepath.Join(filepath.Dir(a.Path), "outside-link.txt")
	if err = os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	forged.Path = link
	if err = e.Send(sid, "reject escape", "escape", []Attachment{forged}); err == nil {
		t.Fatal("attachment symlink escape accepted")
	}
}

func TestAcceptanceHistoryProjectionPreservesUICommandsAndMessageIdentity(t *testing.T) {
	root := t.TempDir()
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	created := func(ms int64) string { return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano) }
	session := &Session{ID: "history-fixture", Title: "History", CWD: root, CreatedAt: created(1), UpdatedAt: created(6000), Status: "idle", Messages: []Message{
		{ID: "user-one", Role: "user", Text: "first question", AgentKey: "user:1000", Status: "complete", CreatedAt: created(1000)},
		{ID: "answer-one", Role: "assistant", Text: "first answer", AgentKey: "assistant:2000", Status: "complete", CreatedAt: created(2000)},
		{ID: "ui-only", Role: "user", Text: "/local-ui-only", Status: "complete", CreatedAt: created(2500)},
		{ID: "user-two", Role: "user", Text: "second question", AgentKey: "user:3000", Status: "complete", CreatedAt: created(3000)},
		{ID: "answer-two", Role: "assistant", Text: "partial", AgentKey: "assistant:4000", Status: "uncertain", CreatedAt: created(4000)},
		{ID: "uncertain", Role: "user", Text: "possibly never accepted", Status: "uncertain", CreatedAt: created(5000)},
	}}
	if err = store.Save(session); err != nil {
		t.Fatal(err)
	}
	raw := func(role, text string, ms float64) map[string]any {
		return map[string]any{"role": role, "timestamp": ms, "content": []any{map[string]any{"type": "text", "text": text}}}
	}
	history := []any{raw("user", "first question", 1000), raw("assistant", "first answer", 2000), raw("user", "second question", 3000), raw("toolResult", "hidden tool output", 3500), raw("assistant", "complete second answer", 4000)}
	f := &acceptanceFactory{starts: make(chan *acceptanceClient, 10), history: history}
	e, err := New(store, f, "test-pi")
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.Select("main", session.ID); err != nil {
		t.Fatal(err)
	}
	verify := func(engine *Engine) {
		t.Helper()
		s := engine.Snapshot("main").Current
		want := map[string]string{"user-one": "first question", "answer-one": "first answer", "ui-only": "/local-ui-only", "user-two": "second question", "answer-two": "complete second answer", "uncertain": "possibly never accepted"}
		if len(s.Messages) != len(want) {
			t.Fatalf("projection duplicated/dropped messages: %v", s.Messages)
		}
		for _, m := range s.Messages {
			if want[m.ID] != m.Text {
				t.Fatalf("message identity changed: %s => %q", m.ID, m.Text)
			}
			delete(want, m.ID)
		}
		if len(want) != 0 {
			t.Fatalf("missing original ids: %v", want)
		}
	}
	if err = e.Refresh(session.ID); err != nil {
		t.Fatal(err)
	}
	verify(e)
	e.ReapIdle(-time.Second)
	if err = e.Refresh(session.ID); err != nil {
		t.Fatal(err)
	}
	verify(e)
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := New(reopened, f, "test-pi")
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if err = restarted.Refresh(session.ID); err != nil {
		t.Fatal(err)
	}
	verify(restarted)
}
func TestAcceptanceStaleStartupClosedAfterDeleteOrDirectoryChange(t *testing.T) {
	for _, action := range []string{"delete", "cwd"} {
		t.Run(action, func(t *testing.T) {
			e, f := acceptanceEngine(t)
			gate := make(chan struct{})
			f.gate = gate
			sid, err := e.NewSession("main", "")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- e.Refresh(sid) }()
			old := <-f.starts
			newCWD := t.TempDir()
			if action == "delete" {
				err = e.Delete(sid)
			} else {
				err = e.SetCWD(sid, newCWD)
			}
			if err != nil {
				t.Fatal(err)
			}
			close(gate)
			select {
			case err = <-done:
				if err == nil {
					t.Fatal("stale startup accepted")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("stale startup stuck")
			}
			select {
			case _, open := <-old.Events():
				if open {
					t.Fatal("stale process not closed")
				}
			case <-time.After(time.Second):
				t.Fatal("stale process leaked")
			}
			if action == "delete" {
				if len(e.Snapshot("main").Sessions) != 0 {
					t.Fatal("deleted session resurrected")
				}
			} else {
				if e.Snapshot("main").Current.CWD != newCWD {
					t.Fatal("stale startup restored old directory")
				}
				if err = e.Refresh(sid); err != nil {
					t.Fatal(err)
				}
				current := <-f.starts
				if current.cfg.CWD != newCWD {
					t.Fatal("new process used old directory")
				}
			}
		})
	}
}

func TestAcceptanceRealPiChatUICommandRestartAndContinue(t *testing.T) {
	if os.Getenv("PI_POPCHAT_REAL_TEST") != "1" {
		t.Skip("explicit real model opt-in required")
	}
	executable, err := pi.Locate()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	extension := filepath.Join(root, "history-ui.mjs")
	source := `export default function(pi){pi.registerCommand("popchat-history-ui",{description:"test",handler:async(_,ctx)=>{await ctx.ui.confirm("History check","Continue?");}});}`
	if err = os.WriteFile(extension, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	factory := acceptanceFactoryFunc(func(ctx context.Context, cfg agent.Config) (agent.Client, error) {
		cfg.ExtraArgs = []string{"--offline", "--no-tools", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-context-files", "--thinking", "off", "-e", extension}
		return (pi.Factory{}).Start(ctx, cfg)
	})
	store, err := OpenStore(filepath.Join(root, "app"))
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(store, factory, executable)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	marker := "HISTORY_RETAINED_438201"
	if err = e.Send(sid, "Remember the code "+marker+". Reply only with the code.", "normal-first", nil); err != nil {
		t.Fatal(err)
	}
	acceptanceAwaitRealAnswer(t, e, marker)
	if err = e.Send(sid, "/popchat-history-ui", "ui-history", nil); err != nil {
		t.Fatal(err)
	}
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Interaction != nil }, "UI command did not show confirmation")
	interaction := e.Snapshot("main").Current.Interaction
	if err = e.Respond(sid, interaction.ID, true, false); err != nil {
		t.Fatal(err)
	}
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "idle" }, "UI command stayed busy")
	if err = e.Send(sid, "What code did I ask you to remember? Reply only with the code.", "normal-second", nil); err != nil {
		t.Fatal(err)
	}
	acceptanceAwaitRealAnswer(t, e, marker)
	before := e.Snapshot("main").Current
	if len(before.Messages) != 5 {
		t.Fatalf("expected 2 user + 2 assistant + 1 UI-only command, got %d", len(before.Messages))
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(filepath.Join(root, "app"))
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := New(reopened, factory, executable)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if err = resumed.Refresh(sid); err != nil {
		t.Fatal(err)
	}
	after := resumed.Snapshot("main").Current
	if len(after.Messages) != len(before.Messages) {
		t.Fatalf("restart changed message count %d -> %d", len(before.Messages), len(after.Messages))
	}
	expected := map[string]string{}
	for _, m := range before.Messages {
		expected[m.ID] = m.Text
	}
	for _, m := range after.Messages {
		if expected[m.ID] != m.Text {
			t.Fatalf("restart replaced message %s: %q", m.ID, m.Text)
		}
		delete(expected, m.ID)
	}
	if len(expected) != 0 {
		t.Fatal("restart lost original message ids")
	}
	if err = resumed.Send(sid, "Repeat the same remembered code once more. Reply with only the code.", "normal-third", nil); err != nil {
		t.Fatal(err)
	}
	acceptanceAwaitRealAnswer(t, resumed, marker)
	final := resumed.Snapshot("main").Current
	if len(final.Messages) != 7 {
		t.Fatalf("continuation duplicated or dropped messages: %d", len(final.Messages))
	}
	uiCount := 0
	for _, m := range final.Messages {
		if m.ID == "ui-history" && m.Text == "/popchat-history-ui" {
			uiCount++
		}
	}
	if uiCount != 1 {
		t.Fatal("UI-only command not retained exactly once")
	}
	t.Log("PASS: real chat -> UI-only command -> real follow-up -> process/app restart -> real continuation; original IDs/text preserved and UI command appears once")
}

func TestAcceptanceStoppedTaskRejectsLateRetryAndDialogs(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, c := acceptanceStart(t, e, f, "main")
	if err := e.Stop(sid); err != nil {
		t.Fatal(err)
	}
	acceptanceCall(t, c, "abort")
	c.emit("auto_retry_start", nil)
	c.emit("extension_ui_request", map[string]any{"method": "set_editor_text", "text": "retry event processed"})
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Draft == "retry event processed" }, "event consumer stalled")
	if e.Snapshot("main").Current.Status != "stopped" {
		t.Fatal("late retry resurrected stopped task")
	}
	for _, method := range []string{"confirm", "select", "input", "editor"} {
		c.emit("extension_ui_request", map[string]any{"id": "late-" + method, "method": method, "title": "late"})
		response := acceptanceCall(t, c, "extension_ui_response")
		if response["id"] != "late-"+method || response["cancelled"] != true {
			t.Fatalf("late %s was not cancelled: %v", method, response)
		}
		s := e.Snapshot("main").Current
		if s.Status != "stopped" || s.Interaction != nil || e.ActiveCount() != 0 {
			t.Fatalf("late %s resurrected interaction", method)
		}
	}
}

type acceptanceMissingHistoryClient struct{ agent.Client }

func (c acceptanceMissingHistoryClient) ReadHistory(context.Context) ([]map[string]any, error) {
	return nil, agent.ErrHistoryMissing
}
func TestAcceptanceMissingHistoryIsOnlyAllowedForNewConversation(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	underlying := &acceptanceFactory{starts: make(chan *acceptanceClient, 4)}
	factory := acceptanceFactoryFunc(func(ctx context.Context, cfg agent.Config) (agent.Client, error) {
		c, err := underlying.Start(ctx, cfg)
		return acceptanceMissingHistoryClient{c}, err
	})
	e, err := New(store, factory, "test-pi")
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Send(sid, "new conversation", "first", nil); err != nil {
		t.Fatal(err)
	}
	c := <-underlying.starts
	acceptanceCall(t, c, "prompt")
	c.emit("agent_start", nil)
	c.emit("message_start", map[string]any{"message": map[string]any{"role": "user", "timestamp": float64(1000), "content": "new conversation"}})
	acceptanceText(c, "persisted answer", "stop")
	c.emit("agent_settled", nil)
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "idle" }, "initial conversation not complete")
	e.ReapIdle(-time.Second)
	if err = e.Refresh(sid); !errors.Is(err, agent.ErrHistoryMissing) {
		t.Fatalf("missing historical context silently accepted: %v", err)
	}
	recreated := <-underlying.starts
	acceptanceNoCall(t, recreated)
	if len(e.Snapshot("main").Current.Messages) != 2 {
		t.Fatal("missing file erased visible history")
	}
}

func TestModelsDiscoveryLifecycleAndRefresh(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	if s := e.Snapshot("main").Current; s.ModelsState != "" {
		t.Fatalf("unqueried state=%q", s.ModelsState)
	}
	if err := e.Refresh(sid); err != nil {
		t.Fatal(err)
	}
	c := <-f.starts
	if s := e.Snapshot("main").Current; s.ModelsState != "ready" || len(s.Models) != 1 {
		t.Fatalf("discovery state=%s count=%d", s.ModelsState, len(s.Models))
	}
	before := c.modelRequests.Load()
	if err := e.Refresh(sid); err != nil {
		t.Fatal(err)
	}
	if c.modelRequests.Load() != before+1 {
		t.Fatal("refresh reused process without querying available models")
	}
	if len(f.starts) != 0 {
		t.Fatal("refresh restarted agent")
	}
}

func TestAcceptanceRealPiModelDiscovery(t *testing.T) {
	if os.Getenv("PI_POPCHAT_REAL_TEST") != "1" {
		t.Skip("real Pi opt-in required")
	}
	executable, err := pi.Locate()
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	factory := acceptanceFactoryFunc(func(ctx context.Context, cfg agent.Config) (agent.Client, error) {
		cfg.ExtraArgs = []string{"--offline", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-context-files"}
		return (pi.Factory{}).Start(ctx, cfg)
	})
	e, err := New(store, factory, executable)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = e.Refresh(sid); err != nil {
			t.Fatal(err)
		}
		s := e.Snapshot("main").Current
		if s.ModelsState != "ready" || len(s.Models) == 0 {
			t.Fatalf("round=%d state=%s count=%d", i, s.ModelsState, len(s.Models))
		}
		t.Logf("round=%d available models=%d", i+1, len(s.Models))
	}
}

func TestModelRefreshFailureDoesNotFailConversation(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Refresh(sid); err != nil {
		t.Fatal(err)
	}
	c := <-f.starts
	c.modelError.Store(true)
	before := e.Snapshot("main").Current.Status
	if err = e.Refresh(sid); err == nil {
		t.Fatal("expected discovery error")
	}
	s := e.Snapshot("main").Current
	if s.Status != before || s.ModelsState != "error" {
		t.Fatalf("conversation=%s discovery=%s", s.Status, s.ModelsState)
	}
	c.modelError.Store(false)
	if err = e.Refresh(sid); err != nil {
		t.Fatal(err)
	}
	if e.Snapshot("main").Current.ModelsState != "ready" {
		t.Fatal("retry did not recover")
	}
}

func TestPrepareReusesModelsUntilExplicitRefreshOrReconnect(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.Prepare(sid); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	c := <-f.starts
	if got := c.modelRequests.Load(); got != 1 {
		t.Fatalf("20 window opens queried models %d times, want 1", got)
	}
	before := e.Snapshot("main").Version
	if err = e.Prepare(sid); err != nil {
		t.Fatal(err)
	}
	if e.Snapshot("main").Version != before {
		t.Fatal("cached preparation emitted a metadata/loading update")
	}
	if err = e.Refresh(sid); err != nil {
		t.Fatal(err)
	}
	if got := c.modelRequests.Load(); got != 2 {
		t.Fatalf("manual refresh queries=%d, want 2", got)
	}
	if len(f.starts) != 0 {
		t.Fatal("opening or refreshing restarted Agent")
	}
	e.ReapIdle(-time.Second)
	if err = e.Prepare(sid); err != nil {
		t.Fatal(err)
	}
	next := <-f.starts
	if next.modelRequests.Load() != 1 {
		t.Fatal("new Agent connection must discover its own model catalog")
	}
}
