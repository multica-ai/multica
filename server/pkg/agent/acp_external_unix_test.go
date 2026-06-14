//go:build unix

package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestACPExternalExecuteUsesSessionNewResponseForPrompt(t *testing.T) {
	t.Parallel()

	recordPath := filepath.Join(t.TempDir(), "frames.jsonl")
	fakePath := filepath.Join(t.TempDir(), "custom-acp")
	writeTestExecutable(t, fakePath, []byte(fakeACPRecordingScript(recordPath, "ses_ext", `{}`)))

	backend, err := New("custom-acp", Config{
		ExecutablePath: fakePath,
		Transport:      "acp-stdio",
		IsExternal:     true,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new external acp backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "hello acp", ExecOptions{
		Timeout: 5 * time.Second,
		Cwd:     t.TempDir(),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("status = %q error=%q, want completed", result.Status, result.Error)
		}
		if result.SessionID != "ses_ext" {
			t.Fatalf("session id = %q, want ses_ext", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	frame := findRecordedFrame(t, recordPath, "session/prompt")
	params, ok := frame["params"].(map[string]any)
	if !ok {
		t.Fatalf("session/prompt params: got %T, want map", frame["params"])
	}
	if params["sessionId"] != "ses_ext" {
		t.Fatalf("session/prompt.sessionId = %v, want ses_ext", params["sessionId"])
	}

	for _, key := range []string{"prompt", "content"} {
		blocks, ok := params[key].([]any)
		if !ok || len(blocks) != 1 {
			t.Fatalf("session/prompt.%s = %T %v, want one text block", key, params[key], params[key])
		}
		block, ok := blocks[0].(map[string]any)
		if !ok {
			t.Fatalf("session/prompt.%s[0] = %T, want map", key, blocks[0])
		}
		if block["type"] != "text" || block["text"] != "hello acp" {
			t.Fatalf("session/prompt.%s[0] = %v, want text block", key, block)
		}
	}
}
