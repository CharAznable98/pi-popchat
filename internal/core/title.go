package core

import (
	"context"
	"errors"
	"path/filepath"
	"pi-popchat/internal/agent"
	"strings"
	"time"
)

func (e *Engine) refreshTitleFileLocked(s *Session) {
	reader, ok := e.factory.(agent.SessionInfoReader)
	if !ok {
		return
	}
	name, err := reader.ReadSessionInfo(context.Background(), agent.Config{SessionDir: filepath.Join(e.store.Root, "sessions", s.ID), SessionFile: s.SessionFile})
	s.TitleWritable = err == nil
	if err == nil {
		s.AgentTitle = name
		s.Title = name
		if name == "" {
			s.Title = fallbackTitle(s)
		}
	}
}
func (e *Engine) Rename(sid, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("标题不能为空")
	}
	e.mu.Lock()
	s := e.sessionLocked(sid)
	if s == nil {
		e.mu.Unlock()
		return errors.New("会话不存在")
	}
	e.refreshTitleFileLocked(s)
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
