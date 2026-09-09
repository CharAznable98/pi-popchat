// Package pi implements Pi 0.84.1 JSONL RPC using its installed CLI. Authentication,
// model transport, tools, skills and extension execution remain Pi's responsibility.
package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"pi-popchat/internal/agent"
)

var ErrClosed = errors.New("Pi 进程已退出")

type RPCError struct{ Command, Message string }

func (e *RPCError) Error() string { return fmt.Sprintf("Pi %s: %s", e.Command, e.Message) }

type Factory struct{}

var _ agent.Factory = Factory{}

func (Factory) Start(ctx context.Context, cfg agent.Config) (agent.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	executable := cfg.Executable
	if executable == "" {
		var err error
		executable, err = Locate()
		if err != nil {
			return nil, err
		}
	}
	if !filepath.IsAbs(executable) {
		p, err := exec.LookPath(executable)
		if err != nil {
			return nil, err
		}
		executable = p
	}
	args := []string{"--mode", "rpc"}
	if cfg.SessionDir != "" {
		if err := os.MkdirAll(cfg.SessionDir, 0700); err != nil {
			return nil, err
		}
		args = append(args, "--session-dir", cfg.SessionDir)
	}
	if cfg.SessionFile != "" {
		args = append(args, "--session", cfg.SessionFile)
	}
	args = append(args, cfg.ExtraArgs...)
	cmd := exec.Command(executable, args...)
	cmd.Dir = cfg.CWD
	cmd.Env = processEnvironment(executable)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, stdoutWriter, err := os.Pipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	cmd.Stdout = stdoutWriter
	cmd.WaitDelay = 2 * time.Second
	c := &client{historyFile: cfg.SessionFile, historyRoot: cfg.SessionDir, cmd: cmd, stdin: stdin, events: make(chan map[string]any, 128), incoming: make(chan eventEnvelope, 128), done: make(chan struct{}), pending: make(map[string]chan map[string]any), writeGate: make(chan struct{}, 1)}
	cmd.Stderr = &c.stderr
	if err = cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stdoutWriter.Close()
		return nil, fmt.Errorf("无法启动 Pi: %w", err)
	}
	stdoutWriter.Close()
	go c.dispatch()
	go c.readAndWait(stdout)
	return c, nil
}

type eventEnvelope struct {
	event map[string]any
	size  int
}

type client struct {
	historyFile string
	historyRoot string
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	events      chan map[string]any
	incoming    chan eventEnvelope
	done        chan struct{}
	writeGate   chan struct{}
	mu          sync.Mutex
	pending     map[string]chan map[string]any
	seq         atomic.Uint64
	closing     atomic.Bool
	closeOnce   sync.Once
	stderr      tailBuffer // Deliberately not returned in UI events: extensions may log secrets.
}

func (c *client) Events() <-chan map[string]any { return c.events }
func (c *client) Request(ctx context.Context, command map[string]any) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	typ, _ := command["type"].(string)
	if typ == "" {
		return nil, errors.New("Pi 命令缺少 type")
	}
	data := make(map[string]any, len(command)+1)
	for k, v := range command {
		data[k] = v
	}
	oneWay := typ == "extension_ui_response"
	id := strconv.FormatUint(c.seq.Add(1), 10)
	response := make(chan map[string]any, 1)
	if !oneWay {
		data["id"] = id
		c.mu.Lock()
		c.pending[id] = response
		c.mu.Unlock()
		defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, ErrClosed
	case c.writeGate <- struct{}{}:
	}
	if err := ctx.Err(); err != nil {
		<-c.writeGate
		return nil, err
	}
	// os.File pipes support write deadlines, including large image payloads. A
	// canceled context unblocks a stalled pipe; incomplete writes terminate the
	// process because a truncated JSONL command cannot be safely recovered.
	var stopCancel func() bool
	cancelDone := make(chan struct{})
	if pipe, ok := c.stdin.(*os.File); ok {
		if deadline, ok := ctx.Deadline(); ok {
			_ = pipe.SetWriteDeadline(deadline)
		}
		stopCancel = context.AfterFunc(ctx, func() { _ = pipe.SetWriteDeadline(time.Now()); close(cancelDone) })
	}
	_, err = c.stdin.Write(encoded)
	if stopCancel != nil {
		// If cancellation has begun, wait until its deadline write completes before
		// clearing the deadline so it cannot poison the next request.
		if !stopCancel() {
			<-cancelDone
		}
		if pipe, ok := c.stdin.(*os.File); ok {
			_ = pipe.SetWriteDeadline(time.Time{})
		}
	}
	<-c.writeGate
	if err != nil {
		// A partial JSONL write cannot safely be followed by another command.
		_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("写入 Pi 命令失败: %w", err)
	}
	if oneWay {
		return map[string]any{"type": "response", "success": true, "command": typ}, nil
	}
	select {
	case r := <-response:
		if success, ok := r["success"].(bool); ok && !success {
			message, _ := r["error"].(string)
			return r, &RPCError{Command: typ, Message: message}
		}
		return r, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		// A response read immediately before EOF is still a valid acknowledgement.
		select {
		case r := <-response:
			if success, ok := r["success"].(bool); ok && !success {
				message, _ := r["error"].(string)
				return r, &RPCError{Command: typ, Message: message}
			}
			return r, nil
		default:
			return nil, ErrClosed
		}
	}
}

