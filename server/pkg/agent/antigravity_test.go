package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func quietAntigravityLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func fakeAntigravityACPScript() string {
	return `#!/bin/sh
if [ -n "$ANTIGRAVITY_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$ANTIGRAVITY_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      if [ -n "$ANTIGRAVITY_RECORD_SESSION_NEW" ]; then
        printf '%s\n' "$line" > "$ANTIGRAVITY_RECORD_SESSION_NEW"
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake_123","models":{"availableModels":[{"modelId":"gemini-3.8-flash-high","name":"Gemini 3.8 Flash (High)"}],"currentModelId":"gemini-3.8-flash-high"}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      if [ "$ANTIGRAVITY_REJECT_RESUME" = "1" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32002,"message":"Session not found in the current GEMINI_HOME"}}\n' "$id"
      else
        if [ -n "$ANTIGRAVITY_RECORD_SESSION_RESUME" ]; then
          printf '%s\n' "$line" > "$ANTIGRAVITY_RECORD_SESSION_RESUME"
        fi
        printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake_123"}}\n' "$id"
      fi
      ;;
    *'"method":"session/set_mode"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      if echo "$line" | grep -q 'bogus-model'; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"Model not available: bogus-model"}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      fi
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_fake_123","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Done with task"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func fakeAntigravityACPScriptWithTools() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_tools_456"}}\n' "$id"
      ;;
    *'"method":"session/set_mode"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_tools_456","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Inspecting files..."}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_tools_456","update":{"sessionUpdate":"tool_call","toolCallId":"tc-1","title":"Running view_file","kind":"read","rawInput":{"AbsolutePath":"/test/file.txt"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_tools_456","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-1","status":"completed"}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_tools_456","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Final answer after tool."}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func makeFakeAntigravityExecutable(t *testing.T, script string) string {
	t.Helper()
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "agy_acp_server")
	writeTestExecutable(t, binPath, []byte(script))
	return binPath
}

func TestAntigravityToolNameFromTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title string
		kind  string
		want  string
	}{
		{"Running view_file", "read", "view_file"},
		{"Running run_command", "execute", "run_command"},
		{"Running write_to_file", "write", "write_to_file"},
		{"Running replace_file_content", "edit", "replace_file_content"},
		{"terminal: ls -la", "execute", "terminal"},
		{"read: /etc/hosts", "read", "read_file"},
		{"custom_tool", "other", "custom_tool"},
	}

	for _, tc := range tests {
		got := antigravityToolNameFromTitle(tc.title, tc.kind)
		if got != tc.want {
			t.Errorf("antigravityToolNameFromTitle(%q, %q) = %q, want %q", tc.title, tc.kind, got, tc.want)
		}
	}
}

func TestAntigravityACPLaunchArgs(t *testing.T) {
	t.Parallel()

	args := antigravityACPLaunchArgs()
	if runtime.GOOS == "linux" {
		if !slices.Equal(args, []string{"--uid="}) {
			t.Fatalf("antigravityACPLaunchArgs on linux = %v, want [--uid=]", args)
		}
	} else {
		if len(args) != 0 {
			t.Fatalf("antigravityACPLaunchArgs on %s = %v, want empty", runtime.GOOS, args)
		}
	}
}

func TestAntigravityDefaultExecutable(t *testing.T) {
	t.Parallel()

	if got := antigravityDefaultExecutable(); got != "agy_acp_server" {
		t.Errorf("antigravityDefaultExecutable = %q, want agy_acp_server", got)
	}
}

func TestAntigravityBlockedArgs(t *testing.T) {
	t.Parallel()

	customArgs := []string{"--debug", "--notices", "--help", "-h", "--helpfull"}
	filtered := filterCustomArgs(customArgs, antigravityBlockedArgs, quietAntigravityLogger())

	if slices.Contains(filtered, "--notices") || slices.Contains(filtered, "--help") || slices.Contains(filtered, "-h") || slices.Contains(filtered, "--helpfull") {
		t.Fatalf("filterCustomArgs did not strip blocked args: %v", filtered)
	}
	if !slices.Contains(filtered, "--debug") {
		t.Fatalf("filterCustomArgs unexpectedly stripped --debug: %v", filtered)
	}
}

func TestEnsureAntigravityHarnessEnv(t *testing.T) {
	t.Parallel()

	// Existing env with ANTIGRAVITY_HARNESS_PATH should be preserved
	existing := map[string]string{"ANTIGRAVITY_HARNESS_PATH": "/custom/path"}
	got := ensureAntigravityHarnessEnv(existing, "/usr/bin/agy_acp_server")
	if got["ANTIGRAVITY_HARNESS_PATH"] != "/custom/path" {
		t.Errorf("ensureAntigravityHarnessEnv did not preserve existing env: %v", got)
	}

	// When localharness_external is next to executable, inject ANTIGRAVITY_HARNESS_PATH
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "agy_acp_server")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	harnessPath := filepath.Join(tmpDir, "localharness_external")
	if err := os.WriteFile(harnessPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	empty := map[string]string{}
	injected := ensureAntigravityHarnessEnv(empty, binPath)
	if injected["ANTIGRAVITY_HARNESS_PATH"] != harnessPath {
		t.Errorf("ensureAntigravityHarnessEnv = %v, want %q", injected, harnessPath)
	}
}

func TestAntigravityModelError(t *testing.T) {
	t.Parallel()

	models := []Model{
		{ID: "gemini-3.8-flash-high", Label: "Gemini 3.8 Flash (High)"},
		{ID: "gemini-3.7-flash-high", Label: "Gemini 3.7 Flash (High)"},
	}

	if err := antigravityModelError("gemini-3.8-flash-high", models); err != nil {
		t.Errorf("antigravityModelError valid model returned err: %v", err)
	}
	if err := antigravityModelError("", models); err != nil {
		t.Errorf("antigravityModelError empty model returned err: %v", err)
	}
	if err := antigravityModelError("gemini-9.9", nil); err != nil {
		t.Errorf("antigravityModelError empty models slice returned err: %v", err)
	}
	if err := antigravityModelError("nonexistent", models); err == nil {
		t.Errorf("antigravityModelError invalid model returned nil, want error")
	}
}

func TestAntigravityExecuteSuccess(t *testing.T) {
	t.Parallel()

	binPath := makeFakeAntigravityExecutable(t, fakeAntigravityACPScript())
	backend := &antigravityBackend{
		cfg: Config{
			ExecutablePath: binPath,
			Logger:         quietAntigravityLogger(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "Hello", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var messages []Message
	for msg := range session.Messages {
		messages = append(messages, msg)
	}

	res := <-session.Result
	if res.Status != "completed" {
		t.Fatalf("status = %q, error = %q, want completed", res.Status, res.Error)
	}
	if res.SessionID != "ses_fake_123" {
		t.Errorf("sessionID = %q, want ses_fake_123", res.SessionID)
	}
	if !strings.Contains(res.Output, "Done with task") {
		t.Errorf("output = %q, want 'Done with task'", res.Output)
	}
	if len(messages) == 0 {
		t.Errorf("expected streamed messages, got none")
	}
}

func TestAntigravityExecuteDeliverableWithTools(t *testing.T) {
	t.Parallel()

	binPath := makeFakeAntigravityExecutable(t, fakeAntigravityACPScriptWithTools())
	backend := &antigravityBackend{
		cfg: Config{
			ExecutablePath: binPath,
			Logger:         quietAntigravityLogger(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "Run tool", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var toolSeen bool
	for msg := range session.Messages {
		if msg.Type == MessageToolUse && (msg.Tool == "read_file" || msg.Tool == "view_file") {
			toolSeen = true
		}
	}

	if !toolSeen {
		t.Errorf("expected read_file or view_file tool invocation in messages")
	}

	res := <-session.Result
	if res.Status != "completed" {
		t.Fatalf("status = %q, want completed", res.Status)
	}
	// Deliverable tracker isolates post-tool user-facing response
	if !strings.Contains(res.Output, "Final answer after tool.") {
		t.Errorf("output = %q, want post-tool final answer", res.Output)
	}
}

func fakeAntigravityMultiTurnACPScript() string {
	return `#!/bin/sh
if [ -n "$ANTIGRAVITY_LAUNCH_COUNT_FILE" ]; then
  printf 'launch\n' >> "$ANTIGRAVITY_LAUNCH_COUNT_FILE"
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_multiturn_123","models":{"availableModels":[{"modelId":"gemini-3.8-flash-high","name":"Gemini 3.8 Flash (High)"}],"currentModelId":"gemini-3.8-flash-high"}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_multiturn_123"}}\n' "$id"
      ;;
    *'"method":"session/set_mode"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      if [ -n "$ANTIGRAVITY_RECORD_SET_MODEL" ]; then
        printf '%s\n' "$line" >> "$ANTIGRAVITY_RECORD_SET_MODEL"
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      if [ -n "$ANTIGRAVITY_PROMPT_COUNT_FILE" ]; then
        printf 'prompt\n' >> "$ANTIGRAVITY_PROMPT_COUNT_FILE"
      fi
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_multiturn_123","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Multi-turn response"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      ;;
  esac
