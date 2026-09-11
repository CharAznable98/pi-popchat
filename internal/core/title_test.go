package core

import (
	"context"
	"errors"
	"pi-popchat/internal/agent"
	"strings"
	"sync"
	"testing"
)

type titleFactory struct {
	mu        sync.Mutex
	name      string
	persisted bool
	client    *titleClient
}
type titleClient struct {
	*acceptanceClient
	owner   *titleFactory
	renames int
}

func (f *titleFactory) ReadSessionInfo(context.Context, agent.Config) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.persisted {
		return "", agent.ErrHistoryMissing
	}
	return f.name, nil
}
func (f *titleFactory) Start(_ context.Context, cfg agent.Config) (agent.Client, error) {
	f.client = &titleClient{acceptanceClient: &acceptanceClient{cfg: cfg, events: make(chan map[string]any, 32), calls: make(chan map[string]any, 32)}, owner: f}
	return f.client, nil
}
func (c *titleClient) Request(ctx context.Context, cmd map[string]any) (map[string]any, error) {
	switch cmd["type"] {
	case "set_session_name":
		c.owner.mu.Lock()
		defer c.owner.mu.Unlock()
		c.renames++
		if cmd["name"] == "reject" {
			return nil, errors.New("synthetic rejection")
		}
		c.owner.name = strings.Join(strings.Fields(str(cmd["name"])), " ")
		return map[string]any{"success": true}, nil
	case "get_state":
		c.owner.mu.Lock()
		defer c.owner.mu.Unlock()
		return map[string]any{"data": map[string]any{"sessionName": c.owner.name}}, nil
	default:
		return c.acceptanceClient.Request(ctx, cmd)
	}
}
func TestPiTitleAuthorityRenameAndRecovery(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	f := &titleFactory{}
	e, err := New(store, f, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	sid, _ := e.NewSession("main", "")
	if err = e.Rename(sid, "premature"); err == nil || f.client != nil {
		t.Fatal("renamed before Pi persistence")
	}
	e.mu.Lock()
	s := e.sessions[sid]
	s.DraftOnly = false
	s.Messages = []Message{{ID: "u", Role: "user", Text: strings.Repeat("字", 40)}}
	s.Title = "obsolete local title"
	_ = e.saveLocked(s)
	e.mu.Unlock()
	f.mu.Lock()
	f.persisted = true
	f.name = "Pi initial"
	f.mu.Unlock()
	if err = e.Rename(sid, "  Pi\n normalized  "); err != nil {
		t.Fatal(err)
	}
	if e.Snapshot("main").Current.Title != "Pi normalized" || f.client.renames != 1 {
		t.Fatal("not Pi-normalized title")
	}
	if err = e.Rename(sid, "reject"); err == nil || e.Snapshot("main").Current.Title != "Pi normalized" {
		t.Fatal("rejected rename changed cache")
	}
	c := f.client
	c.emit("session_info_changed", map[string]any{"name": "Extension title"})
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Title == "Extension title" }, "extension title lost")
	c.emit("session_info_changed", nil)
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Title == strings.Repeat("字", 32) }, "clear ignored")
	root := store.Root
	_ = e.Close()
	f.mu.Lock()
	f.name = "Pi on disk"
	f.mu.Unlock()
	reopened, _ := OpenStore(root)
	restored, err := New(reopened, f, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if restored.Snapshot("main").Current.Title != "Pi on disk" {
		t.Fatal("local cache overrode Pi on restart")
	}
}

func TestStaleStateCannotOverwriteNewerTitleEvent(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, c := acceptanceStart(t, e, f, "main")
	c.emit("session_info_changed", map[string]any{"name": "new title", "_sequence": uint64(10)})
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Title == "new title" }, "missing event")
	e.applyMetadata(sid, "get_state", map[string]any{"_eventSequence": uint64(9), "data": map[string]any{"sessionName": "old title"}})
	if e.Snapshot("main").Current.Title != "new title" {
		t.Fatal("stale RPC overwrote title")
	}
}
