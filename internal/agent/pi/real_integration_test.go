package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"pi-popchat/internal/agent"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in: this test calls the user's configured provider through Pi.
// It uses disposable working/session directories, disables tools and discoveries,
// and never reads or changes authentication or provider configuration files.
func TestRealPiConversationResumeAndAbort(t *testing.T) {
	if os.Getenv("PI_POPCHAT_REAL_TEST") != "1" {
		t.Skip("set PI_POPCHAT_REAL_TEST=1 to authorize real Pi/model calls")
	}
	executable, err := Locate()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := agent.Config{Executable: executable, CWD: root, SessionDir: filepath.Join(root, "sessions"), ExtraArgs: []string{"--offline", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-context-files", "--no-tools", "--thinking", "off"}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	start := func() (agent.Client, <-chan map[string]any) {
		c, err := (Factory{}).Start(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c, c.Events()
	}
	request := func(c agent.Client, typ string, fields map[string]any) map[string]any {
		if fields == nil {
			fields = map[string]any{}
		}
		fields["type"] = typ
		r, err := c.Request(ctx, fields)
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		return r
	}
	waitSettled := func(events <-chan map[string]any) (string, bool) {
		text := ""
		streamed := false
		for {
			select {
			case e, ok := <-events:
				if !ok {
					t.Fatal("Pi exited before settled")
				}
				switch e["type"] {
				case "message_update":
					streamed = true
				case "message_end":
					msg, _ := e["message"].(map[string]any)
					if msg["stopReason"] == "error" {
						t.Fatalf("real model failed: %v", msg["errorMessage"])
					}
					if msg["role"] == "assistant" {
						raw, _ := json.Marshal(msg["content"])
						text += string(raw)
					}
				case "agent_settled":
					return text, streamed
				case "process_exit":
					t.Fatal("Pi exited unexpectedly")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	c, events := start()
	state := request(c, "get_state", nil)["data"].(map[string]any)
	if state["model"] == nil {
		t.Fatal("Pi has no default model; configure Pi first")
	}
	marker := fmt.Sprintf("POPCHECK%d", time.Now().UnixNano())
	request(c, "prompt", map[string]any{"message": "Remember this exact code for the next turn: " + marker + ". Reply with only the code. Do not use tools."})
	text, streamed := waitSettled(events)
	if !strings.Contains(text, marker) || !streamed {
		t.Fatalf("first turn missing marker or stream (stream=%v)", streamed)
	}
	state = request(c, "get_state", nil)["data"].(map[string]any)
	cfg.SessionFile, _ = state["sessionFile"].(string)
	if cfg.SessionFile == "" {
		t.Fatal("missing persisted session file")
	}
	if _, err := os.Stat(cfg.SessionFile); err != nil {
		t.Fatal(err)
	}
	c.Close()
	for range events {
	}
	resumed, resumedEvents := start()
	restored := request(resumed, "get_state", nil)["data"].(map[string]any)
	if restored["sessionId"] != state["sessionId"] {
		t.Fatal("session id changed on resume")
	}
	request(resumed, "prompt", map[string]any{"message": "What exact code did I ask you to remember? Reply only with that code."})
	text, _ = waitSettled(resumedEvents)
	if !strings.Contains(text, marker) {
		t.Fatal("resumed real model did not recall persisted context")
	}
	request(resumed, "prompt", map[string]any{"message": "Write a long list of 300 distinct brief ideas for improving a desktop chat application."})
	request(resumed, "abort", nil)
	stopped := request(resumed, "get_state", nil)["data"].(map[string]any)
	if stopped["isStreaming"] == true {
		t.Fatal("abort left Pi streaming")
	}
	resumed.Close()
	for range resumedEvents {
	}
	t.Log("PASS: real configured model, streamed response, isolated persisted history, process restart/context recall, abort returns idle")
}

func TestRealPiExtensionInteractions(t *testing.T) {
	if os.Getenv("PI_POPCHAT_REAL_TEST") != "1" {
		t.Skip("set PI_POPCHAT_REAL_TEST=1 to run installed Pi")
	}
	root := t.TempDir()
	extension := filepath.Join(root, "dialogs.mjs")
	source := `export default function(pi) { pi.registerCommand("popchat-test-dialogs", {description:"test only", handler:async(args,ctx)=>{const a=await ctx.ui.confirm("Confirm","Continue?");const b=await ctx.ui.select("Select",["A","B"]);const c=await ctx.ui.input("Input","placeholder");const d=await ctx.ui.editor("Editor","initial");ctx.ui.notify(JSON.stringify({a,b,c,d}),"info");}});}`
	if err := os.WriteFile(extension, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := (Factory{}).Start(ctx, agent.Config{CWD: root, SessionDir: filepath.Join(root, "sessions"), ExtraArgs: []string{"--offline", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-context-files", "--no-tools", "-e", extension}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	completed := make(chan error, 1)
	go func() {
		_, err := c.Request(ctx, map[string]any{"type": "prompt", "message": "/popchat-test-dialogs"})
		completed <- err
	}()
	methods := []string{}
	for {
		select {
		case e := <-c.Events():
			if e["type"] == "process_exit" {
				t.Fatal("Pi exited during interaction")
			}
			if e["type"] != "extension_ui_request" {
				continue
			}
			method, _ := e["method"].(string)
			response := map[string]any{"type": "extension_ui_response", "id": e["id"]}
			switch method {
			case "confirm":
				response["confirmed"] = true
			case "select":
				response["value"] = "B"
			case "input":
				response["value"] = "typed"
			case "editor":
				response["value"] = "edited"
			case "notify":
				if e["message"] != `{"a":true,"b":"B","c":"typed","d":"edited"}` {
					t.Fatalf("wrong interaction result: %v", e["message"])
				}
				if err := <-completed; err != nil {
					t.Fatal(err)
				}
				if strings.Join(methods, ",") != "confirm,select,input,editor" {
					t.Fatal(methods)
				}
				t.Log("PASS: real Pi confirm/select/input/editor round trips while prompt awaits response")
				return
			default:
				continue
			}
			methods = append(methods, method)
			if _, err := c.Request(ctx, response); err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