done
`
}

func TestAntigravityExecuteMultiTurnSessionReuse(t *testing.T) {
	clearAntigravitySessionPool()
	t.Cleanup(clearAntigravitySessionPool)

	tmpDir := t.TempDir()
	launchCountFile := filepath.Join(tmpDir, "launch_count.txt")
	promptCountFile := filepath.Join(tmpDir, "prompt_count.txt")
	setModelRecordFile := filepath.Join(tmpDir, "set_model.txt")

	t.Setenv("ANTIGRAVITY_LAUNCH_COUNT_FILE", launchCountFile)
	t.Setenv("ANTIGRAVITY_PROMPT_COUNT_FILE", promptCountFile)
	t.Setenv("ANTIGRAVITY_RECORD_SET_MODEL", setModelRecordFile)

	binPath := makeFakeAntigravityExecutable(t, fakeAntigravityMultiTurnACPScript())
	backend := &antigravityBackend{
		cfg: Config{
			ExecutablePath: binPath,
			Logger:         quietAntigravityLogger(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Turn 1: Cold start, creates session
	session1, err := backend.Execute(ctx, "Turn 1 prompt", ExecOptions{Model: "gemini-3.8-flash-high"})
	if err != nil {
		t.Fatalf("turn 1 execute: %v", err)
	}
	for range session1.Messages {
	}
	res1 := <-session1.Result
	if res1.Status != "completed" {
		t.Fatalf("turn 1 status = %q, want completed", res1.Status)
	}
	if res1.SessionID != "ses_multiturn_123" {
		t.Fatalf("turn 1 session ID = %q, want ses_multiturn_123", res1.SessionID)
	}

	// Turn 2: Warm resume with same session ID, switching model to test session/set_model
	session2, err := backend.Execute(ctx, "Turn 2 prompt", ExecOptions{
		ResumeSessionID: res1.SessionID,
		Model:           "gemini-3.5-pro",
	})
	if err != nil {
		t.Fatalf("turn 2 execute: %v", err)
	}
	for range session2.Messages {
	}
	res2 := <-session2.Result
	if res2.Status != "completed" {
		t.Fatalf("turn 2 status = %q, want completed", res2.Status)
	}
	if res2.SessionID != "ses_multiturn_123" {
		t.Fatalf("turn 2 session ID = %q, want ses_multiturn_123", res2.SessionID)
	}

	// Verify only 1 process launch occurred (warm process reused)!
	launchBytes, err := os.ReadFile(launchCountFile)
	if err != nil {
		t.Fatalf("read launch count: %v", err)
	}
	launches := strings.Count(string(launchBytes), "launch")
	if launches != 1 {
		t.Errorf("launch count = %d, want 1 (warm process should be reused)", launches)
	}

	// Verify 2 prompts executed!
	promptBytes, err := os.ReadFile(promptCountFile)
	if err != nil {
		t.Fatalf("read prompt count: %v", err)
	}
	prompts := strings.Count(string(promptBytes), "prompt")
	if prompts != 2 {
		t.Errorf("prompt count = %d, want 2", prompts)
	}

	// Verify model switch called session/set_model on the warm connection
	setModelBytes, err := os.ReadFile(setModelRecordFile)
	if err != nil {
		t.Fatalf("read set_model record: %v", err)
	}
	if !strings.Contains(string(setModelBytes), "gemini-3.5-pro") {
		t.Errorf("expected session/set_model with gemini-3.5-pro, got %s", string(setModelBytes))
	}
}

func TestAntigravityExecuteMultiTurnSessionReuseDisabled(t *testing.T) {
	clearAntigravitySessionPool()
	t.Cleanup(clearAntigravitySessionPool)

	tmpDir := t.TempDir()
	launchCountFile := filepath.Join(tmpDir, "launch_count.txt")
	promptCountFile := filepath.Join(tmpDir, "prompt_count.txt")

	t.Setenv("ANTIGRAVITY_LAUNCH_COUNT_FILE", launchCountFile)
	t.Setenv("ANTIGRAVITY_PROMPT_COUNT_FILE", promptCountFile)
	t.Setenv("MULTICA_ANTIGRAVITY_DISABLE_SESSION_REUSE", "1")

	binPath := makeFakeAntigravityExecutable(t, fakeAntigravityMultiTurnACPScript())
	backend := &antigravityBackend{
		cfg: Config{
			ExecutablePath: binPath,
			Logger:         quietAntigravityLogger(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	session1, err := backend.Execute(ctx, "Turn 1", ExecOptions{})
	if err != nil {
		t.Fatalf("turn 1 execute: %v", err)
	}
	for range session1.Messages {
	}
	res1 := <-session1.Result
	if res1.Status != "completed" {
		t.Fatalf("turn 1 status = %q", res1.Status)
	}

	session2, err := backend.Execute(ctx, "Turn 2", ExecOptions{ResumeSessionID: res1.SessionID})
	if err != nil {
		t.Fatalf("turn 2 execute: %v", err)
	}
	for range session2.Messages {
	}
	res2 := <-session2.Result
	if res2.Status != "completed" {
		t.Fatalf("turn 2 status = %q", res2.Status)
	}

	launchBytes, _ := os.ReadFile(launchCountFile)
	launches := strings.Count(string(launchBytes), "launch")
	if launches != 2 {
		t.Errorf("launch count = %d, want 2 when session reuse is disabled", launches)
	}
}

func TestAntigravityExecuteWarmFallbackOnDeadProcess(t *testing.T) {
	clearAntigravitySessionPool()
	t.Cleanup(clearAntigravitySessionPool)

	binPath := makeFakeAntigravityExecutable(t, fakeAntigravityACPScript())
	backend := &antigravityBackend{
		cfg: Config{
			ExecutablePath: binPath,
			Logger:         quietAntigravityLogger(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	session1, err := backend.Execute(ctx, "Turn 1", ExecOptions{})
	if err != nil {
		t.Fatalf("turn 1 execute: %v", err)
	}
	for range session1.Messages {
	}
	res1 := <-session1.Result
	if res1.Status != "completed" {
		t.Fatalf("turn 1 status = %q", res1.Status)
	}

	// Turn 2 with ResumeSessionID: fakeAntigravityACPScript exited after Turn 1.
	// alive() will report false, so executeACP falls back cleanly to a new process and resumes.
	session2, err := backend.Execute(ctx, "Turn 2", ExecOptions{ResumeSessionID: res1.SessionID})
	if err != nil {
		t.Fatalf("turn 2 execute: %v", err)
	}
	for range session2.Messages {
	}
	res2 := <-session2.Result
	if res2.Status != "completed" {
		t.Fatalf("turn 2 status = %q", res2.Status)
	}
}


func fakeAgyStreamJSONScript() string {
	return `#!/bin/sh
