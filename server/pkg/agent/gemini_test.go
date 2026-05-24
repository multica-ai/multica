package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBuildGeminiArgsBaseline(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("write a haiku", ExecOptions{}, slog.Default())
	expected := []string{
		"-p", "write a haiku",
		"--dangerously-skip-permissions",
	}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Fatalf("expected args[%d] = %q, got %q", i, want, args[i])
		}
	}
}

func TestBuildGeminiArgsIgnoresModel(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{Model: "gemini-3.5-flash-high"}, slog.Default())

	for _, a := range args {
		if a == "-m" || a == "gemini-3.5-flash-high" {
			t.Fatalf("AGY does not expose a model flag; expected model to be omitted, got args=%v", args)
		}
	}
}

func TestBuildGeminiArgsWithResume(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{ResumeSessionID: "3"}, slog.Default())

	var foundResume bool
	for i, a := range args {
		if a == "--conversation" {
			if i+1 >= len(args) || args[i+1] != "3" {
				t.Fatalf("expected --conversation followed by session id, got %v", args)
			}
			foundResume = true
			break
		}
	}
	if !foundResume {
		t.Fatalf("expected --conversation flag when ResumeSessionID is set, got args=%v", args)
	}
}

func TestBuildGeminiArgsAddsWorkspaceDir(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{Cwd: "/repo"}, slog.Default())
	if len(args) < 2 || args[0] != "--add-dir" || args[1] != "/repo" {
		t.Fatalf("expected AGY workspace mount prefix, got args=%v", args)
	}
}

func TestBuildGeminiArgsOmitsModelWhenEmpty(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{}, slog.Default())
	for _, a := range args {
		if a == "-m" {
			t.Fatalf("expected no -m flag when Model is empty, got args=%v", args)
		}
		if a == "--conversation" {
			t.Fatalf("expected no --conversation flag when ResumeSessionID is empty, got args=%v", args)
		}
	}
}

func TestBuildGeminiArgsPassesThroughCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{
		CustomArgs: []string{"--sandbox"},
	}, slog.Default())

	if args[len(args)-1] != "--sandbox" {
		t.Fatalf("expected --sandbox at end of args, got %v", args)
	}
}

// envLookup returns the value of key in an env slice, or ("", false) if absent.
// When the key appears multiple times the last occurrence wins, mirroring how
// libc's getenv resolves duplicates on the daemon's supported platforms — the
// caller-supplied override therefore takes precedence over our default.
func envLookup(env []string, key string) (string, bool) {
	prefix := key + "="
	var value string
	var found bool
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			value = strings.TrimPrefix(entry, prefix)
			found = true
		}
	}
	return value, found
}

func TestBuildGeminiEnvSetsTrustWorkspaceDefault(t *testing.T) {
	t.Parallel()

	env := buildGeminiEnv(nil)
	got, ok := envLookup(env, "GEMINI_CLI_TRUST_WORKSPACE")
	if !ok {
		t.Fatalf("expected GEMINI_CLI_TRUST_WORKSPACE to be set, got env=%v", env)
	}
	if got != "true" {
		t.Fatalf("expected GEMINI_CLI_TRUST_WORKSPACE=true, got %q", got)
	}
}

func TestBuildGeminiEnvRespectsExplicitOverride(t *testing.T) {
	t.Parallel()

	// Users who deliberately set the value (e.g. to "false" to opt back into
	// the legacy Gemini folder-trust gate, or to a future-proofed value) must win over
	// our daemon default.
	env := buildGeminiEnv(map[string]string{"GEMINI_CLI_TRUST_WORKSPACE": "false"})
	got, ok := envLookup(env, "GEMINI_CLI_TRUST_WORKSPACE")
	if !ok {
		t.Fatalf("expected GEMINI_CLI_TRUST_WORKSPACE to be set, got env=%v", env)
	}
	if got != "false" {
		t.Fatalf("expected caller's GEMINI_CLI_TRUST_WORKSPACE=false to win, got %q", got)
	}
}

