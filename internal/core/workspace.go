package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Old records predate explicit ownership. Only their exact default path counts.
// A user-selected directory never acquires ownership by matching that path.
func (e *Engine) managedWorkspaceLocked(s *Session) bool {
	return s.CWDSource != "user" && s.CWD == filepath.Join(e.store.Root, "workspaces", s.ID) && filepath.Base(s.ID) == s.ID && s.ID != "." && s.ID != ".."
}

func (e *Engine) validateWorkspaceRemovalLocked(s *Session) error {
	if !e.managedWorkspaceLocked(s) {
		return errors.New("只能删除应用默认管理的工作目录")
	}
	parent := filepath.Join(e.store.Root, "workspaces")
	actual, err := filepath.EvalSymlinks(parent)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if actual != parent {
		return errors.New("工作目录父路径已改变，不能自动删除")
	}
	info, err := os.Lstat(s.CWD)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("工作目录已被替换，不能自动删除")
	}
	for _, other := range e.sessions {
		if other.ID == s.ID {
			continue
		}
		path, err := filepath.EvalSymlinks(other.CWD)
		if err != nil {
			path = filepath.Clean(other.CWD)
		}
		if workspaceContains(s.CWD, path) || workspaceContains(path, s.CWD) {
			return fmt.Errorf("工作目录仍被其他会话或草稿使用，请先保留目录：%s", other.Title)
		}
	}
	return nil
}

// Overlapping work trees share files in either containment direction.
func workspaceContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
