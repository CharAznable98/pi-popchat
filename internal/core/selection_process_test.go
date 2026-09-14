package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSelectionTemplateSinglePassAndNoImplicitText(t *testing.T) {
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	at := time.Date(2026, 9, 11, 14, 5, 6, 0, zone)
	got := RenderSelection("{{text}}|{{language}}|{{date}}|{{time}}|{{timezone}}|{{unknown}}", "literal {{time}}", "zh-Hans-CN", at)
	want := "literal {{time}}|zh-Hans-CN|2026-09-11|14:05:06|Asia/Shanghai|{{unknown}}"
	if got != want {
		t.Fatalf("got %q", got)
	}
	if got = RenderSelection("fixed", "selected", "en", at); got != "fixed" {
		t.Fatal("selection appended")
	}
}
func TestSelectionSubmissionPreservesComposerAndUsesDistinctSessions(t *testing.T) {
	e, f := acceptanceEngine(t)
	draft, _ := e.NewSession("main", "")
	if err := e.Draft(draft, "synthetic unsent draft", nil); err != nil {
		t.Fatal(err)
	}
	a, err := e.SubmitSelection("synthetic selection A")
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.SubmitSelection("synthetic selection B")
	if err != nil {
		t.Fatal(err)
	}
	if a == b || a == draft || b == draft {
		t.Fatal("selection reused session")
	}
	if e.CurrentID("panel") != b || e.CurrentID("main") != draft {
		t.Fatal("wrong view selected")
	}
	saved, err := e.store.LoadDraft()
	if err != nil || saved.ID != draft || saved.Draft != "synthetic unsent draft" {
		t.Fatal("composer overwritten")
	}
	for i := 0; i < 2; i++ {
		c := <-f.starts
		call := acceptanceCall(t, c, "prompt")
		if !strings.HasPrefix(str(call["message"]), "synthetic selection ") {
			t.Fatal("wrong prompt")
		}
	}
}
func TestSelectionCommitFailureDoesNotStartAgent(t *testing.T) {
	e, f := acceptanceEngine(t)
	_, _ = e.store.db.Exec("CREATE TRIGGER fail_selection BEFORE INSERT ON settings BEGIN SELECT RAISE(FAIL,'fixture'); END")
	if _, err := e.SubmitSelection("synthetic"); err == nil {
		t.Fatal("expected commit failure")
	}
	all, err := e.store.Load()
	if err != nil || len(all) != 0 || len(e.Snapshot("panel").Sessions) != 0 {
		t.Fatal("partial session committed")
	}
	select {
	case <-f.starts:
		t.Fatal("started before commit")
	case <-time.After(30 * time.Millisecond):
	}
}
func TestProcessToolFailureDoesNotFailConversationAndSurvivesRefresh(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, c := acceptanceStart(t, e, f, "main")
	c.emit("tool_execution_start", map[string]any{"toolCallId": "read-1", "toolName": "read", "args": map[string]any{"path": "synthetic.txt", "secret": "must not persist"}})
	c.emit("tool_execution_end", map[string]any{"toolCallId": "read-1", "toolName": "read", "isError": true, "result": map[string]any{"secret": "must not persist"}})
	acceptanceText(c, "synthetic recovery", "stop")
	c.emit("agent_settled", nil)
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "idle" }, "tool failure failed conversation")
	s := e.Snapshot("main").Current
	var user Message
	for _, m := range s.Messages {
		if m.Role == "user" {
			user = m
			break
		}
	}
	if len(user.Steps) != 1 || user.Steps[0].Status != "failed" || user.Steps[0].Object != "synthetic.txt" {
		t.Fatalf("bad projection: %+v", user)
	}
	serialized, _ := json.Marshal(s)
	if strings.Contains(string(serialized), "must not persist") {
		t.Fatal("raw data persisted")
	}
	e.applyMetadata(sid, "get_messages", map[string]any{"data": map[string]any{"messages": []any{map[string]any{"role": "user", "content": user.Text}}}})
	if len(e.Snapshot("main").Current.Messages[0].Steps) != 1 {
		t.Fatal("history refresh lost process")
	}
	loaded, err := e.store.Load()
	if err != nil || len(loaded[0].Messages[0].Steps) != 1 {
		t.Fatal("process not persisted")
	}
}
func TestSelectionSettingsCopiesAndEmptyButtons(t *testing.T) {
	e, _ := acceptanceEngine(t)
	settings := e.Settings()
	settings.Selection.Buttons[0].Name = "changed"
	if e.Settings().Selection.Buttons[0].Name == "changed" {
		t.Fatal("settings alias")
	}
	settings.Selection.Buttons = []SelectionButton{}
	if err := e.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	if len(e.Settings().Selection.Buttons) != 0 {
		t.Fatal("empty buttons restored implicitly")
	}
}