log=""
while [ $# -gt 0 ]; do
  case "$1" in
    --log-file) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -z "$log" ] || printf '%s\n' 'I0903 12:00:00.000000 1 model_config_manager.go:311] Propagating selected model override to backend: label="Gemini 3.8 Flash (High)"' >> "$log"
cat <<'JSONL'
{"event":"init","conversation_id":"07a6d8f2-8523-46fc-8fc9-87e630cbe295","init":{}}
{"event":"step_update","step_update":{"conversation_id":"07a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":2,"state":"ACTIVE","step_type":"agent_response","text_delta":"Hello"}}
{"event":"step_update","step_update":{"conversation_id":"07a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":2,"state":"DONE","step_type":"agent_response","text_delta":"\n","usage":{"input_tokens":1000,"output_tokens":50,"thinking_tokens":30,"cache_read_tokens":400,"cache_write_tokens":0,"total_tokens":1050}}}
{"event":"step_update","step_update":{"conversation_id":"07a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":3,"state":"DONE","step_type":"checkpoint","usage":{"input_tokens":100,"output_tokens":4,"thinking_tokens":0,"cache_read_tokens":0,"cache_write_tokens":0,"total_tokens":104}}}
{"event":"result","result":{"conversation_id":"07a6d8f2-8523-46fc-8fc9-87e630cbe295","status":"SUCCESS","response":"Hello\n","usage":{"input_tokens":5000,"output_tokens":200,"thinking_tokens":120,"cache_read_tokens":1200,"cache_write_tokens":0,"total_tokens":5200}}}
JSONL
exit 0
`
}

func fakeAgyFailedStreamJSONScript() string {
	return `#!/bin/sh
