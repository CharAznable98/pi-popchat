package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"pi-popchat/internal/agent"
)

type runtime struct {
	sequence    uint64
	progress    chan struct{}
	client      agent.Client
	start       sync.Mutex
	failed      bool
	stopped     bool
	assistantID string
	lastUsed    time.Time
}
type delivery struct {
	sid    string
	cancel context.CancelFunc
}
type Engine struct {
	deliveries  map[string]delivery
	committed   map[string][]byte
	wg          sync.WaitGroup
	mu          sync.Mutex
	store       *Store
	factory     agent.Factory
	executable  string
	sessions    map[string]*Session
	runtimes    map[string]*runtime
	selected    map[string]string
	hiddenAt    time.Time
	settings    Settings
	environment Environment
	version     uint64
	lastError   string
	closing     bool
	Changed     func()
	Notify      func(string, string, string)
}

func New(store *Store, factory agent.Factory, executable string) (*Engine, error) {
	all, err := store.Load()
	if err != nil {
		return nil, err
	}
	draft, err := store.LoadDraft()
	if err != nil {
		return nil, err
	}
	if draft != nil {
		all = append(all, draft)
	}
	e := &Engine{store: store, factory: factory, executable: executable, sessions: map[string]*Session{}, deliveries: map[string]delivery{}, committed: map[string][]byte{}, runtimes: map[string]*runtime{}, selected: map[string]string{}, settings: Settings{Shortcut: "Alt+Space"}, version: 1}
	_ = store.Get("settings", &e.settings)
	_ = store.Get("selected", &e.selected)
	_ = store.Get("hiddenAt", &e.hiddenAt)
	if e.settings.PiPath != "" {
		e.executable = e.settings.PiPath
	}
	e.environment = Environment{PiPath: e.executable, Available: e.executable != ""}
	for _, s := range all {
		// A draft has never run a submitted task; its persisted failure belongs
		// to preparation and may be cleared when reconnection succeeds.
		if s.DraftOnly && s.Status == "failed" {
			s.preparationFailure = s.Error
		}
		s.ModelsState = ""
		s.ModelsError = ""
		if busy(s.Status) {
			s.Status = "interrupted"
			s.Error = "上次执行已中断，请确认后继续。"
			s.Interaction = nil
		}
		for i := range s.Messages {
			if s.Messages[i].Status == "sending" || s.Messages[i].Status == "accepted" {
				s.Messages[i].Status = "uncertain"
			}
		}
		if len(s.Queue) > 0 {
			s.QueuePaused = true
			for i := range s.Queue {
				s.Queue[i].Status = "paused"
			}
		}
		e.sessions[s.ID] = s
		e.committed[s.ID], _ = json.Marshal(s)
		if err = store.Save(s); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// Call with e.mu held. A deletion keeps its record visible but excludes it
// from new operations while its Agent is being closed without the engine lock.
func (e *Engine) sessionLocked(sid string) *Session {
	s := e.sessions[sid]
	if s != nil && s.deleting {
		return nil
	}
	return s
}

func (e *Engine) Root() string { return e.store.Root }
func (e *Engine) changedLocked() {
	e.version++
	if e.Changed != nil {
		go e.Changed()
	}
}
func (e *Engine) saveLocked(s *Session) error {
	// All callers, including RPC completions, must still own a live session.
	// Delete invalidates it under the same mutex before any late completion can save.
	if e.sessionLocked(s.ID) != s {
		return errors.New("会话已删除或失效")
	}
	s.SearchableText = s.Title
	for _, m := range s.Messages {
		s.SearchableText += "\n" + m.Text
	}
	if err := e.store.Save(s); err != nil {
		if old := e.committed[s.ID]; old != nil {
			_ = json.Unmarshal(old, s)
		}
		e.lastError = "保存失败：" + err.Error()
		e.changedLocked()
		return err
	}
	e.committed[s.ID], _ = json.Marshal(s)
	e.lastError = ""
	e.changedLocked()
	return nil
}
func (e *Engine) Snapshot(view string) Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	v := Snapshot{Version: e.version, CurrentID: e.selected[view], Sessions: []*Session{}, Settings: e.settings, Environment: e.environment, Error: e.lastError}
	for _, s := range e.sessions {
		b, _ := json.Marshal(s)
		var cp Session
		_ = json.Unmarshal(b, &cp)
		cp.ManagedWorkspace = e.managedWorkspaceLocked(s)
		if cp.ID == v.CurrentID {
			v.Current = &cp
		}
		if cp.DraftOnly {
			continue
		}
		summary := cp
		summary.Messages = []Message{}
		summary.Queue = []Message{}
		summary.Models = []Model{}
		summary.Commands = []Command{}
		v.Sessions = append(v.Sessions, &summary)
	}
	sort.Slice(v.Sessions, func(i, j int) bool {
		a, b := v.Sessions[i], v.Sessions[j]
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		return a.UpdatedAt > b.UpdatedAt
	})
	return v
}
func (e *Engine) NewSession(view, cwd string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.newLocked(view, cwd)
}
func (e *Engine) newLocked(view, cwd string) (string, error) {
	if e.closing {
		return "", errors.New("应用正在退出")
	}
	for _, s := range e.sessions {
		if s.DraftOnly {
			if s.deleting {
				return "", errors.New("草稿正在删除，请稍后重试")
			}
			next := make(map[string]string, len(e.selected)+1)
			for key, value := range e.selected {
				next[key] = value
			}
			next[view] = s.ID
			if err := e.store.Set("selected", next); err != nil {
				return "", err
			}
			e.selected = next
			e.changedLocked()
			return s.ID, nil
		}
	}
	sid := id()
	source := "user"
	if cwd == "" {
		source = "managed"
		cwd = filepath.Join(e.store.Root, "workspaces", sid)
	} else {
		a, err := filepath.Abs(cwd)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(a)
		if err != nil || !info.IsDir() {
			return "", errors.New("请选择有效文件夹")
		}
		cwd = a
	}
	s := &Session{DraftOnly: true, CWDSource: source, ID: sid, Title: "新对话", CWD: cwd, CreatedAt: now(), UpdatedAt: now(), Status: "idle", SessionFile: filepath.Join(e.store.Root, "sessions", sid, "session.jsonl")}
	normalize(s)
	e.sessions[sid] = s
	if err := e.saveLocked(s); err != nil {
		delete(e.sessions, sid)
		return "", err
	}
	e.selected[view] = sid
	_ = e.store.Set("selected", e.selected)
	return sid, nil
}
func (e *Engine) Select(view, sid string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing {
		return errors.New("应用正在退出")
	}
	if e.sessionLocked(sid) == nil {
		return errors.New("会话不存在")
	}
	e.selected[view] = sid
	_ = e.store.Set("selected", e.selected)
	e.changedLocked()
	return nil
}
func (e *Engine) PanelShown() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.sessionLocked(e.selected["panel"])
	if s == nil || (!e.hiddenAt.IsZero() && time.Since(e.hiddenAt) > 30*time.Minute && !s.DraftOnly && !busy(s.Status)) {
		_, err := e.newLocked("panel", "")
		return err
	}
	return nil
}
func (e *Engine) PanelHidden() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing {
		return
	}
	e.hiddenAt = time.Now()
	_ = e.store.Set("hiddenAt", e.hiddenAt)
}
func (e *Engine) Transfer() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sessionLocked(e.selected["panel"]) == nil {
		return errors.New("没有浮窗会话")
	}
	e.selected["main"] = e.selected["panel"]
	_ = e.store.Set("selected", e.selected)
	e.changedLocked()
	return nil
}
func (e *Engine) CurrentID(view string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.selected[view]
}
func (e *Engine) SetEnvironment(env Environment) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing {
		return
	}
	e.environment = env
	e.executable = ""
	if env.Available {
		e.executable = env.PiPath
	}
	e.changedLocked()
}
func (e *Engine) SetSettings(s Settings) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.store.Set("settings", s); err != nil {
		return err
	}
	e.settings = s
	if s.PiPath != "" {
		e.executable = s.PiPath
	}
	e.changedLocked()
	return nil
}
func (e *Engine) Settings() Settings { e.mu.Lock(); defer e.mu.Unlock(); return e.settings }
func (e *Engine) ActiveCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, s := range e.sessions {
		if busy(s.Status) {
			n++
		}
	}
	return n
}
func (e *Engine) finishPreparation(s *Session, attempt uint64, wasDraft bool, c agent.Client, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Ignore obsolete attempts, deletion, shutdown, and draft promotion. Once
	// submitted, delivery owns execution failures and queue state.
	if s == nil || e.closing || e.sessionLocked(s.ID) != s || s.preparationAttempt != attempt || (wasDraft && !s.DraftOnly) {
		return
	}
	if err != nil && c == nil {
		s.Status = "failed"
		s.Error = err.Error()
		s.QueuePaused = !s.DraftOnly
		if s.DraftOnly {
			s.preparationFailure = s.Error
		}
		_ = e.saveLocked(s)
	} else if err == nil && s.DraftOnly && s.Status == "failed" && s.preparationFailure != "" && s.Error == s.preparationFailure {
		r := e.runtimes[s.ID]
		if r == nil || r.client != c {
			return
		}
		s.Status = "idle"
		s.Error = ""
		s.QueuePaused = false
		if e.saveLocked(s) == nil {
			s.preparationFailure = ""
		}
	}
}
func (e *Engine) ensure(sid string) (agent.Client, error) {
	return e.ensureMetadata(sid, false)
}
func (e *Engine) ensureMetadata(sid string, refresh bool) (agent.Client, error) {
	return e.ensureMetadataAttempt(sid, refresh, nil)
}

