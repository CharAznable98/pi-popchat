package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"pi-popchat/internal/agent"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--mode" {
		runFake()
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func runFake() {
	scanner := bufio.NewScanner(os.Stdin)
	var mu sync.Mutex
	emit := func(v map[string]any) { mu.Lock(); defer mu.Unlock(); b, _ := json.Marshal(v); fmt.Println(string(b)) }
	for scanner.Scan() {
		var cmd map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &cmd)
		switch cmd["type"] {
		case "child":
			child := exec.Command("/bin/sleep", "30")
			child.Stdout = os.Stdout
			child.Stderr = os.Stderr
			if err := child.Start(); err != nil {
				os.Exit(2)
			}
			go child.Wait()
			emit(map[string]any{"type": "response", "id": cmd["id"], "success": true, "pid": child.Process.Pid})
		case "wait":
			continue
		case "exit":
			os.Exit(7)
		case "error":
			emit(map[string]any{"type": "response", "id": cmd["id"], "success": false, "error": "expected failure"})
		case "extension_ui_response":
			emit(map[string]any{"type": "interaction_received", "id": cmd["id"]})
		default:
			go func(cmd map[string]any) {
				time.Sleep(5 * time.Millisecond)
				emit(map[string]any{"type": "message_update", "value": cmd["value"]})
				emit(map[string]any{"type": "response", "id": cmd["id"], "success": true, "data": cmd["value"]})
			}(cmd)
		}
	}
}
func fakeClient(t *testing.T) agent.Client {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c, err := (Factory{}).Start(context.Background(), agent.Config{Executable: exe, CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func TestConcurrentResponsesAndEvents(t *testing.T) {
	c := fakeClient(t)
	events := make(chan int, 1)
	go func() {
		n := 0
		for e := range c.Events() {
			if e["type"] == "message_update" {
				n++
				if e["_sequence"] != uint64(n) {
					t.Errorf("event sequence: %v expected %d", e["_sequence"], n)
				}
			}
		}
		events <- n
	}()
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			r, err := c.Request(ctx, map[string]any{"type": "echo", "value": i})
			if seq, ok := r["_eventSequence"].(uint64); err == nil && (!ok || seq == 0) {
				t.Errorf("response missing barrier: %v", r)
			}
			if err != nil || r["data"] != float64(i) {
				t.Errorf("request %d: %v %v", i, r, err)
			}
		}(i)
	}
	wg.Wait()
	c.Close()
	if n := <-events; n != 40 {
		t.Fatalf("events: %d", n)
	}
}
func TestFailureCancellationAndOneWay(t *testing.T) {
	c := fakeClient(t)
	_, err := c.Request(context.Background(), map[string]any{"type": "error"})
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected RPCError: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = c.Request(ctx, map[string]any{"type": "wait"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	_, err = c.Request(context.Background(), map[string]any{"type": "extension_ui_response", "id": "dialog-7", "confirmed": true})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-c.Events():
		if ev["id"] != "dialog-7" {
			t.Fatal(ev)
		}
	case <-time.After(time.Second):
		t.Fatal("interaction response blocked")
	}
	c.(*client).mu.Lock()
	remaining := len(c.(*client).pending)
	c.(*client).mu.Unlock()
	if remaining != 0 {
		t.Fatalf("pending leak: %d", remaining)
	}
}
func TestExitUnblocksPendingAndDeliversExit(t *testing.T) {
	c := fakeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := c.Request(ctx, map[string]any{"type": "exit"})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("exit: %v", err)
	}
	select {
	case ev := <-c.Events():
		if ev["type"] != "process_exit" || ev["exitCode"] != 7 || ev["expected"] != false {
			t.Fatal(ev)
		}
	case <-ctx.Done():
		t.Fatal("missing exit event")
	}
}
func TestTailBoundedAndVersions(t *testing.T) {
	var b tailBuffer
	b.Write([]byte(strings.Repeat("a", 90000)))
	b.Write([]byte("END"))
	if len(b.data) != 32768 || !strings.HasSuffix(string(b.data), "END") {
		t.Fatal("tail not bounded")
	}
	if !newerNode("/x/v24.15.0/bin/pi", "/x/v9.0.0/bin/pi") {
		t.Fatal("semantic version ordering")
	}
}

func TestCloseTerminatesOwnedToolProcessGroup(t *testing.T) {
	c := fakeClient(t)
	go func() {
		for range c.Events() {
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := c.Request(ctx, map[string]any{"type": "child"})
	if err != nil {
		t.Fatal(err)
	}
	pid := int(r["pid"].(float64))
	c.Close()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("owned child process %d survived Close", pid)
}