printf '%s\n' '{"event":"result","result":{"conversation_id":"17a6d8f2-8523-46fc-8fc9-87e630cbe295","status":"ERROR","error":"provider failed","usage":{"input_tokens":12,"output_tokens":3,"thinking_tokens":2,"cache_read_tokens":7,"cache_write_tokens":2,"total_tokens":15}}}'
exit 1
`
}

func fakeAgyTrailingNetworkErrorScript(responseDone bool) string {
	state := "ACTIVE"
	if responseDone {
		state = "DONE"
	}
	return fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"event":"step_update","step_update":{"conversation_id":"37a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":2,"state":"%s","step_type":"agent_response","text_delta":"Complete answer."}}'
printf '%%s\n' '{"event":"result","result":{"conversation_id":"37a6d8f2-8523-46fc-8fc9-87e630cbe295","status":"ERROR","response":"Complete answer.","error":"There was a network issue connecting to the server, please try again."}}'
exit 1
`, state)
}

// fakeAgyTrailingNetworkErrorWithStaleActiveStepScript reproduces the real
// agy 1.1.27 sequence from 2026-09-07: a background command reports ACTIVE,
// is later cancelled through a separate manage_task step (so the original
// step never receives DONE), and the agent still emits a complete DONE reply
// before the exact trailing network error.
func fakeAgyTrailingNetworkErrorWithStaleActiveStepScript() string {
	return `#!/bin/sh
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"47a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":1,"state":"ACTIVE","step_type":"tool"}}'
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"47a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":2,"state":"DONE","step_type":"tool"}}'
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"47a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":3,"state":"DONE","step_type":"agent_response","text_delta":"Complete answer after cancelling the background task."}}'
printf '%s\n' '{"event":"result","result":{"conversation_id":"47a6d8f2-8523-46fc-8fc9-87e630cbe295","status":"ERROR","response":"Complete answer after cancelling the background task.","error":"There was a network issue connecting to the server, please try again."}}'
exit 1
`
}

// fakeAgyTrailingTransportErrorScript reproduces the real agy 1.2.13/1.2.14
// sequence observed on 2026-09-30: the agent emits a complete DONE reply, agy
// then makes one more streamGenerateContent call whose round trip never
// produces a response, and reports the exhausted retries as
// `API error (attempt N): request failed: Post "...": EOF`. This is the same
// shape as the trailing network error above — a finished answer followed by a
// transport failure — spelled differently, so it must be preserved too.
func fakeAgyTrailingTransportErrorScript() string {
	return `#!/bin/sh
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"67a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":1,"state":"DONE","step_type":"agent_response","text_delta":"Complete answer before the transport failure."}}'
printf '%s\n' '{"event":"result","result":{"conversation_id":"67a6d8f2-8523-46fc-8fc9-87e630cbe295","status":"ERROR","response":"Complete answer before the transport failure.","error":"API error (attempt 3): request failed: Post \"https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse\": EOF"}}'
exit 1
`
}

// fakeAgyTrailingNonTransportErrorScript is the negative control for the above:
// the answer is complete and DONE, but the trailing failure is a provider-side
// quota/capacity error rather than a transport one. That is a real failure the
// user must see, so it must stay failed.
func fakeAgyTrailingNonTransportErrorScript() string {
	return `#!/bin/sh
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"77a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":1,"state":"DONE","step_type":"agent_response","text_delta":"Answer produced before quota ran out."}}'
printf '%s\n' '{"event":"result","result":{"conversation_id":"77a6d8f2-8523-46fc-8fc9-87e630cbe295","status":"ERROR","response":"Answer produced before quota ran out.","error":"API error (attempt 3): model capacity exhausted, retry later"}}'
exit 1
`
}

