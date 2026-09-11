package core

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type SelectionButton struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Template string `json:"template"`
}
type SelectionSettings struct {
	Enabled bool              `json:"enabled"`
	Buttons []SelectionButton `json:"buttons"`
}

func DefaultSelectionSettings() *SelectionSettings {
	return &SelectionSettings{Enabled: true, Buttons: []SelectionButton{
		{ID: "translate", Name: "翻译", Template: "请将以下内容翻译为{{language}}，只输出译文：\n{{text}}"},
		{ID: "explain", Name: "解释", Template: "请用{{language}}解释以下内容：\n{{text}}"},
	}}
}

var templateVariable = regexp.MustCompile(`\{\{([a-zA-Z_]+)\}\}`)

func RenderSelection(template, text, language string, at time.Time) string {
	vars := map[string]string{"text": text, "language": language, "date": at.Format("2006-01-02"), "time": at.Format("15:04:05"), "timezone": at.Location().String()}
	return templateVariable.ReplaceAllStringFunc(template, func(token string) string {
		if v, ok := vars[token[2:len(token)-2]]; ok {
			return v
		}
		return token
	})
}
func cloneSettings(s Settings) Settings {
	b, _ := json.Marshal(s)
	var out Settings
	_ = json.Unmarshal(b, &out)
	return out
}
func validateSelection(s *SelectionSettings) error {
	if s == nil {
		return nil
	}
	if len(s.Buttons) > 20 {
		return errors.New("最多配置 20 个划词按钮")
	}
	seen := map[string]bool{}
	for _, b := range s.Buttons {
		if b.ID == "" || seen[b.ID] {
			return errors.New("划词按钮 ID 无效或重复")
		}
		seen[b.ID] = true
		if strings.TrimSpace(b.Name) == "" || len([]rune(b.Name)) > 30 || strings.TrimSpace(b.Template) == "" || len(b.Template) > 200000 {
			return errors.New("划词按钮需填写名称（最多 30 字）和提示词（最多 200000 字节）")
		}
	}
	return nil
}

// SubmitSelection atomically persists a distinct first submission. It never
// promotes or clears the shared composer, and starts delivery only after commit.
func (e *Engine) SubmitSelection(text string) (string, error) {
	if strings.TrimSpace(text) == "" || len(text) > 200000 {
		return "", errors.New("划词提示词为空或超过 200000 字节")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing {
		return "", errors.New("应用正在退出")
	}
	sid := id()
	m := Message{ID: id(), Role: "user", Text: text, Status: "sending", CreatedAt: now(), Attachments: []Attachment{}}
	s := &Session{ID: sid, CWDSource: "managed", CWD: filepath.Join(e.store.Root, "workspaces", sid), CreatedAt: now(), UpdatedAt: now(), Status: "starting", SessionFile: filepath.Join(e.store.Root, "sessions", sid, "session.jsonl"), Messages: []Message{m}}
	normalize(s)
	s.Title = fallbackTitle(s)
	s.SearchableText = s.Title + "\n" + text
	selected := map[string]string{}
	for k, v := range e.selected {
		selected[k] = v
	}
	selected["panel"] = sid
	if err := e.store.saveSubmission(s, selected); err != nil {
		return "", err
	}
	e.sessions[sid] = s
	e.selected = selected
	e.committed[sid], _ = json.Marshal(s)
	e.hiddenAt = time.Time{}
	e.changedLocked()
	e.scheduleLocked(sid, m, "prompt")
	return sid, nil
}
