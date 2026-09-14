package pi

import (
	"context"
	"os"
	"path/filepath"
	"pi-popchat/internal/agent"
	"testing"
)

func TestTitleUsesLatestMetadataIncludingClear(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.jsonl")
	cfg := agent.Config{SessionDir: root, SessionFile: path}
	content := `{"type":"session","version":3,"id":"s"}
{"type":"session_info","id":"a","parentId":null,"name":"old"}
{"type":"session_info","id":"b","parentId":"a","name":"latest"}
`
	os.WriteFile(path, []byte(content), 0600)
	name, err := (Factory{}).ReadSessionInfo(context.Background(), cfg)
	if err != nil || name != "latest" {
		t.Fatalf("%q %v", name, err)
	}
	content += `{"type":"session_info","id":"c","parentId":"b","name":""}` + "\n"
	os.WriteFile(path, []byte(content), 0600)
	name, err = (Factory{}).ReadSessionInfo(context.Background(), cfg)
	if err != nil || name != "" {
		t.Fatalf("clear: %q %v", name, err)
	}
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	os.WriteFile(outside, []byte(content), 0600)
	cfg.SessionFile = outside
	if _, err = (Factory{}).ReadSessionInfo(context.Background(), cfg); err == nil {
		t.Fatal("unmanaged file accepted")
	}
}