func (c *client) readAndWait(stdout io.ReadCloser) {
	defer stdout.Close()
	waitResult := make(chan error, 1)
	go func() {
		err := c.cmd.Wait()
		_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
		waitResult <- err
	}()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	var eventSequence uint64
	for scanner.Scan() {
		var event map[string]any
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		} // Non-protocol startup output is never exposed as chat.
		if event["type"] == "response" {
			event["_eventSequence"] = eventSequence
			id, _ := event["id"].(string)
			c.mu.Lock()
			if event["command"] == "get_state" {
				if data, ok := event["data"].(map[string]any); ok {
					if path, ok := data["sessionFile"].(string); ok && path != "" {
						c.historyFile = path
					}
				}
			}
			ch := c.pending[id]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- event:
				default:
				}
			}
		} else {
			eventSequence++
			event["_sequence"] = eventSequence
			c.incoming <- eventEnvelope{event: event, size: len(scanner.Bytes())}
		}
	}
	readErr := scanner.Err()
	if readErr != nil {
		_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
	}
	err := <-waitResult
	// Terminate tools that outlive their Pi parent, without touching unrelated Pi.
	_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
	_ = c.stdin.Close()
	close(c.done)
	reason := ""
	if readErr != nil {
		reason = "Pi 输出超出协议限制或读取失败"
	} else if err != nil && !c.closing.Load() {
		reason = "Pi 进程异常退出"
	}
	c.incoming <- eventEnvelope{event: map[string]any{"type": "process_exit", "_sequence": eventSequence + 1, "expected": c.closing.Load(), "exitCode": c.cmd.ProcessState.ExitCode(), "error": reason}, size: 256}
	close(c.incoming)
}

// A bounded delivery queue decouples RPC acknowledgements from UI scheduling.
// If a consumer stops reading indefinitely, stop the agent rather than silently
// dropping events and leaving the application with an invented execution state.
func (c *client) dispatch() {
	defer close(c.events)
	queue := make([]eventEnvelope, 0, 128)
	queuedBytes := 0
	overflowed := false
	input := c.incoming
	for input != nil || len(queue) > 0 {
		var output chan map[string]any
		var next map[string]any
		if len(queue) > 0 {
			output = c.events
			next = queue[0].event
		}
		select {
		case ev, ok := <-input:
			if !ok {
				input = nil
				continue
			}
			queue = append(queue, ev)
			queuedBytes += ev.size
			if !overflowed && (len(queue) >= 8192 || queuedBytes > 64*1024*1024) {
				overflowed = true
				_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
			}
		case output <- next:
			queuedBytes -= queue[0].size
			queue[0] = eventEnvelope{}
			queue = queue[1:]
		}
	}
}

func (c *client) Close() error {
	c.closeOnce.Do(func() {
		c.closing.Store(true)
		select {
		case <-c.done:
			return
		default:
		}
		_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-c.done:
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
			<-c.done
		}
	})
	return nil
}

type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	const limit = 32 * 1024
	n := len(p)
	if len(p) >= limit {
		b.data = append(b.data[:0], p[len(p)-limit:]...)
	} else {
		b.data = append(b.data, p...)
		if len(b.data) > limit {
			b.data = append(b.data[:0], b.data[len(b.data)-limit:]...)
		}
	}
	return n, nil
}
