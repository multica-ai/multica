//go:build unix

package agent

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCursorExecuteInstallsSupplementHooksForTheRun(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	script := `#!/bin/sh
cat > prompt.txt
printf '%s\n%s\n' "$MULTICA_CURSOR_HOOK_URL" "$MULTICA_CURSOR_HOOK_TOKEN" > hook-env.txt
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-steer"}'
while [ ! -f receipt.jsonl ]; do sleep 0.02; done
cat receipt.jsonl
while [ ! -f release ]; do sleep 0.02; done
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"sess-steer"}'
`
	fakePath := filepath.Join(t.TempDir(), "cursor-agent")
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("cursor", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	session, err := backend.Execute(t.Context(), "original goal", ExecOptions{
		Cwd:                  cwd,
		Timeout:              30 * time.Second,
		EnableTaskSupplement: true,
		CursorHookCommand:    testCursorHookCommand,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	if session.Supplement == nil || session.SupplementReady == nil {
		t.Fatal("negotiated cursor session exposes no supplement hooks")
	}

	deadline := time.Now().Add(10 * time.Second)
	for !session.SupplementReady() {
		if time.Now().After(deadline) {
			t.Fatal("session never became ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var envLines []string
	for len(envLines) < 2 {
		data, _ := os.ReadFile(filepath.Join(cwd, "hook-env.txt"))
		envLines = strings.Fields(string(data))
		if time.Now().After(deadline) {
			t.Fatal("cursor-agent did not receive the hook environment")
		}
		time.Sleep(10 * time.Millisecond)
	}
	getenv := func(name string) string {
		switch name {
		case cursorHookURLEnv:
			return envLines[0]
		case cursorHookTokenEnv:
			return envLines[1]
		}
		return ""
	}
	if _, err := os.Stat(filepath.Join(cwd, ".cursor", "hooks.json")); err != nil {
		t.Fatalf("hooks.json missing during the run: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- session.Supplement(t.Context(), "Prefer the smaller fix.") }()
	var reply map[string]any
	for {
		reply = relayCursorHook(t, getenv, postToolUse("sess-steer"))
		if len(reply) != 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	guidance, _ := reply["additional_context"].(string)
	tag, _, _ := strings.Cut(guidance, "\n")
	if !strings.HasPrefix(tag, cursorGuidanceTagPrefix) || !strings.HasSuffix(guidance, "Prefer the smaller fix.") {
		t.Fatalf("additional_context = %q", guidance)
	}
	assertSupplementWaiting(t, done)
	receipt, err := json.Marshal(map[string]any{
		"type": "tool_call", "subtype": "completed", "call_id": "call-1\nfc_1", "session_id": "sess-steer",
		"tool_call": map[string]any{
			"readToolCall":           map[string]any{"args": map[string]any{"path": "a.txt"}, "result": map[string]any{"success": map[string]any{}}},
			"hookAdditionalContexts": []map[string]any{{"hookEventName": "postToolUse", "content": guidance}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "receipt.tmp"), append(receipt, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(cwd, "receipt.tmp"), filepath.Join(cwd, "receipt.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Supplement = %v, want delivered", err)
	}
	prompt, _ := os.ReadFile(filepath.Join(cwd, "prompt.txt"))
	if !strings.HasPrefix(string(prompt), "original goal") || !strings.Contains(string(prompt), tag) {
		t.Fatalf("prompt does not declare the run's tag %q:\n%s", tag, prompt)
	}

	if err := os.WriteFile(filepath.Join(cwd, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if result := <-session.Result; result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	if session.SupplementReady() {
		t.Fatal("ready after the terminal result")
	}
	if _, err := os.Stat(filepath.Join(cwd, ".cursor", "hooks.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hooks.json after the run: %v, want removed", err)
	}
}

func TestCursorExecuteWithoutNegotiationInstallsNothing(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	script := `#!/bin/sh
cat > prompt.txt
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-plain"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"sess-plain"}'
`
	fakePath := filepath.Join(t.TempDir(), "cursor-agent")
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("cursor", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	session, err := backend.Execute(t.Context(), "original goal", ExecOptions{
		Cwd: cwd, Timeout: 30 * time.Second, CursorHookCommand: testCursorHookCommand,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	if result := <-session.Result; result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	if session.Supplement != nil || session.SupplementReady != nil {
		t.Fatal("un-negotiated session exposes supplement hooks")
	}
	if prompt, _ := os.ReadFile(filepath.Join(cwd, "prompt.txt")); string(prompt) != "original goal" {
		t.Fatalf("prompt = %q, want unchanged", prompt)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".cursor")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(".cursor created without negotiation: %v", err)
	}
}

// Steering was already offered for the run, so a workdir that refuses the
// managed hooks must settle every message as failed rather than leave it
// pending until the turn ends.
func TestCursorExecuteRejectsSupplementsWhenHooksCannotInstall(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	own := []byte(`{"version":1,"hooks":{}}`)
	if err := os.MkdirAll(filepath.Join(cwd, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".cursor", "hooks.json"), own, 0o600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
cat > prompt.txt
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-blocked"}'
while [ ! -f release ]; do sleep 0.02; done
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"sess-blocked"}'
`
	fakePath := filepath.Join(t.TempDir(), "cursor-agent")
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("cursor", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	session, err := backend.Execute(t.Context(), "original goal", ExecOptions{
		Cwd:                  cwd,
		Timeout:              30 * time.Second,
		EnableTaskSupplement: true,
		CursorHookCommand:    testCursorHookCommand,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	if session.Supplement == nil || session.SupplementReady == nil || !session.SupplementReady() {
		t.Fatal("negotiated session does not settle supplements")
	}
	if err := session.Supplement(t.Context(), "Prefer the smaller fix."); err == nil {
		t.Fatal("Supplement succeeded without installed hooks")
	}
	if err := os.WriteFile(filepath.Join(cwd, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if result := <-session.Result; result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	if got, _ := os.ReadFile(filepath.Join(cwd, ".cursor", "hooks.json")); string(got) != string(own) {
		t.Fatalf("workdir hooks.json changed to %q", got)
	}
	if prompt, _ := os.ReadFile(filepath.Join(cwd, "prompt.txt")); string(prompt) != "original goal" {
		t.Fatalf("prompt = %q, want no steering note", prompt)
	}
}
