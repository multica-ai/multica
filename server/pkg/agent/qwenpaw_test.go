package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestQwenPawBypassPermissionsDefault(t *testing.T) {
	env := map[string]string{}
	if !qwenpawBypassPermissions(env) {
		t.Fatal("expected bypassPermissions to be enabled by default when env is empty")
	}
}

func TestQwenPawBypassPermissionsDisabledByEnvMap(t *testing.T) {
	env := map[string]string{"MULTICA_QWENPAW_BYPASS_PERMISSIONS": "false"}
	if qwenpawBypassPermissions(env) {
		t.Fatal("expected false env value to disable bypassPermissions")
	}
}

func TestQwenPawBypassPermissionsDisabledByEnvMapOff(t *testing.T) {
	for _, v := range []string{"0", "false", "no", "off"} {
		env := map[string]string{"MULTICA_QWENPAW_BYPASS_PERMISSIONS": v}
		if qwenpawBypassPermissions(env) {
			t.Fatalf("expected %q to disable bypassPermissions", v)
		}
	}
}

func TestQwenPawBypassPermissionsEnabledByEnvMap(t *testing.T) {
	for _, v := range []string{"1", "true", "yes", "anything-else"} {
		env := map[string]string{"MULTICA_QWENPAW_BYPASS_PERMISSIONS": v}
		if !qwenpawBypassPermissions(env) {
			t.Fatalf("expected %q to enable bypassPermissions", v)
		}
	}
}

