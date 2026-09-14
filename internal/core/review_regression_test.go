package core

import (
	"context"
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
