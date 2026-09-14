package core

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestMessageImageWorkspaceAndContent(t *testing.T) {
	root := t.TempDir()
	f, err := os.Create(filepath.Join(root, "image.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	e := &Engine{sessions: map[string]*Session{"origin": {CWD: root}}}
	for _, path := range []string{"image.png", filepath.Join(root, "image.png")} {
		data, err := e.ReadMessageImage("origin", path)
		if err != nil || !strings.HasPrefix(data, "data:image/png;base64,") {
			t.Fatalf("image read: %v", err)
		}
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.png"), []byte("private synthetic text"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fake.png"), []byte("not an image"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "large.png"), make([]byte, MaxMessageImageBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "pipe.png"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"pipe.png", filepath.Join(outside, "secret.png"), "escape/secret.png", "fake.png", "large.png", "missing.png", "."} {
		if _, err := e.ReadMessageImage("origin", path); err == nil {
			t.Errorf("accepted invalid image %s", path)
		}
	}
	if _, err := e.ReadMessageImage("missing", "image.png"); err == nil {
		t.Fatal("accepted deleted session")
	}
}
