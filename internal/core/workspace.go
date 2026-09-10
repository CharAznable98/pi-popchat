package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Only explicit ownership authorizes removal. Legacy paths cannot distinguish
// an application default from a user choosing that same directory.
func (e *Engine) managedWorkspaceLocked(s *Session) bool {
	return s.CWDSource == "managed" && s.CWD == filepath.Join(e.store.Root, "workspaces", s.ID) && filepath.Base(s.ID) == s.ID && s.ID != "." && s.ID != ".."
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
		for _, pair := range [][2]string{{s.CWD, path}, {path, s.CWD}} {
			contains, err := workspaceContains(pair[0], pair[1])
			if err != nil {
				return fmt.Errorf("无法确认会话工作目录是否重叠，请保留目录：%s：%w", other.Title, err)
			}
			if contains {
				return fmt.Errorf("工作目录仍被其他会话或草稿使用，请先保留目录：%s", other.Title)
			}
		}
	}
	return nil
}

// Keep lexical containment for missing paths, then compare directory identity
// at every ancestor. EvalSymlinks alone does not resolve case aliases on macOS.
func workspaceContains(parent, child string) (bool, error) {
	rel, err := filepath.Rel(parent, child)
	if err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true, nil
	}
	parentInfo, err := os.Stat(parent)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for current := filepath.Clean(child); ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err == nil {
			if os.SameFile(parentInfo, info) {
				return true, nil
			}
		} else if !os.IsNotExist(err) {
			return false, err
		}
		if filepath.Dir(current) == current {
			return false, nil
		}
	}
}
