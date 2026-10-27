package main

import (
	"context"
	"os"
	"path/filepath"
	"pi-popchat/internal/agent"
	"pi-popchat/internal/agent/pi"
	"pi-popchat/internal/core"
	"sync"
	"testing"
	"time"
)

// An in-flight desktop Refresh can finish after OnShutdown has closed the store.
// Shutdown must freeze the projection rather than change a saved idle session
// to failed or silently report a successful selection that was not persisted.
func TestNativeAcceptanceCallbacksAfterShutdown(t *testing.T) {
	store, err := core.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.New(store, pi.Factory{}, "")
	if err != nil {
		t.Fatal(err)
	}
	sid, err := engine.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.Close(); err != nil {
		t.Fatal(err)
	}
	before := engine.Snapshot("main")
	if err = engine.Refresh(sid); err == nil {
		t.Error("refresh after shutdown should fail")
	}
	after := engine.Snapshot("main")
	if after.Current.Status != before.Current.Status || after.Error != before.Error {
		t.Errorf("late refresh mutated closed engine: status %q -> %q; error %q -> %q", before.Current.Status, after.Current.Status, before.Error, after.Error)
	}
	if err = engine.Select("main", sid); err == nil {
		t.Error("notification selection after shutdown falsely succeeds")
	}
}

func TestNativeAppliedHideDoesNotExpireSelectionSession(t *testing.T) {
	store, err := core.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.New(store, nativeTestFactory{}, "fake-pi")
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	d := &Desktop{engine: engine, done: make(chan struct{}), panelHiddenAt: time.Now().Add(-31 * time.Minute)}
	d.flushPanelHidden()
	sid, err := engine.SubmitSelection("synthetic selection")
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.Stop(sid); err != nil {
		t.Fatal(err)
	}
	d.flushPanelHidden()
	if err = engine.PanelShown(); err != nil {
		t.Fatal(err)
	}
	if engine.CurrentID("panel") != sid {
		t.Fatal("a processed hide was replayed after selection reset its timeout")
	}
}

func TestNativeQueuedHideDoesNotExpireSelectionSession(t *testing.T) {
	store, err := core.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.New(store, nativeTestFactory{}, "fake-pi")
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	d := &Desktop{engine: engine, done: make(chan struct{}), panelHiddenAt: time.Now().Add(-31 * time.Minute)}
	sid, err := d.submitSelection("synthetic newer selection")
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.Stop(sid); err != nil {
		t.Fatal(err)
	}
	d.flushPanelHidden()
	if err = engine.PanelShown(); err != nil {
		t.Fatal(err)
	}
	if engine.CurrentID("panel") != sid {
		t.Fatal("a queued hide was replayed after a newer selection submission")
	}
}

func TestNativeSubmissionReceiptRemainsBoundToOriginalSession(t *testing.T) {
	store, err := core.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.New(store, nativeTestFactory{}, "fake-pi")
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	original, err := engine.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.Send(original, "synthetic original", "stable-submission", nil); err != nil {
		t.Fatal(err)
	}
	other, err := engine.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	d := &Desktop{engine: engine, done: make(chan struct{})}
	duplicate, err := d.Action("main", "send", map[string]any{"id": original, "text": "synthetic original", "clientMessageId": "stable-submission"})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.CurrentID != other || duplicate.Submission == nil || duplicate.Submission.SessionID != original || !duplicate.Submission.Duplicate || duplicate.Submission.DraftRevision != 1 {
		t.Fatalf("duplicate receipt followed the selected session: %+v", duplicate.Submission)
	}
	accepted, err := d.Action("main", "send", map[string]any{"id": original, "text": "synthetic follow-up", "clientMessageId": "second-submission", "expectedDraftRevision": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.CurrentID != other || accepted.Submission == nil || accepted.Submission.SessionID != original || accepted.Submission.Duplicate || accepted.Submission.DraftRevision != 2 {
		t.Fatalf("accepted receipt followed the selected session: %+v", accepted.Submission)
	}
}

type nativeTestFactory struct{}
type nativeTestClient struct {
	done   chan struct{}
	events chan map[string]any
	once   sync.Once
}

func (nativeTestFactory) Start(context.Context, agent.Config) (agent.Client, error) {
	return &nativeTestClient{events: make(chan map[string]any), done: make(chan struct{})}, nil
}
func (c *nativeTestClient) Request(context.Context, map[string]any) (map[string]any, error) {
	return map[string]any{"data": map[string]any{}}, nil
}
func (c *nativeTestClient) Events() <-chan map[string]any { return c.events }
func (c *nativeTestClient) Done() <-chan struct{}         { return c.done }
func (c *nativeTestClient) Close() error {
	c.once.Do(func() { close(c.done); close(c.events) })
	return nil
}

func TestNativeAcceptanceQuitDecisionBoundaries(t *testing.T) {
	if !(&Desktop{}).shouldQuit() {
		t.Error("quit before engine initialization should succeed")
	}
	store, err := core.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.New(store, nativeTestFactory{}, "fake-pi")
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	d := &Desktop{engine: engine}
	if !d.shouldQuit() {
		t.Error("idle application should quit without dialog")
	}
	sid, err := engine.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.Send(sid, "pending work", "native-quit-1", nil); err != nil {
		t.Fatal(err)
	}
	if engine.ActiveCount() == 0 {
		t.Fatal("test must have an active task")
	}
	d.quitPrompt = true
	if d.shouldQuit() {
		t.Error("second quit while confirmation pending must not bypass confirmation")
	}
	d.quitApproved = true
	if !d.shouldQuit() {
		t.Error("explicitly approved quit must succeed even with active task")
	}
}

func TestNativeAcceptanceOpenPathRejectsInvalidInputs(t *testing.T) {
	store, err := core.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine, err := core.New(store, nativeTestFactory{}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	d := &Desktop{engine: engine}
	for _, path := range []string{"", "bad\x00path", "file://remote-host/private/a", "file://%zz/path", "relative-missing.txt"} {
		if err := d.openPath("main", path, false); err == nil {
			t.Errorf("invalid path accepted: %q", path)
		}
	}
	sid, err := engine.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = sid
	missing := "does-not-exist-native-acceptance"
	if err := d.openPath("main", missing, true); err == nil {
		t.Error("missing relative path accepted")
	}
	if _, err := os.Stat(filepath.Join(engine.Snapshot("main").Current.CWD, missing)); !os.IsNotExist(err) {
		t.Fatal("test precondition: missing path exists")
	}
	for _, raw := range []string{"javascript:alert(1)", "file:///private/a", "https:///missing-host", "mailto:example@example.com"} {
		if err := openWebURL(raw); err == nil {
			t.Errorf("invalid web URL accepted: %q", raw)
		}
	}
}
