package pi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"pi-popchat/internal/agent"
)

func (Factory) ReadSessionInfo(ctx context.Context, cfg agent.Config) (string, error) {
	f, err := openSessionFile(cfg.SessionFile, cfg.SessionDir)
	if err != nil {
		return "", err
	}
	defer f.Close()
	// Metadata is chronological, independent of the active message branch. Decode
	// only the header/name fields; do not rebuild or retain model history here.
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 64*1024), 32*1024*1024)
	title := ""
	header := false
	partial := false
	for scan.Scan() {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		if len(bytes.TrimSpace(scan.Bytes())) == 0 {
			continue
		}
		if partial {
			return "", errors.New("Pi 会话中间存在损坏记录")
		}
		var entry struct {
			Type    string `json:"type"`
			Name    string `json:"name"`
			Version int    `json:"version"`
			ID      string `json:"id"`
		}
		if json.Unmarshal(scan.Bytes(), &entry) != nil {
			partial = true
			continue
		}
		if !header {
			if entry.Type != "session" || entry.Version != 3 || entry.ID == "" {
				return "", errors.New("不支持此 Pi 会话格式")
			}
			header = true
			continue
		}
		if entry.ID == "" {
			return "", errors.New("Pi 会话记录缺少 ID")
		}
		if entry.Type == "session_info" {
			title = entry.Name
		}
	}
	if err = scan.Err(); err != nil {
		return "", err
	}
	if !header {
		return "", errors.New("Pi 会话缺少有效文件头")
	}
	if partial {
		info, err := f.Stat()
		if err != nil {
			return "", err
		}
		last := []byte{0}
		if info.Size() > 0 {
			if _, err = f.ReadAt(last, info.Size()-1); err != nil {
				return "", err
			}
			if last[0] == '\n' {
				return "", errors.New("Pi 会话尾部完整记录损坏")
			}
		}
	}
	return title, nil
}
