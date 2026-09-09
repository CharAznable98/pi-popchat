package core

import "testing"

func TestLateStartEventCannotUndoUserStop(t *testing.T) {
	e, f := acceptanceEngine(t)
	sid, c := acceptanceStart(t, e, f, "main")
	if err := e.Stop(sid); err != nil {
		t.Fatal(err)
	}
	acceptanceCall(t, c, "abort")
	// IPC can already contain agent_start while the user's abort is being sent.
	c.emit("agent_start", nil)
	c.emit("agent_settled", nil)
	c.emit("extension_ui_request", map[string]any{"method": "notify", "message": "event-barrier"})
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Error == "event-barrier" }, "event barrier not consumed")
	if got := e.Snapshot("main").Current.Status; got != "stopped" {
		t.Fatalf("late event undid stop: %s", got)
	}
	if err := e.Send(sid, "continue explicitly", "explicit-next", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.QueueAction(sid, "resumeQueue", ""); err != nil {
		t.Fatal(err)
	}
	acceptanceCall(t, c, "prompt")
	c.emit("agent_start", nil)
	acceptanceEventually(t, func() bool { return e.Snapshot("main").Current.Status == "running" }, "explicit continuation remained stopped")
}
