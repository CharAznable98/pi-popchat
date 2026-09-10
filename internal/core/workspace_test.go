package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteWorkspaceOwnershipAndChoice(t *testing.T) {
	for _, mode := range []string{"keep", "remove", "user", "legacy", "symlink", "parent-symlink", "shared", "busy", "missing"} {
		t.Run(mode, func(t *testing.T) {
			e, _ := acceptanceEngine(t)
			sid, _ := e.NewSession("main", "")
			path := e.Snapshot("main").Current.CWD
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(path, "keep.txt")
			if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			e.mu.Lock()
			s := e.sessions[sid]
			s.DraftOnly = false
			if mode == "legacy" {
				s.CWDSource = ""
			}
			if mode == "busy" {
				s.Status = "running"
			}
			if err := e.saveLocked(s); err != nil {
				t.Fatal(err)
			}
			e.mu.Unlock()
			if mode == "user" {
				if err := e.SetCWD(sid, path); err != nil {
					t.Fatal(err)
				}
			}
			external := t.TempDir()
			externalFile := filepath.Join(external, "external.txt")
			_ = os.WriteFile(externalFile, []byte("outside"), 0600)
			if mode == "symlink" {
				_ = os.RemoveAll(path)
				if err := os.Symlink(external, path); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "parent-symlink" {
				_ = os.RemoveAll(filepath.Dir(path))
				if err := os.Symlink(external, filepath.Dir(path)); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "missing" {
				_ = os.RemoveAll(path)
			}
			if mode == "shared" {
				if _, err := e.NewSession("panel", path); err != nil {
					t.Fatal(err)
				}
			}
			err := e.DeleteWithWorkspace(sid, mode != "keep")
			denied := mode == "user" || mode == "symlink" || mode == "parent-symlink" || mode == "shared" || mode == "busy"
			if denied {
				if err == nil {
					t.Fatal("unsafe removal accepted")
				}
				if len(e.Snapshot("main").Sessions) != 1 {
					t.Fatal("rejected removal deleted history")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if len(e.Snapshot("main").Sessions) != 0 {
					t.Fatal("history not deleted")
				}
				_, statErr := os.Stat(file)
				if mode == "keep" && statErr != nil {
					t.Fatal("default deletion lost files")
				}
				if mode != "keep" && !os.IsNotExist(statErr) {
					t.Fatal("workspace not removed")
				}
			}
			if _, err := os.Stat(externalFile); err != nil {
				t.Fatal("external file deleted")
			}
		})
	}
}

func TestWorkspaceDeletionDatabaseFailureRestoresFiles(t *testing.T) {
	e, _ := acceptanceEngine(t)
	sid, _ := e.NewSession("main", "")
	path := e.Snapshot("main").Current.CWD
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(path, "report.txt")
	if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	if err := e.DeleteWithWorkspace(sid, true); err == nil {
		t.Fatal("readonly database allowed deletion")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("database failure lost workspace")
	}
	if e.Snapshot("main").Current == nil {
		t.Fatal("database failure removed record")
	}
	if _, err := e.store.db.Exec("PRAGMA query_only=OFF"); err != nil {
		t.Fatal(err)
	}
	if err := e.DeleteWithWorkspace(sid, true); err != nil {
		t.Fatal(err)
	}
}
