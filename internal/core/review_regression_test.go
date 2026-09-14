package core

import (
	"context"
	"fmt"
	"pi-popchat/internal/agent"
	"strings"
	"testing"
	"time"
)

func TestReviewToolIDReuseKeepsPreviousTurn(t *testing.T) {
	s := &Session{Messages: []Message{{ID: "old", Role: "user", Steps: []ProcessStep{{ID: "same", Action: "读取文件", Status: "completed", EndedAt: "old time"}}}, {ID: "new", Role: "user"}}}
	recordTool(s, map[string]any{"type": "tool_execution_start", "toolCallId": "same", "toolName": "read", "args": map[string]any{"path": "new"}})
	if s.Messages[0].Steps[0].Status != "completed" || s.Messages[0].Steps[0].EndedAt != "old time" || len(s.Messages[1].Steps) != 1 {
		t.Fatal("tool ID reuse overwrote history")
	}
}

func TestReviewInsertedMessageKeepsActiveToolOwner(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprintf("pending=%v", pending), func(t *testing.T) {
			e, f := acceptanceEngine(t)
			sid, c := acceptanceStart(t, e, f, "main")
			if pending {
				c.emit("message_end", map[string]any{"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "toolCall", "id": "active", "name": "read"}}}})
			} else {
				c.emit("tool_execution_start", map[string]any{"toolCallId": "active", "toolName": "read"})
			}
			acceptanceEventually(t, func() bool { return len(e.Snapshot("main").Current.Messages[0].Steps) == 1 }, "tool did not start")
			if err := e.Send(sid, "synthetic insertion", "inserted", nil); err != nil {
				t.Fatal(err)
			}
			if err := e.QueueAction(sid, "insert", "inserted"); err != nil {
				t.Fatal(err)
			}
			if cmd := acceptanceCall(t, c, "prompt"); cmd["streamingBehavior"] != "steer" {
				t.Fatal(cmd)
			}
			if pending {
				c.emit("tool_execution_start", map[string]any{"toolCallId": "active", "toolName": "read"})
			}
			c.emit("tool_execution_end", map[string]any{"toolCallId": "active", "toolName": "read", "isError": pending})
			c.emit("tool_execution_start", map[string]any{"toolCallId": "new", "toolName": "read"})
			c.emit("tool_execution_end", map[string]any{"toolCallId": "new", "toolName": "read"})
			c.emit("agent_settled", nil)
			acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "idle" }, "agent did not settle")
			s := e.Snapshot("main").Current
			want := "completed"
			if pending {
				want = "failed"
			}
			if steps := s.Messages[0].Steps; len(steps) != 1 || steps[0].Status != want || steps[0].EndedAt == "" {
				t.Fatalf("original tool lost its owner: %+v", steps)
			}
			for _, m := range s.Messages {
				if m.ID == "inserted" && (len(m.Steps) != 1 || m.Steps[0].ID != "new" || m.Steps[0].Status != "completed") {
					t.Fatalf("inserted message must own only the new tool: %+v", m.Steps)
				}
			}
		})
	}
}

type blockedTitleFactory struct {
	titleFactory
	entered chan struct{}
	release chan struct{}
}

