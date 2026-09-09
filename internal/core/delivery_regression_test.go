package core

import (
	"context"
	"encoding/base64"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"pi-popchat/internal/agent"
)

type settlingFactory struct{ client *settlingClient }
type settlingClient struct {
	*acceptanceClient
	states       atomic.Int32
	queryStarted chan struct{}
	releaseQuery chan struct{}
}

func (f *settlingFactory) Start(_ context.Context, cfg agent.Config) (agent.Client, error) {
	f.client.cfg = cfg
	return f.client, nil
}
func (c *settlingClient) Request(ctx context.Context, cmd map[string]any) (map[string]any, error) {
	if cmd["type"] == "get_state" && c.states.Add(1) == 2 {
		close(c.queryStarted)
		select {
		case <-c.releaseQuery:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		// This response was sampled before settled, but its caller completes later.
		return map[string]any{"success": true, "data": map[string]any{"isStreaming": true}}, nil
	}
	return c.acceptanceClient.Request(ctx, cmd)
}
func TestSettlementDuringSubmissionCleanupAdvancesQueue(t *testing.T) {
	for _, mode := range []string{"advance", "save-failure", "stop"} {
		t.Run(mode, func(t *testing.T) {
			store, err := OpenStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			c := &settlingClient{acceptanceClient: &acceptanceClient{events: make(chan map[string]any, 256), calls: make(chan map[string]any, 256)}, queryStarted: make(chan struct{}), releaseQuery: make(chan struct{})}
			e, err := New(store, &settlingFactory{client: c}, "test-pi")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { e.Close() })
			sid, err := e.NewSession("main", "")
			if err != nil {
				t.Fatal(err)
			}
			if err = e.Send(sid, "first", "first", nil); err != nil {
				t.Fatal(err)
			}
			acceptanceCall(t, c.acceptanceClient, "prompt")
			select {
			case <-c.queryStarted:
			case <-time.After(3 * time.Second):
				t.Fatal("no post-submit state query")
			}
			if err = e.Send(sid, "queued", "queued", nil); err != nil {
				t.Fatal(err)
			}
			c.emit("agent_settled", nil)
			acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "idle" }, "settled not consumed")

			if mode == "save-failure" {
				if _, err := e.store.db.Exec("PRAGMA query_only=ON"); err != nil {
					t.Fatal(err)
				}
				defer e.store.db.Exec("PRAGMA query_only=OFF")
			}
			if mode == "stop" {
				if err := e.Stop(sid); err != nil {
					t.Fatal(err)
				}
				acceptanceCall(t, c.acceptanceClient, "abort")
			}
			close(c.releaseQuery)
			if mode == "stop" {
				acceptanceNoCall(t, c.acceptanceClient)
				return
			}
			if mode == "save-failure" {
				acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.QueuePaused }, "failed save did not visibly pause queue")
				acceptanceNoCall(t, c.acceptanceClient)
				if _, err := e.store.db.Exec("PRAGMA query_only=OFF"); err != nil {
					t.Fatal(err)
				}
				if err := e.QueueAction(sid, "resumeQueue", ""); err != nil {
					t.Fatal(err)
				}
			}
			if got := acceptanceCall(t, c.acceptanceClient, "prompt")["message"]; got != "queued" {
				t.Fatalf("wrong queued delivery: %v", got)
			}
			acceptanceNoCall(t, c.acceptanceClient)
		})
	}
}

func TestQueuedImageGrowthIsRejectedBeforeRPC(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, c := acceptanceStart(t, e, f, "main")
	png := []byte{137, 80, 78, 71, 13, 10, 26, 10}
	a, err := e.SaveAttachment("image.png", "image/png", base64.StdEncoding.EncodeToString(png))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Send(sid, "queued image", "image-message", []Attachment{a}); err != nil {
		t.Fatal(err)
	}
	if err = os.Truncate(a.Path, MaxMessageImageBytes+1); err != nil {
		t.Fatal(err)
	}
	c.emit("agent_settled", nil)
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "failed" }, "grown image was not rejected")
	acceptanceNoCall(t, c)
	if !e.Snapshot("main").Current.QueuePaused {
		t.Fatal("invalid attachment did not pause queue")
	}
}