// fakeAgyTrailingNetworkErrorAfterNewPartialResponseScript guards against a
// completed earlier answer making a later, interrupted answer look complete.
func fakeAgyTrailingNetworkErrorAfterNewPartialResponseScript() string {
	return `#!/bin/sh
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"57a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":2,"state":"DONE","step_type":"agent_response","text_delta":"Earlier answer."}}'
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"57a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":3,"state":"DONE","step_type":"tool"}}'
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"57a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":4,"state":"ACTIVE","step_type":"agent_response","text_delta":"Partial final answer."}}'
printf '%s\n' '{"event":"result","result":{"conversation_id":"57a6d8f2-8523-46fc-8fc9-87e630cbe295","status":"ERROR","response":"Partial final answer.","error":"There was a network issue connecting to the server, please try again."}}'
exit 1
`
}

func fakeAgyCancelledStreamJSONScript() string {
	return `#!/bin/sh
printf '%s\n' '{"event":"init","conversation_id":"27a6d8f2-8523-46fc-8fc9-87e630cbe295","init":{"model":"gemini-3.8-flash-high"}}'
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"27a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":1,"state":"DONE","step_type":"tool","usage":{"input_tokens":100,"output_tokens":10,"cache_read_tokens":20,"total_tokens":110}}}'
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"27a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":1,"state":"DONE","step_type":"tool","usage":{"input_tokens":120,"output_tokens":12,"cache_read_tokens":20,"total_tokens":132}}}'
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"27a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":2,"state":"DONE","step_type":"agent_response","text_delta":"working","usage":{"input_tokens":50,"output_tokens":5,"cache_read_tokens":10,"total_tokens":55}}}'
sleep 60
`
}

func TestAntigravityBackendStreamJSONCapturesUsage(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyStreamJSONScript()))

	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var streamed strings.Builder
	for msg := range session.Messages {
		if msg.Type == MessageText {
			streamed.WriteString(msg.Content)
		}
	}
	result, ok := <-session.Result
	if !ok {
		t.Fatal("result channel closed without a value")
	}
	if result.Status != "completed" || result.Output != "Hello\n" {
		t.Fatalf("result = status %q output %q error %q", result.Status, result.Output, result.Error)
	}
	if streamed.String() != "Hello\n" {
		t.Fatalf("streamed output = %q, want %q", streamed.String(), "Hello\n")
	}
	if result.SessionID != "07a6d8f2-8523-46fc-8fc9-87e630cbe295" {
		t.Fatalf("session id = %q", result.SessionID)
	}
	usage, ok := result.Usage["Gemini 3.8 Flash (High)"]
	if !ok {
		t.Fatalf("usage missing model key: %#v", result.Usage)
	}
	// result.usage is cumulative across a resumed conversation. The two DONE
	// steps are this execution's usage and must win without double-counting the
	// terminal aggregate.
	want := TokenUsage{InputTokens: 1100, OutputTokens: 54, CacheReadTokens: 400}
	if usage != want {
		t.Fatalf("usage = %+v, want %+v", usage, want)
	}
}

func TestAntigravityBackendIgnoresTrailingNetworkErrorAfterDoneResponse(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyTrailingNetworkErrorScript(true)))

	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	result, ok := <-session.Result
	if !ok {
		t.Fatal("result channel closed without a value")
	}
	if result.Status != "completed" || result.Output != "Complete answer." || result.Error != "" {
		t.Fatalf("result = status %q output %q error %q", result.Status, result.Output, result.Error)
	}
}

// agy 1.4.6 can retain an earlier interruption in the result of a resumed
// conversation, even after the current response finishes successfully (#9528).
func TestAntigravityBackendRecoveredStreamInterruption(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		done     bool
		exitCode int
	}{
		{done: true, exitCode: 0},
		{done: false, exitCode: 0},
		{done: true, exitCode: 1},
	} {
		t.Run(fmt.Sprintf("response_done=%v/exit=%d", tc.done, tc.exitCode), func(t *testing.T) {
			t.Parallel()
			fakePath := filepath.Join(t.TempDir(), "agy")
			script := strings.ReplaceAll(fakeAgyTrailingNetworkErrorScript(tc.done), antigravityNetworkIssueError,
				"The stream was interrupted. Please continue the task you were working on.")
			script = strings.ReplaceAll(script, "exit 1", fmt.Sprintf("exit %d", tc.exitCode))
			writeTestExecutable(t, fakePath, []byte(script))

			backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
			if err != nil {
				t.Fatalf("new antigravity backend: %v", err)
			}
			session, err := backend.Execute(context.Background(), "follow-up", ExecOptions{
				ResumeSessionID: "37a6d8f2-8523-46fc-8fc9-87e630cbe295",
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			for range session.Messages {
			}
			result, ok := <-session.Result
			if !ok {
				t.Fatal("result channel closed without a value")
			}
			wantStatus := "failed"
			completed := tc.done && tc.exitCode == 0
			if completed {
				wantStatus = "completed"
			}
			if result.Status != wantStatus || result.Output != "Complete answer." || (result.Error == "") != completed {
				t.Fatalf("result = status %q output %q error %q", result.Status, result.Output, result.Error)
			}
		})
	}
}

func TestAntigravityBackendIgnoresStaleActiveStepAfterDoneResponse(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyTrailingNetworkErrorWithStaleActiveStepScript()))

	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	result, ok := <-session.Result
	if !ok {
		t.Fatal("result channel closed without a value")
	}
	if result.Status != "completed" || result.Output != "Complete answer after cancelling the background task." || result.Error != "" {
		t.Fatalf("result = status %q output %q error %q", result.Status, result.Output, result.Error)
	}
}

