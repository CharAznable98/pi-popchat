package core

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
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
			denied := mode == "legacy" || mode == "user" || mode == "symlink" || mode == "parent-symlink" || mode == "shared" || mode == "busy"
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

func TestDeleteWorkspaceRejectsAncestorReference(t *testing.T) {
	for _, alias := range []bool{false, true} {
		t.Run(fmt.Sprint("symlink=", alias), func(t *testing.T) {
			e, _ := acceptanceEngine(t)
			sid, _ := e.NewSession("main", "")
			path := e.Snapshot("main").Current.CWD
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(path, "shared.txt")
			if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			e.mu.Lock()
			e.sessions[sid].DraftOnly = false
			err := e.saveLocked(e.sessions[sid])
			e.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			ancestor := filepath.Dir(path)
			if alias {
				link := filepath.Join(t.TempDir(), "workspaces-link")
				if err := os.Symlink(ancestor, link); err != nil {
					t.Fatal(err)
				}
				ancestor = link
			}
			other, err := e.NewSession("panel", ancestor)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.DeleteWithWorkspace(sid, true); err == nil {
				t.Error("ancestor workspace reference allowed deletion")
			}
			if _, err := os.Stat(file); err != nil {
				t.Error("another workspace lost its file:", err)
			}
			if e.CurrentID("main") != sid || e.CurrentID("panel") != other {
				t.Error("rejected deletion changed selections")
			}
		})
	}
}

func TestUnknownWorkspaceSourceNeverAuthorizesRemoval(t *testing.T) {
	e, _ := acceptanceEngine(t)
	sid, err := e.NewSession("main", "")
	if err != nil {
		t.Fatal(err)
	}
	cwd := e.Snapshot("main").Current.CWD
	if err = os.MkdirAll(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(cwd, "fixture.txt")
	if err = os.WriteFile(file, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	s := e.sessions[sid]
	s.CWDSource = ""
	err = e.saveLocked(s)
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if e.Snapshot("main").Current.ManagedWorkspace {
		t.Error("unknown legacy source advertised as managed")
	}
	if err = e.DeleteWithWorkspace(sid, true); err == nil {
		t.Error("unknown legacy source allowed recursive deletion")
	}
	if _, err = os.Stat(file); err != nil {
		t.Error("legacy work file was removed")
	}
}

func TestDirectorySelectionAndDeletionSerialize(t *testing.T) {
	for i := 0; i < 20; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			e, _ := acceptanceEngine(t)
			sid, err := e.NewSession("main", "")
			if err != nil {
				t.Fatal(err)
			}
			cwd := e.Snapshot("main").Current.CWD
			if err = os.MkdirAll(cwd, 0700); err != nil {
				t.Fatal(err)
			}
			e.mu.Lock()
			e.sessions[sid].DraftOnly = false
			err = e.saveLocked(e.sessions[sid])
			e.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			other, err := e.NewSession("panel", "")
			if err != nil {
				t.Fatal(err)
			}
			removed := make(chan error, 1)
			selected := make(chan error, 1)
			// Queue deletion first while selection can still inspect the existing path.
			e.mu.Lock()
			go func() { removed <- e.DeleteWithWorkspace(sid, true) }()
			time.Sleep(time.Millisecond)
			go func() { selected <- e.SetCWD(other, cwd) }()
			time.Sleep(time.Millisecond)
			e.mu.Unlock()
			deleteErr, selectErr := <-removed, <-selected
			if deleteErr == nil && selectErr == nil {
				t.Fatal("selection accepted the path deleted by concurrent operation")
			}
			if selectErr == nil {
				if _, err = os.Stat(e.Snapshot("panel").Current.CWD); err != nil {
					t.Fatal("successful selection references missing directory")
				}
			}
		})
	}
}