func TestQwenPawToolNameFromTitle(t *testing.T) {
	tests := map[string]string{
		"execute_shell_command":   "terminal",
		"Run command: git status":   "terminal",
		"Read file: README.md":      "read_file",
		"file_search":               "search_files",
		"Apply Patch":               "edit_file",
		"Shell: ls -la":             "terminal",
		"Read: /path/to/file":       "read_file",
		"Write: new content":        "write_file",
		"Edit: line 42":             "edit_file",
		"Search: pattern":            "search_files",
		"grep in files":             "search_files",
		"Glob: **/*.go":             "glob",
		"Web Search: latest news":   "web_search",
		"search_query: weather":      "web_search",
		"Fetch: https://example.com": "web_fetch",
		"Open: https://example.com": "web_fetch",
		"Todo write":                "todo_write",
		"custom tool":               "custom_tool",
		"":                          "",
		"Read":                      "read_file",
	}

	for input, want := range tests {
		got := qwenpawToolNameFromTitle(input)
		if got != want {
			t.Fatalf("qwenpawToolNameFromTitle(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestQwenPawBackendSmokeACP(t *testing.T) {
	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePy := filepath.Join(tempDir, "fake_qwenpaw.py")
	fakePath := filepath.Join(tempDir, "qwenpaw")
	launcher := []byte("#!/usr/bin/env sh\nexec python3 \"" + fakePy + "\" \"$@\"\n")
	if runtime.GOOS == "windows" {
		fakePath += ".bat"
		launcher = []byte("@echo off\r\npython \"%~dp0fake_qwenpaw.py\" %*\r\n")
	}

	writeTestExecutable(t, fakePath, launcher)
	if err := os.WriteFile(fakePy, []byte(fakeQwenPawACPPython()), 0o755); err != nil {
		t.Fatalf("write fake qwenpaw python: %v", err)
	}

	backend, err := New("qwenpaw", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"QWENPAW_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new qwenpaw backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "hello from multica", ExecOptions{
		Cwd:        tempDir,
		Timeout:    5 * time.Second,
		CustomArgs: []string{"acp", "--workspace", "ignored", "--debug"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var messages []Message
	messagesDone := make(chan struct{})
	go func() {
		defer close(messagesDone)
		for msg := range session.Messages {
			messages = append(messages, msg)
		}
	}()

	result := <-session.Result
	<-messagesDone

	if result.Status != "completed" {
		t.Fatalf("status = %q error = %q", result.Status, result.Error)
	}
	if result.Output != "pong" {
		t.Fatalf("output = %q, want pong", result.Output)
	}
	if result.SessionID != "ses_new" {
		t.Fatalf("session id = %q, want ses_new", result.SessionID)
	}
	if usage := result.Usage["unknown"]; usage.InputTokens != 3 || usage.OutputTokens != 2 {
		t.Fatalf("usage = %+v, want input=3 output=2", usage)
	}
	if len(messages) != 1 || messages[0].Type != MessageText || messages[0].Content != "pong" {
		t.Fatalf("messages = %+v, want one text message", messages)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	args := make([]string, 0, len(lines))
	for _, line := range lines {
		args = append(args, strings.TrimSpace(line))
	}
	wantPrefix := []string{"acp", "--bypass-permissions", "--workspace", tempDir}
	if len(args) < len(wantPrefix) {
		t.Fatalf("argv too short: %q", args)
	}
	for i, want := range wantPrefix {
		if args[i] != want {
			t.Fatalf("arg[%d] = %q, want %q (full: %q)", i, args[i], want, args)
		}
	}
	for _, blocked := range []string{"ignored"} {
		for _, got := range args {
			if got == blocked {
				t.Fatalf("blocked custom arg %q was not filtered: %q", blocked, args)
			}
		}
	}
	if args[len(args)-1] != "--debug" {
		t.Fatalf("allowed custom arg was not preserved: %q", args)
	}
}

func TestQwenPawBackendSmokeACPBypassPermissionsDisabled(t *testing.T) {
	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePy := filepath.Join(tempDir, "fake_qwenpaw.py")
	fakePath := filepath.Join(tempDir, "qwenpaw")
	launcher := []byte("#!/usr/bin/env sh\nexec python3 \"" + fakePy + "\" \"$@\"\n")
	if runtime.GOOS == "windows" {
		fakePath += ".bat"
		launcher = []byte("@echo off\r\npython \"%~dp0fake_qwenpaw.py\" %*\r\n")
	}

	writeTestExecutable(t, fakePath, launcher)
	if err := os.WriteFile(fakePy, []byte(fakeQwenPawACPPython()), 0o755); err != nil {
		t.Fatalf("write fake qwenpaw python: %v", err)
	}

	backend, err := New("qwenpaw", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"QWENPAW_ARGS_FILE":                   argsFile,
			"MULTICA_QWENPAW_BYPASS_PERMISSIONS":  "false",
		},
	})
	if err != nil {
		t.Fatalf("new qwenpaw backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "hello", ExecOptions{
		Cwd:     tempDir,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q error = %q", result.Status, result.Error)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")

	hasBypass := false
	for _, arg := range args {
		if arg == "--bypass-permissions" {
			hasBypass = true
			break
		}
	}
	if hasBypass {
		t.Fatalf("expected no --bypass-permissions when MULTICA_QWENPAW_BYPASS_PERMISSIONS=false, got args: %q", args)
	}
}

func TestQwenPawBackendSessionResume(t *testing.T) {
	tempDir := t.TempDir()
	fakePy := filepath.Join(tempDir, "fake_qwenpaw.py")
	fakePath := filepath.Join(tempDir, "qwenpaw")
	launcher := []byte("#!/usr/bin/env sh\nexec python3 \"" + fakePy + "\" \"$@\"\n")
	if runtime.GOOS == "windows" {
		fakePath += ".bat"
		launcher = []byte("@echo off\r\npython \"%~dp0fake_qwenpaw.py\" %*\r\n")
	}

	writeTestExecutable(t, fakePath, launcher)
	if err := os.WriteFile(fakePy, []byte(fakeQwenPawACPSessionResumePython()), 0o755); err != nil {
		t.Fatalf("write fake qwenpaw python: %v", err)
	}

	backend, err := New("qwenpaw", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new qwenpaw backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "resume test", ExecOptions{
		Cwd:             tempDir,
		Timeout:         5 * time.Second,
		ResumeSessionID: "ses_previous",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q error = %q", result.Status, result.Error)
	}
	if result.SessionID != "ses_resumed" {
		t.Fatalf("session id = %q, want ses_resumed", result.SessionID)
	}
}

func TestQwenPawBackendSetModelFailure(t *testing.T) {
	tempDir := t.TempDir()
	fakePy := filepath.Join(tempDir, "fake_qwenpaw.py")
	fakePath := filepath.Join(tempDir, "qwenpaw")
	launcher := []byte("#!/usr/bin/env sh\nexec python3 \"" + fakePy + "\" \"$@\"\n")
	if runtime.GOOS == "windows" {
		fakePath += ".bat"
		launcher = []byte("@echo off\r\npython \"%~dp0fake_qwenpaw.py\" %*\r\n")
	}

	writeTestExecutable(t, fakePath, launcher)
	if err := os.WriteFile(fakePy, []byte(fakeQwenPawACPModelRejectPython()), 0o755); err != nil {
		t.Fatalf("write fake qwenpaw python: %v", err)
	}

	backend, err := New("qwenpaw", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new qwenpaw backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "test", ExecOptions{
		Cwd:     tempDir,
		Timeout: 5 * time.Second,
		Model:   "bad-model",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if result.Error == "" {
		t.Fatalf("expected non-empty error, got %q", result.Error)
	}
	if !strings.Contains(result.Error, "bad-model") {
		t.Fatalf("error = %q, want it to mention the model name", result.Error)
	}
}

func fakeQwenPawACPPython() string {
	return `import json
import os
import sys

args_file = os.environ.get("QWENPAW_ARGS_FILE")
if args_file:
    with open(args_file, "w", encoding="utf-8") as fh:
        for arg in sys.argv[1:]:
            fh.write(arg + "\n")

for line in sys.stdin:
    req = json.loads(line)
    method = req.get("method")
    req_id = req.get("id")
    params = req.get("params", {})
    if method == "initialize":
        result = {
            "protocolVersion": 1,
            "agentCapabilities": {"loadSession": True},
        }
    elif method == "session/new":
        result = {"sessionId": "ses_new"}
    elif method == "session/set_config_option":
        params_val = params.get("value")
        if params_val != "bypassPermissions":
            result = {"error": "expected bypassPermissions"}
        else:
            result = {}
    elif method == "session/set_model":
        model_id = params.get("modelId")
        if model_id == "bad-model":
            sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": req_id, "error": {"code": -32603, "message": "model not found: " + model_id}}) + "\n")
            sys.stdout.flush()
            continue
        result = {}
    elif method == "session/prompt":
        sys.stdout.write(json.dumps({
            "jsonrpc": "2.0",
            "method": "session/notification",
            "params": {
                "sessionId": "ses_new",
                "update": {
                    "type": "AgentMessageChunk",
                    "content": {"type": "text", "text": "pong"},
                },
            },
        }) + "\n")
        sys.stdout.flush()
        result = {
            "stopReason": "end_turn",
            "usage": {"inputTokens": 3, "outputTokens": 2},
        }
    else:
        result = {}
    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": req_id, "result": result}) + "\n")
    sys.stdout.flush()
`
}

func fakeQwenPawACPSessionResumePython() string {
	return `import json
import sys

# Track what methods are called
called = []

for line in sys.stdin:
    req = json.loads(line)
    method = req.get("method")
    req_id = req.get("id")
    params = req.get("params", {})

    if method == "initialize":
        result = {"protocolVersion": 1, "agentCapabilities": {"loadSession": True}}
    elif method == "session/load":
        called.append("load")
        # Return error so it falls back to resume
        sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": req_id, "error": {"code": -32603, "message": "session not found"}}) + "\n")
        sys.stdout.flush()
        continue
    elif method == "session/resume":
        called.append("resume")
        result = {"sessionId": "ses_resumed"}
    elif method == "session/prompt":
        result = {
            "stopReason": "end_turn",
            "usage": {"inputTokens": 1, "outputTokens": 1},
        }
    else:
        result = {}

    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": req_id, "result": result}) + "\n")
    sys.stdout.flush()
`
}

func fakeQwenPawACPModelRejectPython() string {
	return `import json
import sys

for line in sys.stdin:
    req = json.loads(line)
    method = req.get("method")
    req_id = req.get("id")
    params = req.get("params", {})

    if method == "initialize":
        result = {"protocolVersion": 1, "agentCapabilities": {"loadSession": True}}
    elif method == "session/new":
        result = {"sessionId": "ses_new"}
    elif method == "session/set_model":
        model_id = params.get("modelId", params.get("model_id", ""))
        if model_id == "bad-model":
            # Return error to indicate model rejection
            sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": req_id, "error": {"code": -32603, "message": "model not supported: " + model_id}}) + "\n")
            sys.stdout.flush()
            continue
        result = {}
    elif method == "session/prompt":
        result = {"stopReason": "end_turn", "usage": {"inputTokens": 1, "outputTokens": 1}}
    else:
        result = {}

    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": req_id, "result": result}) + "\n")
    sys.stdout.flush()
`
}