// A complete DONE answer followed by an exhausted-retry transport error is a
// finished turn. agy spells such a failure `API error (attempt N): request
// failed: Post "...": EOF` rather than the network-issue sentence, and matching
// only the latter discarded an answer that had already been produced and
// streamed (2026-09-30: every turn of one conversation reported
// blocked/agent_error.unknown while the reply sat in result.response).
func TestAntigravityBackendIgnoresTrailingTransportErrorAfterDoneResponse(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyTrailingTransportErrorScript()))

	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	result, ok := <-session.Result
	if !ok {
		t.Fatal("result channel closed without a value")
	}
	if result.Status != "completed" || result.Output != "Complete answer before the transport failure." || result.Error != "" {
		t.Fatalf("result = status %q output %q error %q", result.Status, result.Output, result.Error)
	}
}

// The negative control: a complete DONE answer does not on its own make a turn
// successful. When the trailing failure is provider-side (quota, capacity,
// policy, auth) rather than transport, the turn must stay failed so the user
// sees the real cause instead of an answer presented as a finished result.
func TestAntigravityBackendKeepsNonTransportFailureAfterDoneResponse(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyTrailingNonTransportErrorScript()))

	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	result, ok := <-session.Result
	if !ok {
		t.Fatal("result channel closed without a value")
	}
	if result.Status != "failed" || result.Output != "Answer produced before quota ran out." || !strings.Contains(result.Error, "model capacity exhausted") {
		t.Fatalf("result = status %q output %q error %q", result.Status, result.Output, result.Error)
	}
}

// TestAntigravityTrailingTransportErrorClassification pins both edges of the
// classifier. The "want true" half is the field-observed spelling plus the
// other causes Go's http client reports for a round trip that never produced a
// response. The "want false" half is the one that matters: a provider
// rejection that happens to travel inside agy's `request failed:` wrapper must
// stay a failure, otherwise a quota or overload error would be laundered into a
// successful turn just because the model had already emitted some text.
func TestAntigravityTrailingTransportErrorClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  string
		want bool
	}{
		{
			name: "field observed EOF after exhausted retries",
			err:  `API error (attempt 3): request failed: Post "https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse": EOF`,
			want: true,
		},
		{
			name: "legacy provider sentence",
			err:  antigravityNetworkIssueError,
			want: true,
		},
		{
			name: "legacy provider sentence in other case and padding",
			err:  "  THERE WAS A NETWORK ISSUE CONNECTING TO THE SERVER, PLEASE TRY AGAIN.  ",
			want: true,
		},
		{name: "recovered stream interruption", err: "The stream was interrupted. Please continue the task you were working on.", want: true},
		{name: "unexpected EOF", err: `Post "https://host/v1": unexpected EOF`, want: true},
		{name: "connection reset", err: `read tcp 1.2.3.4:443: connection reset by peer`, want: true},
		{name: "broken pipe", err: `write tcp 1.2.3.4:443: broken pipe`, want: true},
		{name: "connection refused", err: `dial tcp 1.2.3.4:443: connection refused`, want: true},
		{name: "socket i/o timeout", err: `read tcp 1.2.3.4:443: i/o timeout`, want: true},
		{name: "tls handshake timeout", err: `net/http: TLS handshake timeout`, want: true},
		{name: "tls handshake failure", err: `remote error: tls: handshake failure`, want: true},
		{name: "closed connection", err: `use of closed network connection`, want: true},
		{name: "dns no such host", err: `dial tcp: lookup host: no such host`, want: true},
		{name: "dns server misbehaving", err: `lookup host: server misbehaving`, want: true},
		{name: "network unreachable", err: `connect: network is unreachable`, want: true},
		{name: "malformed response", err: `malformed HTTP response`, want: true},
		{name: "http2 connection lost", err: `http2: client connection lost`, want: true},
		{name: "http2 goaway", err: `http2: server sent GOAWAY and closed the connection`, want: true},

		{name: "empty", err: "", want: false},
		{name: "whitespace only", err: "   ", want: false},
		{
			name: "quota rejection inside the request-failed wrapper",
			err:  `API error (attempt 3): request failed: 429 Too Many Requests`,
			want: false,
		},
		{
			name: "status error inside the request-failed wrapper",
			err:  `API error (attempt 3): request failed: unexpected status code 503`,
			want: false,
		},
		{name: "model capacity exhausted", err: `API error (attempt 3): model capacity exhausted, retry later`, want: false},
		{name: "prefill queue overloaded", err: `PREFILL_QUEUE_OVERLOADED: Overloaded`, want: false},
		{name: "resource exhausted", err: `RESOURCE_EXHAUSTED: Quota exceeded for quota group`, want: false},
		{name: "safety policy", err: `The request was rejected by the safety filter`, want: false},
		{name: "auth", err: `unauthenticated: invalid API key`, want: false},
		{name: "invalid argument", err: `INVALID_ARGUMENT: request contains an invalid argument`, want: false},
		{name: "permission denied", err: `permission denied`, want: false},
		{name: "tool crash", err: `agent executor error: tool crashed`, want: false},
		{name: "bare status text", err: `agy returned status ERROR`, want: false},
		{name: "cancelled", err: `execution cancelled`, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := antigravityTrailingTransportError(tc.err); got != tc.want {
				t.Fatalf("antigravityTrailingTransportError(%q) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// The classifier must not fire on a complete answer alone. Each of these has a
// non-empty response and a DONE agent_response step, so the error class is the
// only thing standing between a preserved answer and a laundered failure.
func TestAntigravityCompletedDespiteTrailingNetworkErrorRequiresAllThree(t *testing.T) {
	t.Parallel()

	const transport = `API error (attempt 3): request failed: Post "https://host/v1": EOF`
	const quota = `API error (attempt 3): model capacity exhausted`

	tests := []struct {
		name              string
		err               string
		response          string
		agentResponseDone bool
		want              bool
	}{
		{name: "all three hold", err: transport, response: "answer", agentResponseDone: true, want: true},
		{name: "response whitespace only", err: transport, response: "  \n ", agentResponseDone: true, want: false},
		{name: "response empty", err: transport, response: "", agentResponseDone: true, want: false},
		{name: "answer still being written", err: transport, response: "partial", agentResponseDone: false, want: false},
		{name: "interrupted response whitespace only", err: "The stream was interrupted. Please continue the task you were working on.", response: "  ", agentResponseDone: true, want: false},
		{name: "provider rejection", err: quota, response: "answer", agentResponseDone: true, want: false},
		{name: "no error at all", err: "", response: "answer", agentResponseDone: true, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := antigravityCompletedDespiteTrailingNetworkError(tc.err, tc.response, tc.agentResponseDone, false)
			if got != tc.want {
				t.Fatalf("antigravityCompletedDespiteTrailingNetworkError(%q, %q, %v) = %v, want %v",
					tc.err, tc.response, tc.agentResponseDone, got, tc.want)
			}
		})
	}
}

func TestAntigravityBackendKeepsNetworkFailureForPartialResponse(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyTrailingNetworkErrorScript(false)))

	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	result, ok := <-session.Result
	if !ok {
		t.Fatal("result channel closed without a value")
	}
	if result.Status != "failed" || result.Output != "Complete answer." || !strings.Contains(result.Error, antigravityNetworkIssueError) {
		t.Fatalf("result = status %q output %q error %q", result.Status, result.Output, result.Error)
	}
}

func TestAntigravityBackendKeepsNetworkFailureForNewerPartialResponse(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyTrailingNetworkErrorAfterNewPartialResponseScript()))

	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	result, ok := <-session.Result
	if !ok {
		t.Fatal("result channel closed without a value")
	}
	if result.Status != "failed" || result.Output != "Partial final answer." || !strings.Contains(result.Error, antigravityNetworkIssueError) {
		t.Fatalf("result = status %q output %q error %q", result.Status, result.Output, result.Error)
	}
}

