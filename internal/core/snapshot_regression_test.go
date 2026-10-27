package core

import (
	"encoding/json"
	"fmt"
	goruntime "runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func populateSnapshotFixture(t *testing.T, e *Engine) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	text := strings.Repeat("synthetic ", 50)
	for i := 0; i < 70; i++ {
		sid := fmt.Sprintf("synthetic-%02d", i)
		s := &Session{ID: sid, Title: "synthetic", Status: "idle", CWD: e.store.Root, CreatedAt: now(), UpdatedAt: now()}
		for j := 0; j < 13; j++ {
			role := "assistant"
			if j%2 == 0 {
				role = "user"
			}
			s.Messages = append(s.Messages, Message{ID: fmt.Sprintf("%s-message-%d", sid, j), Role: role, Text: text, Status: "complete", CreatedAt: now(), Attachments: []Attachment{}})
			s.SearchableText += "\n" + text
		}
		for j := 0; j < 400; j++ {
			s.Models = append(s.Models, Model{ID: fmt.Sprintf("synthetic-model-%d", j), Name: strings.Repeat("m", 40), Provider: "synthetic-provider"})
		}
		for j := 0; j < 150; j++ {
			s.Commands = append(s.Commands, Command{Name: fmt.Sprintf("synthetic-command-%d", j), Description: strings.Repeat("c", 40), Source: "synthetic-extension"})
		}
		if i < 3 {
			n := 447288
			if i == 0 {
				n = 1810110
			}
			s.Messages[0].Attachments = append(s.Messages[0].Attachments, Attachment{ID: "synthetic-image", Name: "synthetic.png", Path: "/synthetic.png", MIME: "image/png", Preview: "data:image/png;base64," + strings.Repeat("A", n)})
		}
		normalize(s)
		e.sessions[sid] = s
		if err := e.store.Save(s); err != nil {
			t.Fatal(err)
		}
		e.committed[sid], _ = json.Marshal(s)
	}
	e.selected["main"] = "synthetic-69"
	e.selected["panel"] = "synthetic-69"
}
func measureSnapshotLatency(t *testing.T, e *Engine, label string) {
	t.Helper()
	goruntime.GC()
	var before, after goruntime.MemStats
	goruntime.ReadMemStats(&before)
	times := make([]time.Duration, 20)
	for i := range times {
		start := time.Now()
		_ = e.Snapshot("main")
		times[i] = time.Since(start)
	}
	goruntime.ReadMemStats(&after)
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	t.Logf("%s snapshot p50=%s p95=%s max=%s allocated_per_call=%d bytes", label, times[10], times[18], times[19], (after.TotalAlloc-before.TotalAlloc)/20)
	if label == "70-sessions-full" && (after.TotalAlloc-before.TotalAlloc)/20 > 2*1024*1024 {
		t.Error("snapshot allocated inactive history or Agent metadata")
	}
}

func TestSnapshotOwnsMutableData(t *testing.T) {
	e, _ := acceptanceEngine(t)
	s := &Session{ID: "snapshot-fixture", Status: "idle", Draft: "private draft",
		Messages:         []Message{{Text: "message", Attachments: []Attachment{{Name: "attachment"}}, Steps: []ProcessStep{{Object: "step"}}}},
		Queue:            []Message{{Text: "queued", Attachments: []Attachment{{Name: "queued attachment"}}, Steps: []ProcessStep{{Object: "queued step"}}}},
		DraftAttachments: []Attachment{{Name: "draft attachment"}}, Models: []Model{{ID: "model"}}, Commands: []Command{{Name: "command"}},
		Interaction: &Interaction{Options: []string{"option"}}}
	e.sessions[s.ID] = s
	e.selected["main"] = s.ID
	first := e.Snapshot("main")
	if len(first.Sessions) != 1 || first.Sessions[0].Draft != "" || len(first.Sessions[0].DraftAttachments) != 0 || first.Sessions[0].Interaction != nil {
		t.Fatal("history summary exposed private composer or interaction data")
	}
	cp := first.Current
	cp.Messages[0].Text = "changed"
	cp.Messages[0].Attachments[0].Name = "changed"
	cp.Messages[0].Steps[0].Object = "changed"
	cp.Queue[0].Text = "changed"
	cp.Queue[0].Attachments[0].Name = "changed"
	cp.Queue[0].Steps[0].Object = "changed"
	cp.DraftAttachments[0].Name = "changed"
	cp.Models[0].ID = "changed"
	cp.Commands[0].Name = "changed"
	cp.Interaction.Options[0] = "changed"
	first.Settings.Selection.Buttons[0].Name = "changed"
	current := e.Snapshot("main").Current
	if current.Messages[0].Text != "message" || current.Messages[0].Attachments[0].Name != "attachment" || current.Messages[0].Steps[0].Object != "step" ||
		current.Queue[0].Text != "queued" || current.Queue[0].Attachments[0].Name != "queued attachment" || current.Queue[0].Steps[0].Object != "queued step" ||
		current.DraftAttachments[0].Name != "draft attachment" || current.Models[0].ID != "model" || current.Commands[0].Name != "command" || current.Interaction.Options[0] != "option" ||
		e.Snapshot("main").Settings.Selection.Buttons[0].Name == "changed" {
		t.Fatal("snapshot shares mutable state with the engine")
	}
}

