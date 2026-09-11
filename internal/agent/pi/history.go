package pi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pi-popchat/internal/agent"
)

var _ agent.HistoryReader = (*client)(nil)

// ReadHistory reads only this client's registered, application-owned session.
// Pi v3 reconstructs its leaf as the last entry on disk when opening a session.
// We follow that parent chain solely for display; Pi still restores its own
// context (including compaction, images, tools and reasoning) via --session.
func (c *client) ReadHistory(ctx context.Context) ([]map[string]any, error) {
	c.mu.Lock()
	path, root := c.historyFile, c.historyRoot
	c.mu.Unlock()
	f, err := openSessionFile(path, root)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readVisibleHistory(ctx, f)
}

type visibleEntry struct {
	Type     string `json:"type"`
	Version  int    `json:"version"`
	ID       string `json:"id"`
	ParentID string `json:"parentId"`
	Message  struct {
		Role      string          `json:"role"`
		Timestamp float64         `json:"timestamp"`
		Content   json.RawMessage `json:"content"`
	} `json:"message"`
}
type visibleNode struct {
	parent  string
	message map[string]any
}

func readVisibleHistory(ctx context.Context, f *os.File) ([]map[string]any, error) {
	scanner := bufio.NewScanner(f)
	// Bound each entry, not the complete session. Images are discarded after
	// parsing each line, so many-image sessions do not form a giant RPC response.
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	nodes := map[string]visibleNode{}
	header := false
	leaf := ""
	invalidTail := false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if invalidTail {
			return nil, errors.New("Pi 会话中间存在损坏记录")
		}
		var entry visibleEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			invalidTail = true
			continue
		}
		if !header {
			if entry.Type != "session" || entry.Version != 3 || entry.ID == "" {
				return nil, errors.New("不支持此 Pi 会话格式，需要 Pi v3 会话文件")
			}
			header = true
			continue
		}
		if entry.ID == "" {
			return nil, errors.New("Pi 会话记录缺少 ID")
		}
		if _, duplicate := nodes[entry.ID]; duplicate {
			return nil, errors.New("Pi 会话记录 ID 重复")
		}
		node := visibleNode{parent: entry.ParentID}
		role := entry.Message.Role
		if entry.Type == "message" && (role == "user" || role == "assistant") {
			text := ""
			if len(entry.Message.Content) > 0 && entry.Message.Content[0] == '"' {
				_ = json.Unmarshal(entry.Message.Content, &text)
			} else {
				// Unknown content fields (base64 image data, thinking/tool arguments) are
				// skipped by the decoder and never retained in the display projection.
				var blocks []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}
				if err := json.Unmarshal(entry.Message.Content, &blocks); err != nil {
					return nil, errors.New("Pi 消息内容格式无效")
				}
				for _, b := range blocks {
					if b.Type == "text" {
						text += b.Text
					}
				}
			}
			node.message = map[string]any{"role": role, "timestamp": entry.Message.Timestamp, "content": []any{map[string]any{"type": "text", "text": text}}}
		}
		nodes[entry.ID] = node
		leaf = entry.ID
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("Pi 单条会话记录超过 32 MB 或无法读取: %w", err)
	}
	if !header {
		return nil, errors.New("Pi 会话缺少有效文件头")
	}
	if invalidTail {
		if info, err := f.Stat(); err == nil && info.Size() > 0 {
			last := []byte{0}
			if _, err = f.ReadAt(last, info.Size()-1); err == nil && last[0] == '\n' {
				return nil, errors.New("Pi 会话尾部完整记录损坏")
			}
		}
	}
	// An interrupted final append may leave a partial last line. Keep all complete
	// ancestors; the application retains unmatched/uncertain visible submissions.
	reverse := []map[string]any{}
	seen := map[string]bool{}
	for leaf != "" {
		if seen[leaf] {
			return nil, errors.New("Pi 会话父链存在循环")
		}
		seen[leaf] = true
		node, ok := nodes[leaf]
		if !ok {
			return nil, errors.New("Pi 会话父链缺少记录")
		}
		if node.message != nil {
			reverse = append(reverse, node.message)
		}
		leaf = node.parent
	}
	result := make([]map[string]any, len(reverse))
	for i, m := range reverse {
		result[len(reverse)-1-i] = m
	}
	return result, nil
}

func openSessionFile(path, root string) (*os.File, error) {
	if path == "" {
		return nil, agent.ErrHistoryMissing
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("无法读取受管会话目录: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, agent.ErrHistoryMissing
	} // Core distinguishes a new conversation from a lost historical session.
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("Pi 会话文件不在应用管理目录中")
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("Pi 会话文件不是普通文件")
	}
	return f, nil
}
