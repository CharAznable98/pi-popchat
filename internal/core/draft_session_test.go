package core

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSharedComposerPersistsOutsideHistory(t *testing.T) {
	e, _ := acceptanceEngine(t)
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	original := e.Snapshot("main").Current
	if _, err := os.Stat(original.CWD); !os.IsNotExist(err) {
		t.Fatal("new composer created a workspace")
	}
	attachment, err := e.SaveAttachment("draft.txt", "text/plain", base64.StdEncoding.EncodeToString([]byte("test fixture")))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.DraftWithAttachments(sid, "unsent", nil, []Attachment{attachment}); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	if err := e.SetCWD(sid, cwd); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if next, err := e.NewSession("panel", ""); err != nil || next != sid {
			t.Fatalf("did not reuse composer: %s %v", next, err)
		}
		e.mu.Lock()
		e.hiddenAt = time.Now().Add(-time.Hour)
		e.mu.Unlock()
		if err := e.PanelShown(); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.Snapshot("panel").Sessions) != 0 {
		t.Fatal("composer leaked into history")
	}
	records, err := e.store.Load()
	if err != nil || len(records) != 0 {
		t.Fatalf("composer saved as a session: %v %v", records, err)
	}
	root := e.Root()
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := New(store, &acceptanceFactory{}, "test-pi")
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	for _, view := range []string{"main", "panel"} {
		s := restored.Snapshot(view)
		if s.CurrentID != sid || len(s.Sessions) != 0 || !s.Current.DraftOnly || s.Current.Draft != "unsent" || s.Current.CWD != cwd || len(s.Current.DraftAttachments) != 1 || s.Current.ManagedWorkspace {
			t.Fatalf("lost shared draft: %+v", s)
		}
	}
}

func TestFirstSubmissionAtomicPromotionAndConcurrentDeduplication(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, _ := e.NewSession("main", "")
	_, _ = e.NewSession("panel", "")
	if err := e.Draft(sid, "first", nil); err != nil {
		t.Fatal(err)
	}
	revision := e.Snapshot("main").Current.DraftRevision
	// Fail after inserting the session to prove the two tables roll back together.
	if _, err := e.store.db.Exec("CREATE TRIGGER reject_promotion BEFORE DELETE ON composer BEGIN SELECT RAISE(ABORT, 'fixture'); END;"); err != nil {
		t.Fatal(err)
	}
	if err := e.SendWithRevision(sid, "first", "message-1", nil, &revision); err == nil {
		t.Fatal("promotion should fail")
	}
	records, _ := e.store.Load()
	draft, _ := e.store.LoadDraft()
	if len(records) != 0 || draft == nil || draft.Draft != "first" || !e.Snapshot("main").Current.DraftOnly {
		t.Fatal("failed promotion lost draft or created history")
	}
	select {
	case <-f.starts:
		t.Fatal("failed save started Agent")
	default:
	}
	if _, err := e.store.db.Exec("DROP TRIGGER reject_promotion"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.SendWithRevision(sid, "first", "message-1", nil, &revision); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	client := <-f.starts
	acceptanceCall(t, client, "prompt")
	acceptanceNoCall(t, client)
	if err := e.SendWithRevision(sid, "first", "other-window", nil, &revision); err == nil {
		t.Fatal("stale second window resent draft")
	}
	snapshot := e.Snapshot("panel")
	if snapshot.CurrentID != sid || snapshot.Current.DraftOnly || len(snapshot.Sessions) != 1 || len(snapshot.Current.Messages) != 1 {
		t.Fatalf("incorrect promotion: %+v", snapshot)
	}
	if draft, err := e.store.LoadDraft(); err != nil || draft != nil {
		t.Fatalf("promoted composer still persisted: %v", err)
	}
	next, err := e.NewSession("main", "")
	if err != nil || next == sid {
		t.Fatal("new conversation reused submitted session")
	}
	if e.CurrentID("panel") != sid {
		t.Fatal("new changed other window's submitted session")
	}
	if err := e.DraftWithRevision(sid, "late write", nil, nil, &revision); err == nil {
		t.Fatal("stale draft write accepted")
	}
	// Saves for the previous session cannot remove the next shared composer.
	if err := e.Pin(sid, true); err != nil {
		t.Fatal(err)
	}
	draft, err = e.store.LoadDraft()
	if err != nil || draft == nil || draft.ID != next {
		t.Fatal("older session removed new composer")
	}
}

func TestDraftPreparationFailureDoesNotBlockFirstSubmission(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, _ := e.NewSession("main", "")
	e.SetEnvironment(Environment{})
	if err := e.Prepare(sid); err == nil {
		t.Fatal("missing Agent should fail")
	}
	if len(e.Snapshot("main").Sessions) != 0 {
		t.Fatal("metadata failure created history")
	}
	e.SetEnvironment(Environment{Available: true, PiPath: "test-pi"})
	if err := e.Send(sid, "first", "first", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case client := <-f.starts:
		acceptanceCall(t, client, "prompt")
	case <-time.After(time.Second):
		t.Fatal("first submission stranded in paused queue")
	}
}

func TestFirstSubmissionValidationAndAgentFailure(t *testing.T) {
	for _, mode := range []string{"text", "attachment", "command"} {
		t.Run(mode, func(t *testing.T) {
			e, _ := acceptanceEngine(t)
			sid, _ := e.NewSession("main", "")
			if err := e.Send(sid, "  ", "empty", nil); err == nil {
				t.Fatal("empty send accepted")
			}
			if len(e.Snapshot("main").Sessions) != 0 {
				t.Fatal("invalid send created history")
			}
			e.SetEnvironment(Environment{})
			text := "first"
			var attachments []Attachment
			if mode == "command" {
				text = "/fixture"
			}
			if mode == "attachment" {
				text = ""
				a, err := e.SaveAttachment("a.txt", "text/plain", base64.StdEncoding.EncodeToString([]byte("fixture")))
				if err != nil {
					t.Fatal(err)
				}
				attachments = []Attachment{a}
			}
			if err := e.Send(sid, text, "valid", attachments); err != nil {
				t.Fatal(err)
			}
			acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "failed" }, "Agent failure not shown")
			if s := e.Snapshot("main"); len(s.Sessions) != 1 || len(s.Current.Messages) != 1 || s.Current.DraftOnly {
				t.Fatal("Agent failure lost submitted conversation")
			}
			if _, err := os.Stat(filepath.Join(e.Root(), "sessions", sid)); !os.IsNotExist(err) {
				t.Fatal("missing Agent created session files")
			}
		})
	}
}
