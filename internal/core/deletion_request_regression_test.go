package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"pi-popchat/internal/agent"
	"testing"
	"time"
)

// The RPC response may already be received when deletion closes the client.
// Hold its return so the request finishes its final save after deletion.
type delayedRequestClient struct {
	*acceptanceClient
	kind    string
	entered chan struct{}
	release chan struct{}
}

func (c *delayedRequestClient) Request(ctx context.Context, cmd map[string]any) (map[string]any, error) {
	if cmd["type"] == c.kind {
		close(c.entered)
		select {
		case <-c.release:
			return map[string]any{"success": true}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.acceptanceClient.Request(ctx, cmd)
}

type delayedRequestFactory struct{ client *delayedRequestClient }

func (f delayedRequestFactory) Start(_ context.Context, cfg agent.Config) (agent.Client, error) {
	f.client.cfg = cfg
	return f.client, nil
}

func TestDeletionCannotBeUndoneByInflightRequest(t *testing.T) {
	for _, kind := range []string{"set_model", "abort"} {
		for _, remove := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/remove=%v", kind, remove), func(t *testing.T) {
				e, _ := acceptanceEngine(t)
				c := &delayedRequestClient{acceptanceClient: &acceptanceClient{events: make(chan map[string]any, 256), calls: make(chan map[string]any, 256)}, kind: kind, entered: make(chan struct{}), release: make(chan struct{})}
				e.factory = delayedRequestFactory{c}
				sid, err := e.NewSession("main", "")
				if err != nil {
					t.Fatal(err)
				}
				if err = e.Send(sid, "fixture", "first", nil); err != nil {
					t.Fatal(err)
				}
				acceptanceCall(t, c.acceptanceClient, "prompt")
				c.emit("agent_start", nil)
				c.emit("agent_settled", nil)
				acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "idle" }, "fixture did not settle")
				cwd := e.Snapshot("main").Current.CWD
				file := filepath.Join(cwd, "fixture.txt")
				if err = os.WriteFile(file, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() {
					if kind == "abort" {
						done <- e.Stop(sid)
					} else {
						done <- e.SetModel(sid, "fixture", "next")
					}
				}()
				select {
				case <-c.entered:
				case <-time.After(time.Second):
					t.Fatal("request did not start")
				}
				if err = e.DeleteWithWorkspace(sid, remove); err != nil {
					close(c.release)
					t.Fatal(err)
				}
				close(c.release)
				select {
				case err = <-done:
					if err == nil {
						t.Error("late request falsely reported successful save")
					}
				case <-time.After(time.Second):
					t.Fatal("late request did not finish")
				}
				rows, err := e.store.Load()
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) != 0 {
					t.Error("late request resurrected deleted session in database")
				}
				if draft, err := e.store.LoadDraft(); err != nil || draft != nil {
					t.Errorf("late request restored composer: %v", err)
				}
				_, err = os.Stat(file)
				if remove && !os.IsNotExist(err) {
					t.Error("workspace not removed")
				}
				if !remove && err != nil {
					t.Error("retained file lost")
				}
				root := e.Root()
				if err = e.Close(); err != nil {
					t.Fatal(err)
				}
				store, err := OpenStore(root)
				if err != nil {
					t.Fatal(err)
				}
				restored, err := New(store, delayedRequestFactory{c}, "test-pi")
				if err != nil {
					t.Fatal(err)
				}
				defer restored.Close()
				if len(restored.Snapshot("main").Sessions) != 0 {
					t.Error("ghost session reappeared on restart")
				}
			})
		}
	}
}

func TestDraftCreationSaveFailureLeavesNoLiveSession(t *testing.T) {
	e, _ := acceptanceEngine(t)
	if _, err := e.store.db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.NewSession("main", ""); err == nil {
		t.Fatal("readonly database accepted new draft")
	}
	e.mu.Lock()
	count := len(e.sessions)
	e.mu.Unlock()
	if count != 0 || e.CurrentID("main") != "" {
		t.Fatal("failed creation left a live session")
	}
	if _, err := e.store.db.Exec("PRAGMA query_only=OFF"); err != nil {
		t.Fatal(err)
	}
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := e.store.LoadDraft()
	if err != nil || draft == nil || draft.ID != sid {
		t.Fatal("retry failed to persist draft")
	}
}