func TestBuildGeminiEnvPreservesOtherExtras(t *testing.T) {
	t.Parallel()

	env := buildGeminiEnv(map[string]string{"GEMINI_API_KEY": "secret"})
	if got, ok := envLookup(env, "GEMINI_API_KEY"); !ok || got != "secret" {
		t.Fatalf("expected GEMINI_API_KEY=secret to pass through, got %q (ok=%v)", got, ok)
	}
	if got, ok := envLookup(env, "GEMINI_CLI_TRUST_WORKSPACE"); !ok || got != "true" {
		t.Fatalf("expected default GEMINI_CLI_TRUST_WORKSPACE=true, got %q (ok=%v)", got, ok)
	}
}

func TestBuildGeminiArgsFiltersBlockedCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{
		CustomArgs: []string{"-o", "text", "--sandbox"},
	}, slog.Default())

	// -o text should be filtered, --sandbox should pass through
	for i, a := range args {
		if a == "-o" && i+1 < len(args) && args[i+1] == "text" {
			t.Fatalf("blocked -o text should have been filtered: %v", args)
		}
	}
	if args[len(args)-1] != "--sandbox" {
		t.Fatalf("expected --sandbox to pass through, got %v", args)
	}
}

func TestAgyAuthErrorDetectsAuthPrompt(t *testing.T) {
	t.Parallel()

	output := "Authentication required. Please visit the URL to log in:\nError: authentication timed out."
	if got := agyAuthError(output); got != "agy authentication required" {
		t.Fatalf("agyAuthError() = %q, want agy authentication required", got)
	}
}

func TestAgyAuthErrorIgnoresNormalOutput(t *testing.T) {
	t.Parallel()

	if got := agyAuthError("completed review"); got != "" {
		t.Fatalf("agyAuthError() = %q, want empty", got)
	}
}

func TestParseAgyJSONLineFromTranscript(t *testing.T) {
	t.Parallel()

	line := `{"step_index":4,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","content":"Implemented the change.","thinking":"Checking the next step.","conversationId":"conv-123"}`
	evt, ok := parseAgyJSONLine(line)
	if !ok {
		t.Fatal("expected AGY transcript line to parse")
	}
	if evt.sessionID() != "conv-123" {
		t.Fatalf("sessionID = %q, want conv-123", evt.sessionID())
	}
	messages := evt.messages()
	if len(messages) != 2 {
		t.Fatalf("expected thinking + text messages, got %+v", messages)
	}
	if messages[0].Type != MessageThinking || messages[0].Content != "Checking the next step." {
		t.Fatalf("unexpected thinking message: %+v", messages[0])
	}
	if messages[1].Type != MessageText || messages[1].Content != "Implemented the change." {
		t.Fatalf("unexpected text message: %+v", messages[1])
	}
}

func TestParseAgyJSONLineToolUseAndResult(t *testing.T) {
	t.Parallel()

	toolLine := `{"step_index":7,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"run_command","args":{"CommandLine":"\"go test ./...\"","Cwd":"\"/repo\""}}]}`
	evt, ok := parseAgyJSONLine(toolLine)
	if !ok {
		t.Fatal("expected AGY tool-call line to parse")
	}
	messages := evt.messages()
	if len(messages) != 1 {
		t.Fatalf("expected one tool-use message, got %+v", messages)
	}
	if messages[0].Type != MessageToolUse || messages[0].Tool != "run_command" {
		t.Fatalf("unexpected tool-use message: %+v", messages[0])
	}
	if messages[0].CallID != "agy-step-7-tool-0" {
		t.Fatalf("CallID = %q, want agy-step-7-tool-0", messages[0].CallID)
	}
	if messages[0].Input["CommandLine"] != "\"go test ./...\"" {
		t.Fatalf("unexpected tool input: %+v", messages[0].Input)
	}

	resultLine := `{"step_index":8,"source":"MODEL","type":"RUN_COMMAND","status":"DONE","content":"Output:\nok\n"}`
	evt, ok = parseAgyJSONLine(resultLine)
	if !ok {
		t.Fatal("expected AGY tool-result line to parse")
	}
	messages = evt.messages()
	if len(messages) != 1 || messages[0].Type != MessageToolResult {
		t.Fatalf("expected one tool-result message, got %+v", messages)
	}
	if messages[0].CallID != "agy-step-8" || messages[0].Output != "Output:\nok\n" {
		t.Fatalf("unexpected tool-result message: %+v", messages[0])
	}
}

