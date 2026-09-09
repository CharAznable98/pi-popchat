package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"pi-popchat/internal/agent/pi"
	"pi-popchat/internal/core"
)

type Desktop struct {
	mainWanted, panelWanted  bool
	panelActivationPending   bool
	panelActivationScreen    *application.Screen
	done                     chan struct{}
	app                      *application.App
	main, panel              *application.WebviewWindow
	engine                   *core.Engine
	mu                       sync.Mutex
	shortcut, errorText      string
	quitApproved, quitPrompt bool
	detectMu                 sync.Mutex
}

func (d *Desktop) setError(s string) {
	d.mu.Lock()
	d.errorText = s
	d.mu.Unlock()
	d.app.Event.Emit("popchat:changed")
}
func (d *Desktop) Snapshot(view string) core.Snapshot {
	s := d.engine.Snapshot(view)
	d.mu.Lock()
	if d.errorText != "" {
		s.Error = d.errorText
	}
	d.mu.Unlock()
	return s
}
func (d *Desktop) SaveAttachment(name, mime, data string) (core.Attachment, error) {
	return d.engine.SaveAttachment(name, mime, data)
}
func (d *Desktop) Action(view, action string, p map[string]any) (core.Snapshot, error) {
	select {
	case <-d.done:
		return core.Snapshot{}, errors.New("应用正在退出")
	default:
	}
	if view != "main" && view != "panel" {
		return core.Snapshot{}, errors.New("未知窗口")
	}
	if p == nil {
		p = map[string]any{}
	}
	str := func(k string) string { s, _ := p[k].(string); return s }
	sid := d.engine.CurrentID(view)
	if str("sessionId") != "" {
		sid = str("sessionId")
	}
	if (action == "send" || action == "draft") && str("id") != "" {
		sid = str("id")
	}
	var err error
	switch action {
	case "new":
		_, err = d.engine.NewSession(view, str("cwd"))
	case "select":
		err = d.engine.Select(view, str("id"))
		if err == nil {
			go d.engine.Refresh(str("id"))
		}
	case "send":
		var attachments []core.Attachment
		attachments, err = decodeAttachments(p["attachments"])
		if err != nil {
			return d.Snapshot(view), err
		}
		mid := str("clientMessageId")
		if mid == "" {
			mid = str("clientMessageID")
		}
		if mid == "" {
			mid = str("messageId")
		}
		if mid == "" {
			mid = str("clientId")
		}
		err = d.engine.Send(sid, str("text"), mid, attachments)
	case "draft":
		var expected *string
		if x, ok := p["expectedDraft"].(string); ok {
			expected = &x
		}
		if x, ok := p["expected"].(string); ok {
			expected = &x
		}
		attachments, parseErr := decodeAttachments(p["attachments"])
		if parseErr != nil {
			return d.Snapshot(view), parseErr
		}
		var revision *uint64
		if n, ok := p["expectedDraftRevision"].(float64); ok {
			v := uint64(n)
			revision = &v
		}
		err = d.engine.DraftWithRevision(sid, str("text"), expected, attachments, revision)
	case "stop":
		err = d.engine.Stop(sid)
	case "insert", "removeQueued", "resumeQueue", "pauseQueue":
		err = d.engine.QueueAction(sid, action, str("messageId"))
	case "rename":
		err = d.engine.Rename(str("id"), str("title"))
	case "pin":
		v, _ := p["pinned"].(bool)
		err = d.engine.Pin(str("id"), v)
	case "delete":
		err = d.engine.Delete(str("id"))
	case "respond":
		cancelled, _ := p["cancelled"].(bool)
		err = d.engine.Respond(sid, str("requestId"), p["value"], cancelled)
	case "model":
		err = d.engine.SetModel(sid, str("provider"), str("id"))
	case "refreshAgent":
		d.refreshEnvironment()
		if d.engine.Snapshot(view).Environment.Available {
			err = d.engine.Refresh(sid)
		}
	case "settings":
		old := d.engine.Settings()
		next := old
		if _, ok := p["shortcut"]; ok {
			next.Shortcut = str("shortcut")
		}
		if _, ok := p["piPath"]; ok {
			next.PiPath = str("piPath")
		}
		if next.Shortcut != old.Shortcut {
			err = d.setShortcut(next.Shortcut)
		}
		if err == nil {
			err = d.engine.SetSettings(next)
			if err != nil {
				_ = d.setShortcut(old.Shortcut)
			} else {
				go d.refreshEnvironment()
			}
		}
	case "chooseDirectory":
		var path string
		path, err = d.app.Dialog.OpenFile().CanChooseDirectories(true).CanChooseFiles(false).SetTitle("选择会话工作目录").PromptForSingleSelection()
		if err == nil && path != "" {
			err = d.engine.SetCWD(sid, path)
		}
	case "openFile", "revealFile":
		err = d.openPath(view, str("path"), action == "revealFile")
	case "openURL":
		err = openWebURL(str("url"))
	case "transfer":
		err = d.engine.Transfer()
		if err == nil {
			d.hidePanel()
			d.showMain()
		}
	case "hide":
		if view == "panel" {
			d.hidePanel()
		} else {
			d.hideMain()
		}
	case "showMain":
		d.showMain()
	case "quit":
		go d.app.Quit()
	default:
		err = fmt.Errorf("未知操作：%s", action)
	}
	return d.Snapshot(view), err
}
func (d *Desktop) hidePanel() {
	application.InvokeSync(func() {
		d.mu.Lock()
		wasWanted := d.panelWanted
		d.panelWanted = false
		d.panelActivationPending = false
		d.panelActivationScreen = nil
		d.mu.Unlock()
		if wasWanted || d.panel.IsVisible() {
			d.engine.PanelHidden()
		}
		d.panel.Hide()
	})
}
func (d *Desktop) showPanel() {
	d.showPanelOn(activeWindowScreen())
}
func (d *Desktop) showPanelOn(screen *application.Screen) {
	if err := d.engine.PanelShown(); err != nil {
		d.setError(err.Error())
		return
	}
	application.InvokeSync(func() {
		d.mu.Lock()
		d.panelWanted = true
		d.panelActivationPending = !nativeApplicationIsActive()
		d.panelActivationScreen = screen
		d.mu.Unlock()
		if screen != nil {
			d.panel.SetScreen(screen)
		}
		d.panel.Show()
		d.panel.Focus()
		d.panel.ExecJS("document.querySelector('textarea')?.focus()")
	})
	sid := d.engine.CurrentID("panel")
	go d.engine.Refresh(sid)
}
func (d *Desktop) showMain() {
	application.InvokeSync(func() {
		d.mu.Lock()
		d.mainWanted = true
		d.panelActivationPending = false
		d.panelActivationScreen = nil
		d.mu.Unlock()
		d.main.Show()
		d.main.Focus()
	})
}

