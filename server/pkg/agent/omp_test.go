package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"log/slog"
)

func TestNewReturnsOmpBackend(t *testing.T) {
	t.Parallel()
	b, err := New("omp", Config{ExecutablePath: "/nonexistent/omp"})
	if err != nil {
		t.Fatalf("New(omp) error: %v", err)
	}
	if _, ok := b.(*ompBackend); !ok {
		t.Fatalf("expected *ompBackend, got %T", b)
	}
}

// fakeOmpACPScript impersonates `omp acp` for unit tests. OMP speaks the
// standard ACP wire format, so the fake mirrors the traecli/kimi shape:
// initialize advertises loadSession + mcpCapabilities, session/new returns a
// sessionId + model catalog, session/load resumes, session/prompt streams
// session/update frames (sessionUpdate discriminator) and ends with end_turn.
func fakeOmpACPScript() string {
	return `#!/bin/sh
if [ -n "$OMP_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$OMP_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  if [ -n "$OMP_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$OMP_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_new","configOptions":[{"id":"model","category":"model","currentValue":"zai/glm-5.2","options":[{"value":"zai/glm-5.2","name":"GLM-5.2","description":""},{"value":"zai/glm-4.5","name":"GLM-4.5","description":""}]}]}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_loaded","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"history replay ignored"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/set_config_option"'*)
      case "$line" in
        *bogus-model*)
          printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Internal error","data":{"details":"Unknown ACP model: bogus-model"}}}\n' "$id"
          exit 0
          ;;
        *)
          printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[]}}\n' "$id"
          ;;
      esac
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"thinking about it"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"tool_call","toolCallId":"tc-1","name":"Shell","status":"pending","parameters":{"command":"echo hi"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-1","status":"completed","name":"Shell","output":"hi\\n"}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"pong"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestOmpBackendStreamsAndCompletes(t *testing.T) {
	t.Parallel()
	fakePath := filepath.Join(t.TempDir(), "omp")
	writeTestExecutable(t, fakePath, []byte(fakeOmpACPScript()))

	backend, err := New("omp", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new omp backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "say pong", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var messages []Message
	done := make(chan struct{})
	go func() {
		defer close(done)
		for m := range session.Messages {
			messages = append(messages, m)
		}
	}()
	result := <-session.Result
	<-done

	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "pong") {
		t.Fatalf("output = %q, want it to contain the assistant message 'pong'", result.Output)
	}
	if result.SessionID != "ses_new" {
		t.Fatalf("session id = %q, want ses_new", result.SessionID)
	}
	// The agent_message_chunk must surface as MessageText; the tool_call must
	// surface as a MessageToolUse normalized to a canonical tool name.
	var sawText, sawToolUse bool
	for _, m := range messages {
		if m.Type == MessageText && strings.Contains(m.Content, "pong") {
			sawText = true
		}
		if m.Type == MessageToolUse && m.Tool == "terminal" {
			sawToolUse = true
		}
	}
	if !sawText {
		t.Error("expected a MessageText carrying the assistant 'pong'")
	}
	if !sawToolUse {
		t.Errorf("expected the Shell tool_call to normalize to 'terminal'; messages=%+v", messages)
	}
}

func TestOmpBlockedArgsFiltering(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "omp")
	writeTestExecutable(t, fakePath, []byte(fakeOmpACPScript()))

	backend, err := New("omp", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"OMP_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new omp backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "task", ExecOptions{
		Timeout: 5 * time.Second,
		// Users must not be able to strip ACP mode, override the daemon-owned
		// yolo/approval flags, switch to print/rpc mode, or duplicate the
		// subcommand.
		CustomArgs: []string{"acp", "--yolo", "--auto-approve", "-p", "--mode", "rpc", "--output-format", "json", "--approval-mode", "yolo", "--add-dir", "/tmp/extra"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	wantPrefix := []string{"acp", "--yolo"}
	if len(lines) < len(wantPrefix) {
		t.Fatalf("expected at least %d args, got %d: %q", len(wantPrefix), len(lines), lines)
	}
	for i, want := range wantPrefix {
		if lines[i] != want {
			t.Fatalf("arg[%d] = %q, want %q (full: %q)", i, lines[i], want, lines)
		}
	}
	// The hardcoded prefix must appear exactly once each.
	joined := strings.Join(lines, " ")
	for _, once := range []string{"acp", "--yolo"} {
		if c := countTokens(lines, once); c != 1 {
			t.Errorf("expected exactly one %q, got %d (full: %q)", once, c, joined)
		}
	}
	for _, blocked := range []string{"--auto-approve", "-p", "--mode", "rpc", "--output-format", "json", "--approval-mode", "yolo"} {
		for _, got := range lines {
			if got == blocked {
				t.Errorf("blocked custom arg %q survived filtering: %q", blocked, lines)
			}
		}
	}
	// An allowed custom arg must survive.
	if !strings.Contains(joined, "--add-dir /tmp/extra") {
		t.Errorf("expected allowed custom arg --add-dir to survive, got %q", joined)
	}
}

func TestOmpSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()
	fakePath := filepath.Join(t.TempDir(), "omp")
	writeTestExecutable(t, fakePath, []byte(fakeOmpACPScript()))

	backend, err := New("omp", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new omp backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "task", ExecOptions{Model: "bogus-model", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected failed on set_config_option error, got %q", result.Status)
	}
	if !strings.Contains(result.Error, `could not switch to model "bogus-model"`) {
		t.Errorf("expected error to name the model, got %q", result.Error)
	}
	if !strings.Contains(result.Error, "Unknown ACP model") {
		t.Errorf("expected upstream message surfaced, got %q", result.Error)
	}
}

func TestOmpUsesSessionLoadForResume(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	requestsFile := filepath.Join(tempDir, "requests.jsonl")
	fakePath := filepath.Join(tempDir, "omp")
	writeTestExecutable(t, fakePath, []byte(fakeOmpACPScript()))

	backend, err := New("omp", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"OMP_REQUESTS_FILE": requestsFile},
	})
	if err != nil {
		t.Fatalf("new omp backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "continue", ExecOptions{
		ResumeSessionID: "ses_existing",
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed, got %q (error=%q)", result.Status, result.Error)
	}
	if result.SessionID != "ses_existing" {
		t.Fatalf("session id = %q, want ses_existing", result.SessionID)
	}
	raw, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests: %v", err)
	}
	requests := string(raw)
	if !strings.Contains(requests, `"method":"session/load"`) {
		t.Fatalf("expected session/load on resume, got:\n%s", requests)
	}
	if strings.Contains(requests, `"method":"session/resume"`) {
		t.Fatalf("omp must use session/load (loadSession:true), not session/resume:\n%s", requests)
	}
}

// TestOmpSetConfigOptionSwitchesModel pins that a model override goes through
// session/set_config_option (omp rejects session/set_model as an unknown
// method) and the task still completes.
func TestOmpSetConfigOptionSwitchesModel(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	requestsFile := filepath.Join(tempDir, "requests.jsonl")
	fakePath := filepath.Join(tempDir, "omp")
	writeTestExecutable(t, fakePath, []byte(fakeOmpACPScript()))

	backend, err := New("omp", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"OMP_REQUESTS_FILE": requestsFile},
	})
	if err != nil {
		t.Fatalf("new omp backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A valid model id from the fake's configOptions catalog.
	session, err := backend.Execute(ctx, "task", ExecOptions{Model: "zai/glm-4.5", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed with a valid model, got %q (error=%q)", result.Status, result.Error)
	}
	raw, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests: %v", err)
	}
	requests := string(raw)
	if !strings.Contains(requests, `"method":"session/set_config_option"`) {
		t.Fatalf("expected session/set_config_option on model override, got:\n%s", requests)
	}
	if !strings.Contains(requests, `"configId":"model"`) || !strings.Contains(requests, `"value":"zai/glm-4.5"`) {
		t.Fatalf("expected configId=model value=zai/glm-4.5, got:\n%s", requests)
	}
	if strings.Contains(requests, `"method":"session/set_model"`) {
		t.Fatalf("omp must not use session/set_model (rejected by omp), got:\n%s", requests)
	}
}

func TestParseACPModelConfigOptions(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"sessionId":"s","configOptions":[` +
		`{"id":"mode","category":"mode","currentValue":"default","options":[{"value":"default","name":"Default"}]},` +
		`{"id":"model","category":"model","currentValue":"zai/glm-5.2","options":[` +
		`{"value":"zai/glm-5.2","name":"GLM-5.2"},` +
		`{"value":"zai/glm-4.5","name":"GLM-4.5"}]},` +
		`{"id":"thinking","category":"thought_level","currentValue":"high","options":[]}]}`)
	models := parseACPModelConfigOptions(raw)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(models), models)
	}
	byID := map[string]Model{}
	for _, m := range models {
		byID[m.ID] = m
	}
	glm52, glm45 := byID["zai/glm-5.2"], byID["zai/glm-4.5"]
	if glm52.ID == "" || !glm52.Default {
		t.Errorf("zai/glm-5.2 should be present and Default: %+v", glm52)
	}
	if glm45.ID == "" || glm45.Default {
		t.Errorf("zai/glm-4.5 should be present and not Default: %+v", glm45)
	}
	if glm52.Provider != "zai" {
		t.Errorf("provider should split on '/', got %q", glm52.Provider)
	}
	if glm52.Label != "GLM-5.2" {
		t.Errorf("label should be the option name, got %q", glm52.Label)
	}

	// No model config option → nil (caller falls back to manual entry).
	if got := parseACPModelConfigOptions(json.RawMessage(`{"sessionId":"s","configOptions":[{"id":"mode","category":"mode"}]}`)); got != nil {
		t.Errorf("expected nil when no model option, got %+v", got)
	}
	// omp's real session/new has no `models` block; this parser must not read
	// one (that's the other ACP agents' shape, handled by parseACPSessionNewModels).
	if got := parseACPModelConfigOptions(json.RawMessage(`{"models":{"availableModels":[{"modelId":"x"}]}}`)); got != nil {
		t.Errorf("expected nil for models-block-only payload, got %+v", got)
	}
}