func TestParseAgyJSONLineIgnoresPlainJSONOutput(t *testing.T) {
	t.Parallel()

	if _, ok := parseAgyJSONLine(`{"ordinary":"model output"}`); ok {
		t.Fatal("ordinary JSON output must stay on the plain text fallback path")
	}
	if _, ok := parseAgyJSONLine(`{"status":"ok"}`); ok {
		t.Fatal("plain JSON status output must stay on the plain text fallback path")
	}
}

func TestAgyUsageExtraction(t *testing.T) {
	t.Parallel()

	line := `{"conversationId":"conv-usage","metadata":{"tokenUsage":{"model":"gemini-3.5-flash-medium","inputTokens":12,"output_tokens":5,"cachedTokens":3}}}`
	evt, ok := parseAgyJSONLine(line)
	if !ok {
		t.Fatal("expected AGY metadata line to parse")
	}
	usage := evt.extractUsage()
	got := usage["gemini-3.5-flash-medium"]
	if got.InputTokens != 12 || got.OutputTokens != 5 || got.CacheReadTokens != 3 {
		t.Fatalf("unexpected usage: %+v", got)
	}
}

func TestGeminiBackendParsesAgyJSONStream(t *testing.T) {
	t.Parallel()

	binDir := t.TempDir()
	agyPath := filepath.Join(binDir, "agy")
	script := `#!/bin/sh
printf '%s\n' '{"conversationId":"conv-json","state":"running","model":"gemini-3.5-flash-medium"}'
printf '%s\n' '{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"run_command","args":{"CommandLine":"\"pwd\""}}]}'
printf '%s\n' '{"step_index":2,"source":"MODEL","type":"RUN_COMMAND","status":"DONE","content":"Output:\n/tmp\n"}'
printf '%s\n' '{"step_index":3,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","content":"Finished."}'
`
	if err := os.WriteFile(agyPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake agy: %v", err)
	}

	backend := &geminiBackend{cfg: Config{ExecutablePath: agyPath, Logger: slog.Default()}}
	session, err := backend.Execute(context.Background(), "do work", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var messages []Message
	for msg := range session.Messages {
		messages = append(messages, msg)
	}
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("result.Status = %q, error=%q", result.Status, result.Error)
	}
	if result.SessionID != "conv-json" {
		t.Fatalf("SessionID = %q, want conv-json", result.SessionID)
	}
	if result.Output != "Finished." {
		t.Fatalf("Output = %q, want Finished.", result.Output)
	}
	if !hasMessage(messages, MessageToolUse) {
		t.Fatalf("expected tool-use message, got %+v", messages)
	}
	if !hasMessage(messages, MessageToolResult) {
		t.Fatalf("expected tool-result message, got %+v", messages)
	}
	if !hasTextMessage(messages, "Finished.") {
		t.Fatalf("expected final text message, got %+v", messages)
	}
}