// AppKit may restore the main window as key while activating the application.
// Complete only an explicit panel invocation, using its original target screen.
func (d *Desktop) completePanelActivation() {
	application.InvokeSync(func() {
		select {
		case <-d.done:
			return
		default:
		}
		d.mu.Lock()
		pending := d.panelActivationPending && d.panelWanted
		screen := d.panelActivationScreen
		d.panelActivationPending = false
		d.panelActivationScreen = nil
		d.mu.Unlock()
		if !pending {
			return
		}
		if screen != nil {
			d.panel.SetScreen(screen)
		}
		d.panel.Focus()
		d.panel.ExecJS("document.querySelector('textarea')?.focus()")
	})
}
func (d *Desktop) hideMain() {
	application.InvokeSync(func() { d.mu.Lock(); d.mainWanted = false; d.mu.Unlock(); d.main.Hide() })
}
func (d *Desktop) applyVisibility(view string) {
	application.InvokeSync(func() {
		select {
		case <-d.done:
			return
		default:
		}
		d.mu.Lock()
		wanted := d.mainWanted
		w := d.main
		if view == "panel" {
			wanted = d.panelWanted
			w = d.panel
		}
		d.mu.Unlock()
		if wanted {
			w.Show()
		} else {
			w.Hide()
		}
	})
}

