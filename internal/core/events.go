package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"pi-popchat/internal/agent"
)

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func str(v any) string         { s, _ := v.(string); return s }
func textContent(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	items, _ := v.([]any)
	out := ""
	for _, it := range items {
		m := obj(it)
		if str(m["type"]) == "text" {
			out += str(m["text"])
		}
	}
	return out
}
func (e *Engine) applyMetadata(sid, kind string, res map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.sessions[sid]
	if s == nil || e.closing {
		return
	}
	data := obj(res["data"])
	switch kind {
	case "get_state":
		if f := str(data["sessionFile"]); f != "" {
			s.SessionFile = f
		}
		if m := obj(data["model"]); m != nil {
			s.Model = str(m["id"])
			s.Provider = str(m["provider"])
		}
	case "get_available_models":
		s.ModelsState = "ready"
		s.ModelsError = ""
		b, _ := json.Marshal(data["models"])
		_ = json.Unmarshal(b, &s.Models)
	case "get_messages":
		original := s.Messages
		used := map[int]bool{}
		rebuilt := []Message{}
		for _, raw := range anySlice(data["messages"]) {
			pm := obj(raw)
			role := str(pm["role"])
			if role != "user" && role != "assistant" {
				continue
			}
			txt := textContent(pm["content"])
			key := agentMessageKey(pm)
			m := Message{ID: id(), Role: role, Text: txt, AgentKey: key, Status: "complete", CreatedAt: now(), Attachments: []Attachment{}}
			if ts, ok := pm["timestamp"].(float64); ok {
				m.CreatedAt = time.UnixMilli(int64(ts)).UTC().Format(time.RFC3339Nano)
			}
			for i, old := range original {
				if used[i] || old.Role != role {
					continue
				}
				if (key != "" && old.AgentKey == key) || (old.AgentKey == "" && old.Text == txt) {
					used[i] = true
					m.ID = old.ID
					m.CreatedAt = old.CreatedAt
					m.Attachments = old.Attachments
					if role == "user" {
						m.Text = old.Text
					}
					break
				}
			}
			rebuilt = append(rebuilt, m)
		}
		// UI-only extension commands need not exist in Pi history. Preserve them,
		// as well as uncertain submissions, instead of aligning users by ordinal.
		for i, m := range original {
			if !used[i] {
				rebuilt = append(rebuilt, m)
			}
		}
		sort.SliceStable(rebuilt, func(i, j int) bool { return rebuilt[i].CreatedAt < rebuilt[j].CreatedAt })
		s.Messages = rebuilt
	case "get_commands":
		b, _ := json.Marshal(data["commands"])
		_ = json.Unmarshal(b, &s.Commands)
	}
	normalize(s)
	_ = e.saveLocked(s)
}
func (e *Engine) consume(sid string, r *runtime, c agent.Client) {
	for ev := range c.Events() {
		e.event(sid, r, c, ev)
	}
}
func (e *Engine) event(sid string, r *runtime, c agent.Client, ev map[string]any) {
	e.mu.Lock()
	s := e.sessions[sid]
	if s == nil || e.closing || e.runtimes[sid] != r || r.client != c {
		e.mu.Unlock()
		return
	}
	switch n := ev["_sequence"].(type) {
	case float64:
		r.sequence = uint64(n)
	case uint64:
		r.sequence = n
	case int:
		r.sequence = uint64(n)
	}
	if r.progress != nil {
		close(r.progress)
	}
	r.progress = make(chan struct{})
	r.lastUsed = time.Now()
	typ := str(ev["type"])
	persist := true
	notifyTitle := ""
	var next *Message
	switch typ {
	case "process_exit":
		r.client = nil
		if busy(s.Status) {
			s.Status = "interrupted"
			s.Error = "Agent 进程已退出，执行结果可能不完整。请确认后继续。"
			s.QueuePaused = true
			s.Interaction = nil
			for i := range s.Messages {
				if s.Messages[i].Status == "sending" || s.Messages[i].Status == "accepted" {
					s.Messages[i].Status = "uncertain"
				}
			}
			notifyTitle = "任务已中断"
		}
	case "agent_start":
		if !r.stopped {
			s.Status = "running"
			s.Error = ""
			r.failed = false
		}
	case "auto_retry_start":
		if r.stopped {
			persist = false
			break
		}
		s.Status = "retrying"
		s.Error = ""
		r.failed = false
	case "auto_retry_end":
		if success, ok := ev["success"].(bool); ok && !success {
			r.failed = true
			s.Error = str(ev["error"])
		}
	case "tool_execution_start", "tool_execution_update", "tool_execution_end", "turn_start", "turn_end", "agent_end":
		persist = false
	case "message_start":
		m := obj(ev["message"])
		if str(m["role"]) == "user" {
			for i := range s.Messages {
				u := &s.Messages[i]
				if u.Role == "user" && u.AgentKey == "" && (u.Status == "sending" || u.Status == "accepted") {
					u.AgentKey = agentMessageKey(m)
					break
				}
			}
		}
		if str(m["role"]) == "assistant" {
			r.assistantID = id()
			s.Messages = append(s.Messages, Message{ID: r.assistantID, AgentKey: agentMessageKey(m), Role: "assistant", Text: textContent(m["content"]), Status: "sending", CreatedAt: now(), Attachments: []Attachment{}})
		}
	case "message_update":
		a := obj(ev["assistantMessageEvent"])
		if str(a["type"]) == "text_delta" {
			for i := range s.Messages {
				if s.Messages[i].ID == r.assistantID {
					s.Messages[i].Text += str(a["delta"])
					break
				}
			}
			e.changedLocked()
			persist = false
		} else {
			persist = false
		}
	case "message_end":
		m := obj(ev["message"])
		if str(m["role"]) == "assistant" {
			txt := textContent(m["content"])
			for i := range s.Messages {
				if s.Messages[i].ID == r.assistantID {
					s.Messages[i].Text = txt
					s.Messages[i].Status = "complete"
				}
			}
			reason := str(m["stopReason"])
			if reason == "error" {
				r.failed = true
				s.Error = str(m["errorMessage"])
				if s.Error == "" {
					s.Error = "Agent 执行失败"
				}
			}
			if reason == "aborted" {
				r.stopped = true
			}
		}
	case "extension_ui_request":
		method := str(ev["method"])
		switch method {
		case "select", "confirm", "input", "editor":
			if r.stopped {
				// An extension may request another dialog after receiving cancellation.
				// Acknowledge cancellation without resurrecting the stopped conversation.
				requestID := str(ev["id"])
				e.mu.Unlock()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_, _ = c.Request(ctx, map[string]any{"type": "extension_ui_response", "id": requestID, "cancelled": true})
				cancel()
				return
			}
			options := []string{}
			for _, v := range anySlice(ev["options"]) {
				options = append(options, fmt.Sprint(v))
			}
			s.Interaction = &Interaction{ID: str(ev["id"]), Method: method, Title: str(ev["title"]), Message: str(ev["message"]), DefaultValue: str(ev["prefill"]), Options: options}
			s.Status = "waiting"
			notifyTitle = "需要你的回答"
		case "notify":
			s.Error = str(ev["message"])
		case "set_editor_text":
			s.Draft = str(ev["text"])
		default:
			persist = false
		}
	case "agent_settled":
		s.Interaction = nil
		switch {
		case r.stopped:
			s.Status = "stopped"
			s.QueuePaused = true
		case r.failed:
			s.Status = "failed"
			s.QueuePaused = true
			notifyTitle = "任务未完成"
		default:
			s.Status = "idle"
			s.Error = ""
			notifyTitle = "回复已完成"
		}
		for i := range s.Messages {
			if s.Messages[i].Status == "accepted" || s.Messages[i].Status == "sending" {
				s.Messages[i].Status = "complete"
			}
		}
		s.UpdatedAt = now()
		submitting := false
		for _, d := range e.deliveries {
			if d.sid == sid {
				submitting = true
			}
		}
		if !s.QueuePaused && len(s.Queue) > 0 && !r.failed && !r.stopped && !submitting {
			m := s.Queue[0]
			s.Queue = s.Queue[1:]
			m.Status = "sending"
			s.Messages = append(s.Messages, m)
			s.Status = "starting"
			next = &m
			notifyTitle = ""
		}
	default:
		persist = false
	}
	if persist {
		if err := e.saveLocked(s); err != nil {
			next = nil
			s.QueuePaused = true
		}
	}
	if next != nil {
		e.scheduleLocked(sid, *next, "prompt")
	}
	title := s.Title
	e.mu.Unlock()
	if notifyTitle != "" && e.Notify != nil {
		e.Notify(sid, notifyTitle, title)
	}
	if typ == "agent_settled" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		res, err := c.Request(ctx, map[string]any{"type": "get_state"})
		cancel()
		if err == nil {
			e.applyMetadata(sid, "get_state", res)
		}
	}
}
func anySlice(v any) []any { a, _ := v.([]any); return a }

// Some extension commands do not start an agent loop, so no settled event exists.
func (e *Engine) finishCommand(sid, mid string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.sessions[sid]
	if s == nil || e.closing || s.Interaction != nil {
		return
	}
	if s.Status == "starting" || s.Status == "running" {
		s.Status = "idle"
	}
	for i := range s.Messages {
		if s.Messages[i].ID == mid && s.Messages[i].Status == "accepted" {
			s.Messages[i].Status = "complete"
		}
	}
	if err := e.saveLocked(s); err != nil {
		return
	}
	e.advanceIdleQueueLocked(sid)
}

func agentMessageKey(m map[string]any) string {
	if ts, ok := m["timestamp"].(float64); ok {
		return fmt.Sprintf("%s:%.0f", str(m["role"]), ts)
	}
	return ""
}
