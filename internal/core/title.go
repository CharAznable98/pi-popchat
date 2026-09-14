package core

import (
	"context"
	"errors"
	"path/filepath"
	"pi-popchat/internal/agent"
	"strings"
	"time"
)

// refreshTitleFile snapshots identity under the lock, performs bounded I/O
// outside it, and discards results for deleted/replaced sessions or newer titles.
func (e *Engine) refreshTitleFile(sid string, restoreTitle bool) {
	e.mu.Lock()
	reader, ok := e.factory.(agent.SessionInfoReader)
	if !ok {
		e.mu.Unlock()
		return
	}
	s := e.sessionLocked(sid)
	if s == nil || e.closing {
		e.mu.Unlock()
		return
	}
	cfg := agent.Config{SessionDir: filepath.Join(e.store.Root, "sessions", sid), SessionFile: s.SessionFile}
	revision := s.titleRevision
	s.titleReadRevision++
	readRevision := s.titleReadRevision
	ctx, cancel := context.WithTimeout(e.titleContext, 2*time.Second)
	e.mu.Unlock()
	defer cancel()
	type result struct {
		name string
		err  error
	}
	done := make(chan result, 1)
	go func() { name, err := reader.ReadSessionInfo(ctx, cfg); done <- result{name, err} }()
	var value result
	select {
	case value = <-done:
	case <-ctx.Done():
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	current := e.sessionLocked(sid)
	if e.closing || current != s || s.SessionFile != cfg.SessionFile || s.titleReadRevision != readRevision {
		return
	}
	s.TitleWritable = value.err == nil
	if restoreTitle && value.err == nil && revision == 0 && s.titleRevision == revision {
		s.AgentTitle = value.name
		s.Title = value.name
		if s.Title == "" {
			s.Title = fallbackTitle(s)
		}
		s.titleRevision++
	}
	_ = e.saveLocked(s)
}

// Call with e.mu held so shutdown cannot race WaitGroup.Add.
func (e *Engine) refreshTitleAsyncLocked(sid string, restoreTitle bool) {
	if _, ok := e.factory.(agent.SessionInfoReader); !ok {
		return
	}
	if e.closing {
		return
	}
	e.wg.Add(1)
	go func() { defer e.wg.Done(); e.refreshTitleFile(sid, restoreTitle) }()
}
func (e *Engine) Rename(sid, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("标题不能为空")
	}
	e.refreshTitleFile(sid, false)
	e.mu.Lock()
	s := e.sessionLocked(sid)
	if s == nil {
		e.mu.Unlock()
		return errors.New("会话不存在")
	}
	writable := s.TitleWritable
	e.mu.Unlock()
	if !writable {
		return errors.New("Pi 尚未保存此会话，首次回复后可重命名")
	}
	c, err := e.ensure(sid)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := c.Request(ctx, map[string]any{"type": "set_session_name", "name": title})
	if err != nil {
		return err
	}
	if err = e.awaitEvents(ctx, sid, res); err != nil {
		return err
	}
	res, err = c.Request(ctx, map[string]any{"type": "get_state"})
	if err != nil {
		return err
	}
	if err = e.awaitEvents(ctx, sid, res); err != nil {
		return err
	}
	e.applyMetadata(sid, "get_state", res)
	return nil
}
