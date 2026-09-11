package core

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
)

// ReadMessageImage resolves against the initiating session, not the active window.
// OpenRoot confines automatic reads (including symlinks) to that workspace.
func (e *Engine) ReadMessageImage(sessionID, path string) (string, error) {
	e.mu.Lock()
	s := e.sessions[sessionID]
	if s == nil {
		e.mu.Unlock()
		return "", errors.New("会话不存在")
	}
	cwd := s.CWD
	e.mu.Unlock()
	if filepath.IsAbs(path) {
		var err error
		path, err = filepath.Rel(cwd, path)
		if err != nil {
			return "", err
		}
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return "", err
	}
	defer root.Close()
	// Avoid blocking the bridge if a path names a FIFO or another special file.
	f, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxMessageImageBytes {
		return "", errors.New("图片无效或超过 16 MB")
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxMessageImageBytes+1))
	if err != nil {
		return "", err
	}
	if len(b) > MaxMessageImageBytes {
		return "", errors.New("图片超过 16 MB")
	}
	mime := http.DetectContentType(b)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b), nil
	default:
		return "", errors.New("不支持的图片格式")
	}
}