func (e *Engine) ensureMetadataAttempt(sid string, refresh bool, begin func(*Session)) (agent.Client, error) {
	e.mu.Lock()
	s := e.sessionLocked(sid)
	if s == nil || e.closing {
		e.mu.Unlock()
		return nil, errors.New("会话不可用")
	}
	r := e.runtimes[sid]
	if r == nil {
		r = &runtime{progress: make(chan struct{})}
		e.runtimes[sid] = r
	}
	e.mu.Unlock()
	r.start.Lock()
	defer r.start.Unlock()
	e.mu.Lock()
	s = e.sessionLocked(sid)
	if s == nil || e.closing || e.runtimes[sid] != r {
		e.mu.Unlock()
		return nil, errors.New("会话或 Agent 已失效")
	}
	// Number actual serialized attempts, not goroutine scheduling order.
	if begin != nil {
		begin(s)
	}
	if r.client != nil {
		c := r.client
		r.lastUsed = time.Now()
		e.mu.Unlock()
		if refresh {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return c, e.loadModels(ctx, sid, c)
		}
		return c, nil
	}
	s = e.sessionLocked(sid)
	if s == nil || e.closing {
		e.mu.Unlock()
		return nil, errors.New("应用正在退出")
	}
	managed := e.managedWorkspaceLocked(s)
	cfg := agent.Config{Executable: e.executable, CWD: s.CWD, SessionDir: filepath.Join(e.store.Root, "sessions", sid), SessionFile: s.SessionFile}
	e.mu.Unlock()
	if cfg.Executable == "" {
		return nil, errors.New("未找到 Pi，请安装并配置后重新检测")
	}
	if managed {
		if err := os.MkdirAll(cfg.CWD, 0700); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(cfg.SessionDir, 0700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := e.factory.Start(ctx, cfg)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	if e.closing || e.sessionLocked(sid) == nil || e.runtimes[sid] != r {
		e.mu.Unlock()
		c.Close()
		return nil, errors.New("应用正在退出")
	}
	r.client = c
	r.lastUsed = time.Now()
	e.mu.Unlock()
	go e.consume(sid, r, c)
	for _, cmd := range []string{"get_state", "get_available_models", "get_commands", "get_messages"} {
		if cmd == "get_messages" {
			if reader, ok := c.(agent.HistoryReader); ok {
				messages, readErr := reader.ReadHistory(ctx)
				if errors.Is(readErr, agent.ErrHistoryMissing) {
					e.mu.Lock()
					hasHistory := false
					if current := e.sessionLocked(sid); current != nil {
						for _, m := range current.Messages {
							if m.AgentKey != "" || m.Role == "assistant" {
								hasHistory = true
								break
							}
						}
					}
					e.mu.Unlock()
					if !hasHistory {
						readErr = nil
						messages = []map[string]any{}
					}
				}
				if readErr != nil {
					// Invalidate before closing: queued preparations must start a fresh
					// client, and events from this failed initialization must be ignored.
					e.mu.Lock()
					if e.runtimes[sid] == r && r.client == c {
						r.client = nil
					}
					e.mu.Unlock()
					c.Close()
					return nil, readErr
				}
				items := make([]any, len(messages))
				for i, m := range messages {
					items[i] = m
				}
				e.applyMetadata(sid, cmd, map[string]any{"data": map[string]any{"messages": items}})
				continue
			}
		}
		if cmd == "get_available_models" {
			_ = e.loadModels(ctx, sid, c)
			continue
		}
		res, er := c.Request(ctx, map[string]any{"type": cmd})
		if er != nil {
			continue
		}
		e.applyMetadata(sid, cmd, res)
	}
	return c, nil
}

// Model discovery has a lifecycle separate from conversation execution.
func (e *Engine) loadModels(ctx context.Context, sid string, c agent.Client) error {
	e.mu.Lock()
	if s := e.sessionLocked(sid); s != nil {
		s.ModelsState = "loading"
		s.ModelsError = ""
		e.changedLocked()
	}
	e.mu.Unlock()
	res, err := c.Request(ctx, map[string]any{"type": "get_available_models"})
	if err == nil {
		e.applyMetadata(sid, "get_available_models", res)
		return nil
	}
	e.mu.Lock()
	if s := e.sessionLocked(sid); s != nil {
		s.ModelsState = "error"
		s.ModelsError = err.Error()
		e.changedLocked()
	}
	e.mu.Unlock()
	return err
}

// Prepare restores a session if necessary and otherwise reuses its Agent and
// metadata. Window visibility must not force model discovery on a live client.
func (e *Engine) Prepare(sid string) error {
	return e.prepareMetadata(sid, false)
}

// Refresh explicitly updates the model catalog, including on an existing client.
func (e *Engine) Refresh(sid string) error {
	return e.prepareMetadata(sid, true)
}

func (e *Engine) prepareMetadata(sid string, refresh bool) error {
	e.mu.Lock()
	s := e.sessionLocked(sid)
	wasDraft := s != nil && s.DraftOnly
	e.mu.Unlock()
	var attempt uint64
	c, err := e.ensureMetadataAttempt(sid, refresh, func(current *Session) {
		if current == s {
			s.preparationAttempt++
			attempt = s.preparationAttempt
		}
	})
	if attempt != 0 {
		e.finishPreparation(s, attempt, wasDraft, c, err)
	}
	return err
}
func (e *Engine) Send(sid, text, clientID string, attachments []Attachment) error {
	return e.SendWithRevision(sid, text, clientID, attachments, nil)
}

func (e *Engine) SendWithRevision(sid, text, clientID string, attachments []Attachment, revision *uint64) error {
	text = strings.TrimSpace(text)
	if text == "" && len(attachments) == 0 {
		return errors.New("请输入消息")
	}
	if len(text) > 200000 {
		return errors.New("消息过长，请以文件发送")
	}
	if clientID == "" {
		return errors.New("消息缺少唯一标识")
	}
	e.mu.Lock()
	s := e.sessionLocked(sid)
	if s == nil || e.closing {
		e.mu.Unlock()
		return errors.New("会话不可用")
	}
	for _, m := range append(append([]Message{}, s.Messages...), s.Queue...) {
		if m.ID == clientID {
			e.mu.Unlock()
			return nil
		}
	}
	if revision != nil && s.DraftRevision != *revision {
		e.mu.Unlock()
		return errors.New("另一窗口已更新或发送草稿，请核对当前输入后重试")
	}
	var imageBytes int64
	for _, a := range attachments {
		if err := e.validateAttachment(a); err != nil {
			e.mu.Unlock()
			return err
		}
		if strings.HasPrefix(a.MIME, "image/") {
			info, statErr := os.Stat(a.Path)
			if statErr != nil {
				e.mu.Unlock()
				return errors.New("附件已不可访问，请重新添加")
			}
			imageBytes += info.Size()
		}
	}
	if imageBytes > MaxMessageImageBytes {
		e.mu.Unlock()
		return errors.New("每条消息的图片总大小不能超过 16 MB，请减少图片或压缩后发送")
	}
	m := Message{ID: clientID, Role: "user", Text: text, Status: "pending", CreatedAt: now(), Attachments: attachments}
	if m.Attachments == nil {
		m.Attachments = []Attachment{}
	}
	if s.DraftOnly {
		s.CreatedAt = now()
		s.QueuePaused = false
	}
	s.DraftOnly = false
	s.Draft = ""
	s.DraftRevision++
	s.DraftAttachments = []Attachment{}
	s.UpdatedAt = now()
	if s.Title == "新对话" {
		r := []rune(text)
		if len(r) > 32 {
			r = r[:32]
		}
		s.Title = string(r)
		if s.Title == "" {
			s.Title = "附件对话"
		}
	}
	if busy(s.Status) || s.QueuePaused || len(s.Queue) > 0 {
		s.Queue = append(s.Queue, m)
		if s.QueuePaused {
			s.Queue[len(s.Queue)-1].Status = "paused"
		}
		err := e.saveLocked(s)
		e.mu.Unlock()
		return err
	}
	m.Status = "sending"
	s.Messages = append(s.Messages, m)
	s.Status = "starting"
	s.Error = ""
	if err := e.saveLocked(s); err != nil {
		e.mu.Unlock()
		return err
	}
	e.scheduleLocked(sid, m, "prompt")
	e.mu.Unlock()
	return nil
}
func (e *Engine) scheduleLocked(sid string, m Message, kind string) {
	if e.closing {
		return
	}
	if kind == "prompt" {
		if r := e.runtimes[sid]; r != nil {
			r.stopped = false
			r.failed = false
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	e.deliveries[m.ID] = delivery{sid: sid, cancel: cancel}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer cancel()
		defer func() {
			e.mu.Lock()
			delete(e.deliveries, m.ID)
			e.advanceIdleQueueLocked(sid)
			e.mu.Unlock()
		}()
		e.deliver(ctx, sid, m, kind)
	}()
}

// settled can arrive while a submission is still completing its state query.
// The last submission must release that reservation and reconsider the queue.
func (e *Engine) advanceIdleQueueLocked(sid string) {
	s := e.sessionLocked(sid)
	if e.closing || s == nil || s.Status != "idle" || s.QueuePaused || len(s.Queue) == 0 {
		return
	}
	for _, d := range e.deliveries {
		if d.sid == sid {
			return
		}
	}
	m := s.Queue[0]
	s.Queue = s.Queue[1:]
	m.Status = "sending"
	s.Messages = append(s.Messages, m)
	s.Status = "starting"
	if e.saveLocked(s) == nil {
		e.scheduleLocked(sid, m, "prompt")
	} else {
		// The persisted queue was restored by saveLocked. Make the stopped
		// dispatch visible even if storage is still unavailable.
		s.QueuePaused = true
		e.changedLocked()
	}
}
func (e *Engine) deliver(ctx context.Context, sid string, m Message, kind string) {
	if ctx.Err() != nil {
		return
	}
	c, err := e.ensure(sid)
	if err != nil {
		if ctx.Err() == nil {
			e.deliveryError(sid, m.ID, err)
		}
		return
	}
	e.mu.Lock()
	current := e.sessionLocked(sid)
	if current == nil || e.closing || ctx.Err() != nil || current.Status == "stopped" || current.Status == "interrupted" {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()
	cmd := map[string]any{"type": kind, "message": m.Text}
	if kind == "steer" {
		cmd["type"] = "prompt"
		cmd["streamingBehavior"] = "steer"
	}
	images := []map[string]any{}
	remainingImageBytes := int64(MaxMessageImageBytes)
	for _, a := range m.Attachments {
		if er := e.validateAttachment(a); er != nil {
			e.deliveryError(sid, m.ID, er)
			return
		}
		if strings.HasPrefix(a.MIME, "image/") {
			f, er := os.Open(a.Path)
			var b []byte
			if er == nil {
				b, er = io.ReadAll(io.LimitReader(f, remainingImageBytes+1))
				f.Close()
				if int64(len(b)) > remainingImageBytes {
					er = errors.New("图片总大小已超过 16 MB，请重新添加附件")
				}
			}
			if er != nil {
				e.deliveryError(sid, m.ID, er)
				return
			}
			remainingImageBytes -= int64(len(b))
			images = append(images, map[string]any{"type": "image", "mimeType": a.MIME, "data": base64.StdEncoding.EncodeToString(b)})
		} else {
			cmd["message"] = fmt.Sprint(cmd["message"]) + "\n\n附件 " + a.Name + "（本地路径）：" + a.Path
		}
	}
	if len(images) > 0 {
		cmd["images"] = images
	}
	_, err = c.Request(ctx, cmd)
	if err != nil {
		if ctx.Err() == nil {
			e.deliveryError(sid, m.ID, err)
		}
		return
	}
	e.mu.Lock()
	if s := e.sessionLocked(sid); s != nil && !e.closing {
		for i := range s.Messages {
			if s.Messages[i].ID == m.ID && s.Messages[i].Status == "sending" {
				s.Messages[i].Status = "accepted"
			}
		}
		_ = e.saveLocked(s)
	}
	e.mu.Unlock()
	stateCtx, stateCancel := context.WithTimeout(context.Background(), 10*time.Second)
	state, stateErr := c.Request(stateCtx, map[string]any{"type": "get_state"})
	stateCancel()
	if stateErr == nil {
		if err := e.awaitEvents(ctx, sid, state); err != nil {
			return
		}
		e.applyMetadata(sid, "get_state", state)
		if streaming, ok := obj(state["data"])["isStreaming"].(bool); ok && !streaming {
			e.finishCommand(sid, m.ID)
		}
	}
}
func (e *Engine) deliveryError(sid, mid string, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.sessionLocked(sid)
	if s == nil {
		return
	}
	for i := range s.Messages {
		if s.Messages[i].ID == mid && s.Messages[i].Status != "complete" {
			s.Messages[i].Status = "uncertain"
		}
	}
	s.Status = "failed"
	s.Error = err.Error()
	s.QueuePaused = true
	_ = e.saveLocked(s)
}
func (e *Engine) Draft(sid, text string, expected *string) error {
	return e.DraftWithAttachments(sid, text, expected, nil)
}
func (e *Engine) DraftWithAttachments(sid, text string, expected *string, attachments []Attachment) error {
	return e.DraftWithRevision(sid, text, expected, attachments, nil)
}
func (e *Engine) DraftWithRevision(sid, text string, expected *string, attachments []Attachment, revision *uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.sessionLocked(sid)
	if s == nil {
		return errors.New("会话不存在")
	}
	if (expected != nil && s.Draft != *expected) || (revision != nil && s.DraftRevision != *revision) {
		return errors.New("另一窗口已更新草稿，请保留当前输入后重试")
	}
	if attachments != nil {
		for _, a := range attachments {
			if err := e.validateAttachment(a); err != nil {
				return err
			}
		}
		s.DraftAttachments = attachments
	}
	s.Draft = text
	s.DraftRevision++
	return e.saveLocked(s)
}
func (e *Engine) Rename(sid, title string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.sessionLocked(sid)
	if s == nil {
		return errors.New("会话不存在")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("标题不能为空")
	}
	s.Title = title
	return e.saveLocked(s)
}
func (e *Engine) Pin(sid string, pin bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.sessionLocked(sid)
	if s == nil {
		return errors.New("会话不存在")
	}
	s.Pinned = pin
	return e.saveLocked(s)
}
func (e *Engine) SetCWD(sid, cwd string) error {
	e.mu.Lock()
	// Validate under the deletion lock so a successful choice cannot refer to
	// a workspace removed after validation but before updating the session.
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		e.mu.Unlock()
		return errors.New("请选择有效文件夹")
	}
	s := e.sessionLocked(sid)
	if s == nil || len(s.Messages) > 0 || busy(s.Status) {
		e.mu.Unlock()
		return errors.New("已有消息的会话不能更换工作目录，请新建会话")
	}
	s.CWD = cwd
	s.CWDSource = "user"
	r := e.runtimes[sid]
	var client agent.Client
	if r != nil {
		client = r.client
	}
	delete(e.runtimes, sid)
	err = e.saveLocked(s)
	e.mu.Unlock()
	if client != nil {
		closeErr := client.Close()
		if err == nil {
			err = closeErr
		}
	}
	return err
}
func (e *Engine) Delete(sid string) error { return e.DeleteWithWorkspace(sid, false) }

func (e *Engine) DeleteWithWorkspace(sid string, removeWorkspace bool) error {
	e.mu.Lock()
	s := e.sessions[sid]
	if s == nil {
		e.mu.Unlock()
		return nil
	}
	if e.closing || s.deleting {
		e.mu.Unlock()
		return errors.New("会话正在删除或应用正在退出")
	}
	if busy(s.Status) {
		e.mu.Unlock()
		return errors.New("请先停止任务再删除会话")
	}
	var expected os.FileInfo
	var err error
	if removeWorkspace {
		expected, err = e.validateWorkspaceRemovalLocked(s)
		if err != nil {
			e.mu.Unlock()
			return err
		}
	}
	r := e.runtimes[sid]
	var client agent.Client
	if r != nil {
		if removeWorkspace {
			if !r.start.TryLock() {
				e.mu.Unlock()
				return errors.New("Agent 正在准备，请稍后重试删除")
			}
		}
		client = r.client
		delete(e.runtimes, sid)
		if removeWorkspace {
			r.start.Unlock()
		}
	}
	s.deleting = true
	e.wg.Add(1)
	e.mu.Unlock()
	defer e.wg.Done()
	// Closing can wait for process termination. Other windows must stay usable.
	var closeErr error
	if client != nil {
		closeErr = client.Close()
	}
	e.mu.Lock()
	staged, err := func() (string, error) {
		defer e.mu.Unlock()
		defer func() { s.deleting = false; e.changedLocked() }()
		if closeErr != nil {
			return "", closeErr
		}
		if e.closing {
			return "", errors.New("应用正在退出")
		}
		staged := ""
		if removeWorkspace {
			// Directory references and external filesystem identity may change while
			// Close waits, so validate both again before staging.
			current, err := e.validateWorkspaceRemovalLocked(s)
			if err != nil {
				return "", err
			}
			if current != nil && (expected == nil || !os.SameFile(expected, current)) {
				return "", errors.New("工作目录已被替换，已停止删除并保留会话")
			}
			staged, err = stageWorkspaceForRemoval(s.CWD, e.store.Root, sid, expected)
			if err != nil {
				return "", err
			}
		}
		if err := e.store.Delete(sid); err != nil {
			if staged != "" {
				if restoreErr := os.Rename(staged, s.CWD); restoreErr != nil {
					err = fmt.Errorf("删除会话失败：%w；恢复目录失败，文件仍在 %s：%v", err, staged, restoreErr)
				}
			}
			return "", err
		}
		delete(e.sessions, sid)
		delete(e.committed, sid)
		for view, selected := range e.selected {
			if selected == sid {
				delete(e.selected, view)
			}
		}
		_ = e.store.Set("selected", e.selected)
		return staged, nil
	}()
	if err != nil {
		return err
	}
	if staged != "" {
		if err := removeStagedWorkspace(staged, expected); err != nil {
			return fmt.Errorf("会话已删除，但工作文件未完全清理，请在 %s 手动处理：%w", staged, err)
		}
	}
	return nil
}
func (e *Engine) QueueAction(sid, action, mid string) error {
	if action != "pauseQueue" && action != "resumeQueue" && action != "removeQueued" && action != "insert" {
		return errors.New("未知队列操作")
	}
	e.mu.Lock()
	s := e.sessionLocked(sid)
	if s == nil {
		e.mu.Unlock()
		return errors.New("会话不存在")
	}
	if action == "pauseQueue" {
		s.QueuePaused = true
		err := e.saveLocked(s)
		e.mu.Unlock()
		return err
	}
	if action == "resumeQueue" {
		s.QueuePaused = false
		for i := range s.Queue {
			s.Queue[i].Status = "pending"
		}
		if !busy(s.Status) && len(s.Queue) > 0 {
			m := s.Queue[0]
			s.Queue = s.Queue[1:]
			m.Status = "sending"
			s.Messages = append(s.Messages, m)
			s.Status = "starting"
			s.Error = ""
			err := e.saveLocked(s)
			if err == nil {
				e.scheduleLocked(sid, m, "prompt")
			}
			e.mu.Unlock()
			return err
		}
		err := e.saveLocked(s)
		e.mu.Unlock()
		return err
	}
	idx := -1
	for i, m := range s.Queue {
		if m.ID == mid {
			idx = i
			break
		}
	}
	if idx < 0 {
		e.mu.Unlock()
		return errors.New("消息已提交或不存在")
	}
	m := s.Queue[idx]
	s.Queue = append(s.Queue[:idx], s.Queue[idx+1:]...)
	if action == "removeQueued" {
		err := e.saveLocked(s)
		e.mu.Unlock()
		return err
	}
	kind := "steer"
	if !busy(s.Status) {
		kind = "prompt"
		s.Status = "starting"
	}
	m.Status = "sending"
	s.Messages = append(s.Messages, m)
	err := e.saveLocked(s)
	if err == nil {
		e.scheduleLocked(sid, m, kind)
	}
	e.mu.Unlock()
	return err
}
func (e *Engine) Stop(sid string) error {
	e.mu.Lock()
	s := e.sessionLocked(sid)
	r := e.runtimes[sid]
	if s == nil {
		e.mu.Unlock()
		return errors.New("会话不存在")
	}
	s.QueuePaused = true
	pendingInteraction := s.Interaction
	s.Status = "stopped"
	for _, d := range e.deliveries {
		if d.sid == sid {
			d.cancel()
		}
	}
	var client agent.Client
	if r != nil {
		client = r.client
		r.stopped = true
	}
	s.Interaction = nil
	e.mu.Unlock()
	if client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if pendingInteraction != nil {
			_, _ = client.Request(ctx, map[string]any{"type": "extension_ui_response", "id": pendingInteraction.ID, "cancelled": true})
		}
		if res, err := client.Request(ctx, map[string]any{"type": "abort"}); err != nil {
			_ = client.Close()
		} else {
			_ = e.awaitEvents(ctx, sid, res)
		}
	}
	e.mu.Lock()
	if e.sessionLocked(sid) != s || e.closing {
		e.mu.Unlock()
		return errors.New("会话已删除或失效")
	}
	s.Status = "stopped"
	err := e.saveLocked(s)
	e.mu.Unlock()
	return err
}
func (e *Engine) Respond(sid, rid string, value any, cancelled bool) error {
	e.mu.Lock()
	s := e.sessionLocked(sid)
	r := e.runtimes[sid]
	if s == nil || s.Interaction == nil || s.Interaction.ID != rid || r == nil || r.client == nil {
		e.mu.Unlock()
		return errors.New("问题已结束，请刷新会话")
	}
	method := s.Interaction.Method
	c := r.client
	e.mu.Unlock()
	cmd := map[string]any{"type": "extension_ui_response", "id": rid}
	if cancelled {
		cmd["cancelled"] = true
	} else if method == "confirm" {
		cmd["confirmed"] = value
	} else {
		cmd["value"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.Request(ctx, cmd); err != nil {
		return err
	}
	e.mu.Lock()
	if e.sessionLocked(sid) != s || e.closing {
		e.mu.Unlock()
		return errors.New("会话已删除或失效")
	}
	if s.Interaction != nil && s.Interaction.ID == rid {
		s.Interaction = nil
		s.Status = "running"
	}
	err := e.saveLocked(s)
	e.mu.Unlock()
	return err
}
func (e *Engine) SetModel(sid, provider, model string) error {
	e.mu.Lock()
	s := e.sessionLocked(sid)
	if s == nil || busy(s.Status) {
		e.mu.Unlock()
		return errors.New("请在当前任务结束后切换模型")
	}
	e.mu.Unlock()
	c, err := e.ensure(sid)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := c.Request(ctx, map[string]any{"type": "set_model", "provider": provider, "modelId": model})
	if err != nil {
		return err
	}
	_ = res
	e.mu.Lock()
	if e.sessionLocked(sid) != s || e.closing {
		e.mu.Unlock()
		return errors.New("会话已删除或失效")
	}
	s.Model = model
	s.Provider = provider
	err = e.saveLocked(s)
	e.mu.Unlock()
	return err
}
func (e *Engine) Close() error {
	e.mu.Lock()
	e.closing = true
	for _, d := range e.deliveries {
		d.cancel()
	}
	clients := []agent.Client{}
	for _, r := range e.runtimes {
		if r.client != nil {
			clients = append(clients, r.client)
			r.client = nil
		}
	}
	for _, s := range e.sessions {
		if busy(s.Status) {
			s.Status = "interrupted"
			s.Interaction = nil
		}
		if len(s.Queue) > 0 {
			s.QueuePaused = true
		}
		_ = e.saveLocked(s)
	}
	e.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
	e.wg.Wait()
	return e.store.Close()
}
func (e *Engine) ReapIdle(age time.Duration) {
	e.mu.Lock()
	clients := []agent.Client{}
	for sid, r := range e.runtimes {
		s := e.sessionLocked(sid)
		if s != nil && r.client != nil && !busy(s.Status) && len(s.Queue) == 0 && time.Since(r.lastUsed) > age {
			clients = append(clients, r.client)
			r.client = nil
			delete(e.runtimes, sid)
		}
	}
	e.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
}

func (e *Engine) awaitEvents(ctx context.Context, sid string, response map[string]any) error {
	target := uint64(0)
	switch n := response["_eventSequence"].(type) {
	case float64:
		target = uint64(n)
	case uint64:
		target = n
	case int:
		target = uint64(n)
	}
	for {
		e.mu.Lock()
		r := e.runtimes[sid]
		if r == nil || r.sequence >= target || target == 0 {
			e.mu.Unlock()
			return nil
		}
		ch := r.progress
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}
