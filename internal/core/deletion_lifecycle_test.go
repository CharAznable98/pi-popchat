package core

import (
	"context"
	"os"
	"path/filepath"
	"pi-popchat/internal/agent"
	"testing"
	"time"
)

type closeHookClient struct {
	*acceptanceClient
	hook func() error
}

func (c *closeHookClient) Close() error {
	if c.hook != nil {
		if err := c.hook(); err != nil {
			return err
		}
	}
	return c.acceptanceClient.Close()
}

type closeHookFactory struct{ client *closeHookClient }

func (f closeHookFactory) Start(_ context.Context, cfg agent.Config) (agent.Client, error) {
	f.client.cfg = cfg
	return f.client, nil
}
func deletionFixture(t *testing.T) (*Engine, string, *closeHookClient) {
	t.Helper()
	e, _ := acceptanceEngine(t)
	c := &closeHookClient{acceptanceClient: &acceptanceClient{events: make(chan map[string]any, 256), calls: make(chan map[string]any, 256)}}
	e.factory = closeHookFactory{c}
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Prepare(sid); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	e.sessions[sid].DraftOnly = false
	err = e.saveLocked(e.sessions[sid])
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return e, sid, c
}
func TestWorkspaceReplacementDuringCloseIsPreserved(t *testing.T) {
	e, sid, c := deletionFixture(t)
	cwd := e.Snapshot("main").Current.CWD
	moved := cwd + "-original"
	c.hook = func() error {
		if err := os.Rename(cwd, moved); err != nil {
			return err
		}
		if err := os.Mkdir(cwd, 0700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(cwd, "unrelated.txt"), []byte("fixture"), 0600)
	}
	if err := e.DeleteWithWorkspace(sid, true); err == nil {
		t.Error("replacement directory was accepted for recursive removal")
	}
	if _, err := os.Stat(filepath.Join(cwd, "unrelated.txt")); err != nil {
		t.Error("unrelated replacement files deleted:", err)
	}
	if len(e.Snapshot("main").Sessions) != 1 {
		t.Error("replacement rejection removed session record")
	}
}
func TestClosingDeletedAgentDoesNotBlockOtherSessions(t *testing.T) {
	e, sid, c := deletionFixture(t)
	other, err := e.NewSession("panel", "")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	c.hook = func() error { close(entered); <-release; return nil }
	go func() { done <- e.DeleteWithWorkspace(sid, true) }()
	<-entered
	responsive := make(chan error, 1)
	go func() { _ = e.Snapshot("panel"); responsive <- e.Draft(other, "still responsive", nil) }()
	var blocked bool
	select {
	case err = <-responsive:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(200 * time.Millisecond):
		blocked = true
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if blocked {
		<-responsive
		t.Fatal("Agent.Close held the engine mutex and blocked other sessions")
	}
}
func TestReuseDraftSelectionFailureKeepsCurrentSession(t *testing.T) {
	e, sid, _ := deletionFixture(t)
	draft, err := e.NewSession("panel", "")
	if err != nil {
		t.Fatal(err)
	}
	before := e.Snapshot("main")
	if _, err = e.store.db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err = e.NewSession("main", ""); err == nil {
		t.Fatal("readonly selection write succeeded")
	}
	if e.CurrentID("main") != sid {
		t.Error("failed selection changed current session in memory")
	}
	if e.Snapshot("main").Version != before.Version {
		t.Error("failed selection published a state change")
	}
	var selected map[string]string
	if err = e.store.Get("selected", &selected); err != nil {
		t.Fatal(err)
	}
	if selected["main"] != sid || selected["panel"] != draft {
		t.Error("persisted selections changed")
	}
	if _, err = e.store.db.Exec("PRAGMA query_only=OFF"); err != nil {
		t.Fatal(err)
	}
	if got, err := e.NewSession("main", ""); err != nil || got != draft {
		t.Fatalf("selection retry failed: %s %v", got, err)
	}
}

func TestStagingAndCleanupRejectReplacementIdentity(t *testing.T) {
	for _, phase := range []string{"before-move", "before-cleanup"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			cwd := filepath.Join(root, "workspaces", "fixture")
			if err := os.MkdirAll(cwd, 0700); err != nil {
				t.Fatal(err)
			}
			expected, err := os.Lstat(cwd)
			if err != nil {
				t.Fatal(err)
			}
			path := cwd
			if phase == "before-cleanup" {
				path, err = stageWorkspaceForRemoval(cwd, root, "fixture", expected)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = os.Rename(path, path+"-original"); err != nil {
				t.Fatal(err)
			}
			if err = os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(path, "unrelated.txt"), []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			if phase == "before-move" {
				_, err = stageWorkspaceForRemoval(cwd, root, "fixture", expected)
			} else {
				err = removeStagedWorkspace(path, expected)
			}
			if err == nil {
				t.Fatal("replacement identity accepted")
			}
			files, err := filepath.Glob(filepath.Join(root, "workspaces", "*", "unrelated.txt"))
			if err != nil || len(files) != 1 {
				t.Fatalf("replacement content not preserved: %v %v", files, err)
			}
		})
	}
}
func TestDeletionRechecksReferencesAfterClosingAgent(t *testing.T) {
	e, sid, c := deletionFixture(t)
	cwd := e.Snapshot("main").Current.CWD
	other, err := e.NewSession("panel", "")
	if err != nil {
		t.Fatal(err)
	}
	c.hook = func() error { return e.SetCWD(other, cwd) }
	if err = e.DeleteWithWorkspace(sid, true); err == nil {
		t.Fatal("new reference established during close was ignored")
	}
	if _, err = os.Stat(cwd); err != nil {
		t.Fatal("newly shared workspace was removed")
	}
	if e.Snapshot("main").Current == nil {
		t.Fatal("rejected deletion removed history")
	}
	if err = e.Rename(sid, "usable after rejection"); err != nil {
		t.Fatal("deletion marker was not cleared")
	}
}