func TestGeminiBackendFallsBackToPlainStdoutWithHookBridge(t *testing.T) {
	t.Parallel()

	binDir := t.TempDir()
	agyPath := filepath.Join(binDir, "agy")
	if err := os.WriteFile(agyPath, []byte("#!/bin/sh\nprintf 'OK\\n'\n"), 0o755); err != nil {
		t.Fatalf("write fake agy: %v", err)
	}

	backend := &geminiBackend{cfg: Config{ExecutablePath: agyPath, Logger: slog.Default()}}
	session, err := backend.Execute(context.Background(), "do work", ExecOptions{
		Cwd:     t.TempDir(),
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("result.Status = %q, error=%q", result.Status, result.Error)
	}
	if result.Output != "OK" {
		t.Fatalf("Output = %q, want OK", result.Output)
	}
}

func TestSetupAgyHookBridgeWritesTemporaryHooks(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	bridge, err := setupAgyHookBridge(cwd, slog.Default())
	if err != nil {
		t.Fatalf("setupAgyHookBridge: %v", err)
	}
	defer bridge.cleanup(slog.Default())
	if bridge == nil || bridge.eventsPath == "" {
		t.Fatalf("expected bridge with events path, got %+v", bridge)
	}
	if strings.HasPrefix(bridge.rootDir, cwd) {
		t.Fatalf("expected temporary hook root outside task cwd, got root=%q cwd=%q", bridge.rootDir, cwd)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("expected task cwd to stay untouched, stat err=%v", err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(bridge.eventsPath), "hooks.json"))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	if !strings.Contains(string(data), "multica-agy-json-stream") {
		t.Fatalf("hooks.json missing bridge hook: %s", data)
	}
	if !strings.Contains(string(data), "PreInvocation") || !strings.Contains(string(data), "PostToolUse") {
		t.Fatalf("hooks.json missing expected AGY events: %s", data)
	}
	if !strings.Contains(string(data), `"timeout": 10`) {
		t.Fatalf("hooks.json missing second-based timeout: %s", data)
	}
	scriptPath := filepath.Join(filepath.Dir(bridge.eventsPath), "multica-agy-hook.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("hook script missing: %v", err)
	}
	if runtime.GOOS != "windows" {
		cmd := exec.Command(scriptPath)
		cmd.Stdin = strings.NewReader(`{"toolCall":{"name":"run_command","args":{"CommandLine":"pwd"}}}` + "\n")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("run hook script for PreToolUse: %v", err)
		}
		if got := strings.TrimSpace(string(out)); got != `{"decision":"allow"}` {
			t.Fatalf("PreToolUse hook output = %q, want allow decision", got)
		}

		cmd = exec.Command(scriptPath)
		cmd.Stdin = strings.NewReader(`{"stepIdx":1,"error":""}` + "\n")
		out, err = cmd.Output()
		if err != nil {
			t.Fatalf("run hook script for non-PreToolUse: %v", err)
		}
		if got := strings.TrimSpace(string(out)); got != `{}` {
			t.Fatalf("non-PreToolUse hook output = %q, want empty object", got)
		}
	}
}

func TestWatchAgyHookEventsTailsTranscriptPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	transcriptPath := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(eventsPath, nil, 0o600); err != nil {
		t.Fatalf("write events file: %v", err)
	}
	if err := os.WriteFile(transcriptPath, []byte(`{"step_index":3,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","content":"streamed from transcript"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write transcript file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan agyStreamEvent, 8)
	go watchAgyHookEvents(ctx, eventsPath, func(evt agyStreamEvent) {
		events <- evt
	}, slog.Default())

	hookPayload := `{"conversationId":"conv-hook","transcriptPath":` + strconvQuote(transcriptPath) + `}` + "\n"
	if err := os.WriteFile(eventsPath, []byte(hookPayload), 0o600); err != nil {
		t.Fatalf("write hook payload: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case evt := <-events:
			if evt.assistantText() == "streamed from transcript" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for transcript event")
		}
	}
}

func hasMessage(messages []Message, typ MessageType) bool {
	for _, msg := range messages {
		if msg.Type == typ {
			return true
		}
	}
	return false
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func hasTextMessage(messages []Message, content string) bool {
	for _, msg := range messages {
		if msg.Type == MessageText && msg.Content == content {
			return true
		}
	}
	return false
}
