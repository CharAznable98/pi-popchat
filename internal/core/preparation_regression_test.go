package core

import (
	"context"
	"errors"
	"fmt"
	"pi-popchat/internal/agent"
	"sync/atomic"
	"testing"
	"time"
)

type failingPreparationFactory struct {
	attempts     atomic.Int32
	firstStarted chan struct{}
	failFirst    chan struct{}
	retryRelease chan struct{}
	delegate     *acceptanceFactory
}

func (f *failingPreparationFactory) Start(ctx context.Context, cfg agent.Config) (agent.Client, error) {
	if f.attempts.Add(1) == 1 {
		close(f.firstStarted)
		select {
		case <-f.failFirst:
			return nil, errors.New("fixture preparation failure")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	select {
	case <-f.retryRelease:
		return f.delegate.Start(ctx, cfg)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestLatePreparationFailureDoesNotPauseSubmittedConversation(t *testing.T) {
	for _, action := range []string{"prepare", "refresh"} {
		t.Run(action, func(t *testing.T) {
			e, delegate := acceptanceEngine(t)
			f := &failingPreparationFactory{firstStarted: make(chan struct{}), failFirst: make(chan struct{}), retryRelease: make(chan struct{}), delegate: delegate}
			e.factory = f
			sid, err := e.NewSession("main", "")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				if action == "refresh" {
					done <- e.Refresh(sid)
				} else {
					done <- e.Prepare(sid)
				}
			}()
			select {
			case <-f.firstStarted:
			case <-time.After(time.Second):
				t.Fatal("preparation did not start")
			}
			if err := e.Send(sid, "first", "first", nil); err != nil {
				t.Fatal(err)
			}
			close(f.failFirst)
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("fixture preparation should fail")
				}
			case <-time.After(time.Second):
				t.Fatal("preparation did not finish")
			}
			close(f.retryRelease)
			var client *acceptanceClient
			select {
			case client = <-delegate.starts:
			case <-time.After(time.Second):
				t.Fatal("first submission did not retry startup")
			}
			acceptanceCall(t, client, "prompt")
			client.emit("agent_start", nil)
			client.emit("agent_settled", nil)
			acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "idle" }, "first submission did not settle")
			if e.Snapshot("main").Current.QueuePaused {
				t.Fatal("obsolete preparation failure paused the submitted conversation")
			}
			if err := e.Send(sid, "next", "next", nil); err != nil {
				t.Fatal(err)
			}
			acceptanceCall(t, client, "prompt")
		})
	}
}

func TestSuccessfulPreparationClearsEarlierDraftStartupFailure(t *testing.T) {
	for _, overlap := range []bool{false, true} {
		t.Run(fmt.Sprintf("overlap=%v", overlap), func(t *testing.T) {
			e, delegate := acceptanceEngine(t)
			f := &failingPreparationFactory{firstStarted: make(chan struct{}), failFirst: make(chan struct{}), retryRelease: make(chan struct{}), delegate: delegate}
			e.factory = f
			sid, err := e.NewSession("main", "")
			if err != nil {
				t.Fatal(err)
			}
			_, err = e.NewSession("panel", "")
			if err != nil {
				t.Fatal(err)
			}
			first := make(chan error, 1)
			second := make(chan error, 1)
			go func() { first <- e.Prepare(sid) }()
			<-f.firstStarted
			if overlap {
				go func() { second <- e.Refresh(sid) }()
			}
			close(f.failFirst)
			if err = <-first; err == nil {
				t.Fatal("first startup did not fail")
			}
			if !overlap {
				go func() { second <- e.Refresh(sid) }()
			}
			close(f.retryRelease)
			select {
			case err = <-second:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("second preparation did not finish")
			}
			for _, view := range []string{"main", "panel"} {
				s := e.Snapshot(view).Current
				if s.Status != "idle" || s.Error != "" || s.QueuePaused {
					t.Errorf("successful preparation still displays stale failure: status=%s error=%s paused=%v", s.Status, s.Error, s.QueuePaused)
				}
				if len(s.Models) == 0 {
					t.Error("successful preparation did not load models")
				}
			}
		})
	}
}

func TestDraftPreparationRecoveryAfterRestart(t *testing.T) {
	e, _ := acceptanceEngine(t)
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	e.SetEnvironment(Environment{})
	if err = e.Prepare(sid); err == nil {
		t.Fatal("missing Agent did not fail")
	}
	root := e.Root()
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	factory := &acceptanceFactory{starts: make(chan *acceptanceClient, 4)}
	restored, err := New(store, factory, "test-pi")
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.Prepare(sid); err != nil {
		t.Fatal(err)
	}
	s := restored.Snapshot("main").Current
	if s.Status != "idle" || s.Error != "" || !s.DraftOnly {
		t.Fatalf("persisted preparation failure not recovered: %+v", s)
	}
}