func (d *Desktop) togglePanel() {
	target := activeWindowScreen()
	current, _ := d.panel.GetScreen()
	if d.panel.IsVisible() && (target == nil || (current != nil && current.ID == target.ID)) {
		d.hidePanel()
		return
	}
	d.showPanelOn(target)
}
func (d *Desktop) setShortcut(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("快捷键不能为空")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if key == d.shortcut {
		return nil
	}
	if err := d.app.GlobalShortcut.Register(key, d.togglePanel); err != nil {
		return fmt.Errorf("快捷键不可用或已被占用，请重新设置：%w", err)
	}
	if d.shortcut != "" {
		d.app.GlobalShortcut.Unregister(d.shortcut)
	}
	d.shortcut = key
	d.errorText = ""
	return nil
}
func (d *Desktop) shouldQuit() bool {
	d.mu.Lock()
	if d.quitApproved || d.engine == nil || d.engine.ActiveCount() == 0 {
		d.mu.Unlock()
		return true
	}
	if d.quitPrompt {
		d.mu.Unlock()
		return false
	}
	d.quitPrompt = true
	d.mu.Unlock()
	go func() {
		dialog := d.app.Dialog.Question().SetTitle("仍有任务正在执行").SetMessage("退出将中断任务。已保存的历史会保留，重新打开后不会自动重发消息。")
		dialog.AddButton("取消").SetAsCancel().SetAsDefault().OnClick(func() { d.mu.Lock(); d.quitPrompt = false; d.mu.Unlock() })
		dialog.AddButton("退出并中断").OnClick(func() { d.mu.Lock(); d.quitApproved = true; d.quitPrompt = false; d.mu.Unlock(); d.app.Quit() })
		dialog.Show()
	}()
	return false
}
func (d *Desktop) refreshEnvironment() {
	select {
	case <-d.done:
		return
	default:
	}
	d.detectMu.Lock()
	defer d.detectMu.Unlock()
	path := d.engine.Settings().PiPath
	var err error
	if path == "" {
		path, err = pi.Locate()
	}
	env := core.Environment{PiPath: path}
	if err == nil {
		path, err = filepath.Abs(path)
		env.PiPath = path
	}
	if err == nil {
		var info os.FileInfo
		info, err = os.Stat(path)
		if err == nil && (info.IsDir() || info.Mode()&0111 == 0) {
			err = errors.New("Pi 路径不是可执行文件")
		}
	}
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, path, "--version")
		cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(path)+":"+os.Getenv("PATH")+":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin")
		out, e := cmd.Output()
		err = e
		if e == nil {
			env.Version = strings.TrimSpace(string(out))
			if len(env.Version) > 160 {
				env.Version = env.Version[:160]
			}
			env.Available = true
		}
	}
	if err != nil {
		env.Error = "无法使用 Pi，请检查安装与配置后重新检测：" + err.Error()
	}
	d.engine.SetEnvironment(env)
}
func openWebURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("仅支持 HTTP 或 HTTPS 链接")
	}
	return exec.Command("/usr/bin/open", u.String()).Run()
}
func (d *Desktop) openPath(view, raw string, reveal bool) error {
	if strings.HasPrefix(raw, "file:") {
		u, e := url.Parse(raw)
		if e != nil || u.Host != "" && u.Host != "localhost" {
			return errors.New("无效本地文件链接")
		}
		raw = u.Path
	}
	if strings.IndexByte(raw, 0) >= 0 || raw == "" {
		return errors.New("文件路径为空或无效")
	}
	if !filepath.IsAbs(raw) {
		s := d.engine.Snapshot(view)
		if s.Current == nil {
			return errors.New("未选择会话")
		}
		raw = filepath.Join(s.Current.CWD, raw)
	}
	path, err := filepath.Abs(raw)
	if err != nil {
		return err
	}
	if _, err = os.Stat(path); err != nil {
		return fmt.Errorf("文件不存在或不可访问：%w", err)
	}
	if reveal {
		return exec.Command("/usr/bin/open", "-R", path).Run()
	}
	return exec.Command("/usr/bin/open", path).Run()
}
func (d *Desktop) notify(sid, title, body string) {
	if d.panel.IsVisible() {
		return
	}
	if d.main.IsVisible() && d.main.IsFocused() && d.engine.CurrentID("main") == sid {
		return
	}
	sendNotification(sid, title, body)
}

func decodeAttachments(value any) ([]core.Attachment, error) {
	if value == nil {
		return []core.Attachment{}, nil
	}
	b, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("附件格式无效")
	}
	var attachments []core.Attachment
	if err = json.Unmarshal(b, &attachments); err != nil {
		return nil, errors.New("附件格式无效")
	}
	if attachments == nil {
		attachments = []core.Attachment{}
	}
	return attachments, nil
}

func (d *Desktop) notificationSelected(sid string) {
	select {
	case <-d.done:
		return
	default:
	}
	if d.engine.Select("main", sid) == nil {
		d.showMain()
	}
}