func TestPanelHiddenPersistsCapturedTime(t *testing.T) {
	e, _ := acceptanceEngine(t)
	captured := time.Now().Add(-31 * time.Minute)
	e.PanelHiddenAt(captured)
	var persisted time.Time
	if err := e.store.Get("hiddenAt", &persisted); err != nil {
		t.Fatal(err)
	}
	if !persisted.Equal(captured) {
		t.Fatalf("hide time moved from %v to %v during deferred persistence", captured, persisted)
	}
}
func TestSnapshotScaling(t *testing.T) {
	e, _ := acceptanceEngine(t)
	if _, err := e.NewSession("main", ""); err != nil {
		t.Fatal(err)
	}
	measureSnapshotLatency(t, e, "one-empty-session")
	populateSnapshotFixture(t, e)
	encoded := 0
	for _, s := range e.sessions {
		b, _ := json.Marshal(s)
		encoded += len(b)
	}
	t.Logf("synthetic sessions=%d stored_json_bytes=%d", len(e.sessions), encoded)
	measureSnapshotLatency(t, e, "70-sessions-full")
	// Change only inactive metadata input; production Snapshot is untouched.
	for sid, s := range e.sessions {
		if sid != e.selected["main"] {
			s.Models = nil
			s.Commands = nil
		}
	}
	measureSnapshotLatency(t, e, "70-sessions-without-inactive-agent-metadata")
}

func TestSnapshotBacklogDoesNotBlockInteraction(t *testing.T) {
	e, _ := acceptanceEngine(t)
	populateSnapshotFixture(t, e)
	const count = 100
	ready := make(chan struct{}, count)
	done := make(chan struct{}, count)
	e.mu.Lock()
	for range count {
		go func() { ready <- struct{}{}; _ = e.Snapshot("main"); done <- struct{}{} }()
	}
	for range count {
		<-ready
	}
	// A barrier establishes a burst of already-pending requests; neither the
	// storage nor the snapshot implementation is artificially slowed.
	time.Sleep(10 * time.Millisecond)
	type result struct {
		name    string
		elapsed time.Duration
	}
	ui := make(chan result, 2)
	go func() { start := time.Now(); _ = e.PanelShown(); ui <- result{"panel-shown", time.Since(start)} }()
	go func() {
		start := time.Now()
		_ = e.Select("main", "synthetic-68")
		ui <- result{"session-select", time.Since(start)}
	}()
	time.Sleep(2 * time.Millisecond)
	start := time.Now()
	e.mu.Unlock()
	for range 2 {
		r := <-ui
		t.Logf("100 pending snapshots: %s latency=%s", r.name, r.elapsed)
		if r.elapsed > 200*time.Millisecond {
			t.Errorf("interaction waited for unrelated historical snapshots: %s=%s", r.name, r.elapsed)
		}
	}
	for range count {
		<-done
	}
	t.Logf("100 snapshots drained in %s", time.Since(start))
}