func (f *blockedTitleFactory) ReadSessionInfo(ctx context.Context, _ agent.Config) (string, error) {
	select {
	case f.entered <- struct{}{}:
	default:
	}
	select {
	case <-f.release:
		return "old file title", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
func TestReviewTitleReadDoesNotHoldEngineLock(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	f := &blockedTitleFactory{entered: make(chan struct{}, 1), release: make(chan struct{})}
	e, _ := New(store, f, "synthetic")
	sid, _ := e.NewSession("main", "")
	done := make(chan struct{})
	go func() { _ = e.Rename(sid, "title"); close(done) }()
	<-f.entered
	snapshot := make(chan struct{})
	go func() { e.Snapshot("main"); close(snapshot) }()
	blocked := false
	select {
	case <-snapshot:
	case <-time.After(200 * time.Millisecond):
		blocked = true
	}
	close(f.release)
	<-done
	_ = e.Close()
	if blocked {
		t.Fatal("slow title reader blocked snapshot")
	}
}

func TestReviewTitleRestoreDiscardsNewerMetadata(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	f := &blockedTitleFactory{entered: make(chan struct{}, 1), release: make(chan struct{})}
	e, _ := New(store, f, "synthetic")
	defer e.Close()
	sid, _ := e.NewSession("main", "")
	done := make(chan struct{})
	go func() { e.refreshTitleFile(sid, true); close(done) }()
	<-f.entered
	e.mu.Lock()
	e.sessions[sid].titleRevision++
	e.sessions[sid].Title = "new event"
	e.sessions[sid].AgentTitle = "new event"
	e.mu.Unlock()
	close(f.release)
	<-done
	if e.Snapshot("main").Current.Title != "new event" {
		t.Fatal("stale disk result overwrote newer title")
	}
}
func TestReviewTitleStartupDoesNotBlockAndCloseCancelsRead(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	if err := store.Save(&Session{ID: "history", Title: "old", SessionFile: "synthetic", Messages: []Message{{Role: "user", Text: "fallback"}}}); err != nil {
		t.Fatal(err)
	}
	f := &blockedTitleFactory{entered: make(chan struct{}, 1), release: make(chan struct{})}
	created := make(chan *Engine, 1)
	go func() { e, _ := New(store, f, "synthetic"); created <- e }()
	var e *Engine
	select {
	case e = <-created:
	case <-time.After(time.Second):
		close(f.release)
		e = <-created
		_ = e.Close()
		t.Fatal("startup waited for title I/O")
	}
	<-f.entered
	closed := make(chan struct{})
	go func() { _ = e.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		close(f.release)
		<-closed
		t.Fatal("shutdown did not cancel title read")
	}
}
func TestReviewSelectionPromptByteBoundaries(t *testing.T) {
	at := time.Unix(0, 0).UTC()
	cases := []struct {
		template, text string
		valid          bool
	}{
		{"{{text}}", strings.Repeat("a", MaxSelectionPromptBytes), true},
		{"prefix{{text}}", strings.Repeat("a", MaxSelectionPromptBytes), false},
		{"{{text}}", strings.Repeat("汉", 66666), true},
		{"{{text}}", strings.Repeat("汉", 66667), false},
		{"{{text}}{{text}}", strings.Repeat("a", 100001), false},
		{"fixed", strings.Repeat("汉", 200001), true},
		{"{{text}}", "  ", false},
	}
	for _, tc := range cases {
		if ValidSelectionPrompt(RenderSelection(tc.template, tc.text, "zh-Hans", at)) != tc.valid {
			t.Errorf("wrong byte limit, text length %d", len(tc.text))
		}
	}
}
func TestReviewResumedQueueUsesDispatchTime(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, _ := e.NewSession("main", "")
	e.mu.Lock()
	s := e.sessions[sid]
	s.DraftOnly = false
	s.Status = "idle"
	s.QueuePaused = true
	s.Queue = []Message{{ID: "queued", Role: "user", Text: "synthetic", CreatedAt: "2000-01-01T00:00:00Z", Status: "paused"}}
	_ = e.saveLocked(s)
	e.mu.Unlock()
	if err := e.QueueAction(sid, "resumeQueue", ""); err != nil {
		t.Fatal(err)
	}
	_ = f
	acceptanceEventually(t, func() bool {
		m := e.Snapshot("main").Current.Messages
		return len(m) > 0 && m[0].DeliveryStartedAt != ""
	}, "missing dispatch time")
	m := e.Snapshot("main").Current.Messages[0]
	if m.CreatedAt != "2000-01-01T00:00:00Z" || m.DeliveryStartedAt == m.CreatedAt {
		t.Fatal("submission and dispatch times conflated")
	}
}

func TestReviewSelectionExpansionStopsAtByteLimit(t *testing.T) {
	text := strings.Repeat("x", MaxSelectionPromptBytes)
	if got := RenderSelection(strings.Repeat("{{text}}", 32), text, "en", time.Now()); got != "" {
		t.Fatalf("oversized expansion must be rejected, got %d bytes", len(got))
	}
}

func TestReviewSelectionBoundedRenderingPreservesSemantics(t *testing.T) {
	for _, tc := range []struct{ name, template, text, want string }{
		{"exact", "{{text}}", strings.Repeat("界", MaxSelectionPromptBytes/3) + "xx", strings.Repeat("界", MaxSelectionPromptBytes/3) + "xx"},
		{"literal overflow", strings.Repeat("x", MaxSelectionPromptBytes+1), "", ""},
		{"suffix overflow", "{{text}}!", strings.Repeat("x", MaxSelectionPromptBytes), ""},
		{"unknown variable", "{{unknown}} {{text}}", "{{time}}", "{{unknown}} {{time}}"},
		{"unused selection", "fixed", strings.Repeat("x", MaxSelectionPromptBytes+1), "fixed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RenderSelection(tc.template, tc.text, "en", time.Now()); got != tc.want {
				t.Fatalf("unexpected rendering: got %d bytes, want %d", len(got), len(tc.want))
			}
		})
	}
}

func TestReviewFirstSettlementRefreshesTitleWritable(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	f := &titleFactory{}
	e, _ := New(store, f, "synthetic")
	defer e.Close()
	sid, _ := e.NewSession("main", "")
	if _, err := e.ensure(sid); err != nil {
		t.Fatal(err)
	}
	c := f.client
	if err := e.Send(sid, "synthetic first", "first", nil); err != nil {
		t.Fatal(err)
	}
	acceptanceCall(t, c.acceptanceClient, "prompt")
	acceptanceEventually(t, func() bool { e.mu.Lock(); defer e.mu.Unlock(); return len(e.deliveries) == 0 }, "delivery did not finish")
	e.refreshTitleFile(sid, false)
	if e.Snapshot("main").Current.TitleWritable {
		t.Fatal("missing file was writable")
	}
	f.mu.Lock()
	f.persisted = true
	f.mu.Unlock()
	c.emit("agent_settled", nil)
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.TitleWritable }, "first settled reply did not enable rename")
}

func TestReviewDispatchTimerDoesNotAddPersistenceGate(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, _ := e.NewSession("main", "")
	if _, err := e.ensure(sid); err != nil {
		t.Fatal(err)
	}
	c := <-f.starts
	m := Message{ID: "unsent", Role: "user", Text: "synthetic unsent", Status: "sending", CreatedAt: now()}
	e.mu.Lock()
	s := e.sessions[sid]
	s.DraftOnly = false
	s.Status = "starting"
	s.Messages = append(s.Messages, m)
	err := e.saveLocked(s)
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	defer e.store.db.Exec("PRAGMA query_only=OFF")
	e.deliver(context.Background(), sid, m, "prompt")
	if cmd := acceptanceCall(t, c, "prompt"); cmd["message"] != "synthetic unsent" {
		t.Fatal(cmd)
	}
}
