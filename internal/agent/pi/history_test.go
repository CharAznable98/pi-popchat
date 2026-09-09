package pi

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVisibleHistoryCumulativeImagesBeyondRPCFrame(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(file, "{\"type\":\"session\",\"version\":3,\"id\":\"session\"}\n")
	chunk := strings.Repeat("A", 1024*1024)
	for i := 1; i <= 3; i++ {
		parent := "null"
		if i > 1 {
			parent = fmt.Sprintf("\"m%d\"", i-1)
		}
		fmt.Fprintf(file, "{\"type\":\"message\",\"id\":\"m%d\",\"parentId\":%s,\"message\":{\"role\":\"user\",\"timestamp\":%d,\"content\":[{\"type\":\"text\",\"text\":\"photo %d\"},{\"type\":\"image\",\"mimeType\":\"image/png\",\"data\":\"", i, parent, i, i)
		for j := 0; j < 12; j++ {
			if _, err = io.WriteString(file, chunk); err != nil {
				t.Fatal(err)
			}
		}
		io.WriteString(file, "\"}]}}\n")
	}
	file.Close()
	info, _ := os.Stat(path)
	if info.Size() <= 32*1024*1024 {
		t.Fatal("fixture must exceed old entire-response cap")
	}
	c := &client{historyRoot: root, historyFile: path}
	messages, err := c.ReadHistory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("lost image-history messages: %d", len(messages))
	}
	for i, m := range messages {
		content := m["content"].([]any)
		if len(content) != 1 || content[0].(map[string]any)["text"] != fmt.Sprintf("photo %d", i+1) {
			t.Fatal("projection retained image or lost text")
		}
	}
}
func TestVisibleHistoryBranchAndInterruptedTail(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.jsonl")
	data := `{"type":"session","version":3,"id":"session"}
{"type":"message","id":"root","parentId":null,"message":{"role":"user","timestamp":1,"content":"root question"}}
{"type":"message","id":"abandoned","parentId":"root","message":{"role":"assistant","timestamp":2,"content":[{"type":"text","text":"wrong branch"}]}}
{"type":"message","id":"active","parentId":"root","message":{"role":"assistant","timestamp":3,"content":[{"type":"thinking","thinking":"hidden"},{"type":"text","text":"active reply"},{"type":"toolCall","arguments":{"secret":"hidden"}}]}}
{"type":"message","id":`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	c := &client{historyRoot: root, historyFile: path}
	messages, err := c.ReadHistory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[1]["content"].([]any)[0].(map[string]any)["text"] != "active reply" {
		t.Fatalf("mixed branch or raw reasoning: %v", messages)
	}
	if err = os.WriteFile(path, []byte(data+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = c.ReadHistory(context.Background()); err == nil {
		t.Fatal("complete corrupt line silently accepted")
	}
}
func TestVisibleHistoryRejectsUnsupportedAndUnmanagedFiles(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	os.WriteFile(outside, []byte("{}"), 0600)
	c := &client{historyRoot: root, historyFile: outside}
	if _, err := c.ReadHistory(context.Background()); err == nil {
		t.Fatal("unmanaged history accepted")
	}
	path := filepath.Join(root, "v99.jsonl")
	os.WriteFile(path, []byte(`{"type":"session","version":99,"id":"future"}`), 0600)
	c.historyFile = path
	if _, err := c.ReadHistory(context.Background()); err == nil {
		t.Fatal("future schema interpreted as v3")
	}
}