func TestAntigravityStreamUsageMapsProviderBucketsDirectly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  antigravityStreamUsage
		want TokenUsage
	}{
		{
			name: "official example cache larger than input",
			raw:  antigravityStreamUsage{InputTokens: 278, OutputTokens: 4, CacheReadTokens: 30214, TotalTokens: 282},
			want: TokenUsage{InputTokens: 278, OutputTokens: 4, CacheReadTokens: 30214},
		},
		{
			name: "official example cache smaller than input",
			raw:  antigravityStreamUsage{InputTokens: 10418, OutputTokens: 589, CacheReadTokens: 8113, TotalTokens: 11007},
			want: TokenUsage{InputTokens: 10418, OutputTokens: 589, CacheReadTokens: 8113},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.raw.tokenUsage(); got != tt.want {
				t.Fatalf("tokenUsage() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReadAntigravitySelectedModelPrecedence(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "agy.log")
	if err := os.WriteFile(logPath, []byte(
		`I0903 12:00:00 model_config_manager.go:311] Propagating selected model override to backend: label="Gemini 3.8 Flash (High)"`+"\n",
	), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if got := readAntigravitySelectedModel(logPath, "gemini-stream-model", "gemini-configured-model"); got != "gemini-stream-model" {
		t.Fatalf("stream model = %q", got)
	}
	if got := readAntigravitySelectedModel(logPath, "", "gemini-configured-model"); got != "Gemini 3.8 Flash (High)" {
		t.Fatalf("log model = %q", got)
	}
	if got := readAntigravitySelectedModel("", "", "gemini-configured-model"); got != "gemini-configured-model" {
		t.Fatalf("configured model = %q", got)
	}
	if got := readAntigravitySelectedModel("", "", ""); got != "unknown" {
		t.Fatalf("fallback model = %q", got)
	}
}

func TestAntigravityResultStatus(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"SUCCESS":        "completed",
		"completed":      "completed",
		"CANCELLED":      "cancelled",
		"canceled":       "cancelled",
		"ABORTED":        "aborted",
		"TIMEOUT":        "timeout",
		"timed_out":      "timeout",
		"provider_error": "failed",
	}
	for input, want := range tests {
		if got := antigravityResultStatus(input); got != want {
			t.Errorf("antigravityResultStatus(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAntigravityBackendReportsStructuredFailureUsage(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyFailedStreamJSONScript()))
	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{Model: ""})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "failed" || result.Error != "provider failed" {
		t.Fatalf("result = status %q error %q", result.Status, result.Error)
	}
	want := TokenUsage{InputTokens: 12, OutputTokens: 3, CacheReadTokens: 7, CacheWriteTokens: 2}
	if got := result.Usage["unknown"]; got != want {
		t.Fatalf("usage = %+v, want %+v", got, want)
	}
}

func TestAntigravityBackendSumsCompletedStepsWhenCancelled(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyCancelledStreamJSONScript()))
	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for msg := range session.Messages {
		if msg.Type == MessageText && msg.Content == "working" {
			cancel()
		}
	}
	result := <-session.Result
	if result.Status != "aborted" {
		t.Fatalf("status = %q, want aborted (error=%q)", result.Status, result.Error)
	}
	// step 1's second DONE event replaces its first snapshot; step 2 is then
	// added once. Provider buckets retain their documented meanings.
	want := TokenUsage{InputTokens: 170, OutputTokens: 17, CacheReadTokens: 30}
	if got := result.Usage["gemini-3.8-flash-high"]; got != want {
		t.Fatalf("usage = %+v, want %+v", got, want)
	}
}

func TestAntigravityExecuteWithMcpServers(t *testing.T) {
	t.Parallel()

	recordFile := filepath.Join(t.TempDir(), "session_new.json")

	binPath := makeFakeAntigravityExecutable(t, fakeAntigravityACPScript())
	backend := &antigravityBackend{
		cfg: Config{
			ExecutablePath: binPath,
			Logger:         quietAntigravityLogger(),
			Env:            map[string]string{"ANTIGRAVITY_RECORD_SESSION_NEW": recordFile},
		},
	}

	mcpConfig := json.RawMessage(`{
		"mcpServers": {
			"test-server": {
				"command": "node",
				"args": ["server.js"]
			}
		}
	}`)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "Test with MCP", ExecOptions{McpConfig: mcpConfig})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	for range session.Messages {
	}
	res := <-session.Result
	if res.Status != "completed" {
		t.Fatalf("status = %q, want completed", res.Status)
	}

	data, err := os.ReadFile(recordFile)
	if err != nil {
		t.Fatalf("read session_new record: %v", err)
	}
	var req struct {
		Params struct {
			McpServers []struct {
				Name    string `json:"name"`
				Command string `json:"command"`
			} `json:"mcpServers"`
		} `json:"params"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("unmarshal session_new record: %v", err)
	}
	if len(req.Params.McpServers) != 1 || req.Params.McpServers[0].Name != "test-server" {
		t.Errorf("session/new McpServers = %+v, want test-server", req.Params.McpServers)
	}
}

func TestAntigravityExecuteSetModel(t *testing.T) {
	t.Parallel()

	binPath := makeFakeAntigravityExecutable(t, fakeAntigravityACPScript())
	backend := &antigravityBackend{
		cfg: Config{
			ExecutablePath: binPath,
			Logger:         quietAntigravityLogger(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Setting a valid model succeeds
	session, err := backend.Execute(ctx, "Test model", ExecOptions{Model: "gemini-3.8-flash-high"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	for range session.Messages {
	}
	res := <-session.Result
	if res.Status != "completed" {
		t.Fatalf("status = %q, want completed", res.Status)
	}

	// Setting an invalid model fails cleanly
	sessionBad, err := backend.Execute(ctx, "Test bogus model", ExecOptions{Model: "bogus-model"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	for range sessionBad.Messages {
	}
	resBad := <-sessionBad.Result
	if resBad.Status != "failed" || !strings.Contains(resBad.Error, "bogus-model") {
		t.Fatalf("status = %q, error = %q, want failure on bogus-model", resBad.Status, resBad.Error)
	}
}

func TestAntigravityExecuteResumeSuccess(t *testing.T) {
	t.Parallel()

	recordFile := filepath.Join(t.TempDir(), "session_resume.json")

	binPath := makeFakeAntigravityExecutable(t, fakeAntigravityACPScript())
	backend := &antigravityBackend{
		cfg: Config{
			ExecutablePath: binPath,
			Logger:         quietAntigravityLogger(),
			Env:            map[string]string{"ANTIGRAVITY_RECORD_SESSION_RESUME": recordFile},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "Resume task", ExecOptions{ResumeSessionID: "ses_orig_999"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	for range session.Messages {
	}
	res := <-session.Result
	if res.Status != "completed" {
		t.Fatalf("status = %q, want completed", res.Status)
	}

	data, err := os.ReadFile(recordFile)
	if err != nil {
		t.Fatalf("read session_resume record: %v", err)
	}
	var req struct {
		Params struct {
			SessionID string `json:"sessionId"`
		} `json:"params"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("unmarshal session_resume record: %v", err)
	}
	if req.Params.SessionID != "ses_orig_999" {
		t.Errorf("sessionId in session/resume = %q, want ses_orig_999", req.Params.SessionID)
	}
}

func TestAntigravityExecuteResumeRejected(t *testing.T) {
	t.Parallel()

	binPath := makeFakeAntigravityExecutable(t, fakeAntigravityACPScript())
	backend := &antigravityBackend{
		cfg: Config{
			ExecutablePath: binPath,
			Logger:         quietAntigravityLogger(),
			Env:            map[string]string{"ANTIGRAVITY_REJECT_RESUME": "1"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "Resume task", ExecOptions{ResumeSessionID: "stale_session_id"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	for range session.Messages {
	}
	res := <-session.Result
	if !res.ResumeRejected {
		t.Errorf("res.ResumeRejected = false, want true when session is rejected")
	}
	if res.Status != "failed" {
		t.Errorf("status = %q, want failed", res.Status)
	}
}
