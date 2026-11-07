package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const fakeMuseSessionID = "01a14870-5a2f-7791-b847-2385a5d47a8c"

// writeFakeMuseServe writes a stand-in for `muse serve` that answers MSP on
// stdio. It records every request line and its argv so a test can assert on the
// protocol conversation instead of on timing.
func writeFakeMuseServe(t *testing.T, resumeError bool) (bin, requests, args string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "muse")
	requests = filepath.Join(dir, "requests.jsonl")
	args = filepath.Join(dir, "args.txt")

	resumeBranch := `    *'"method":"session/resume"'*)
      printf '{"jsonrpc":"2.0","id":"%s","result":{"session":{"sessionId":"SESSION_ID"}}}\n' "$id"
      ;;`
	if resumeError {
		resumeBranch = `    *'"method":"session/resume"'*)
      printf '{"jsonrpc":"2.0","id":"%s","error":{"code":-32001,"message":"session not found","data":{"kind":"notFound"}}}\n' "$id"
      ;;`
	}

	script := `#!/bin/sh
printf '%s\n' "$*" > ARGS_PATH
while IFS= read -r line; do
  printf '%s\n' "$line" >> REQUESTS_PATH
  id=$(printf '%s' "$line" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":"%s","result":{"serverInfo":{"name":"muse","version":"1.4.10"},"schema":{"version":1}}}\n' "$id"
      ;;
RESUME_BRANCH
    *'"method":"session/start"'*)
      printf '{"jsonrpc":"2.0","id":"%s","result":{"session":{"sessionId":"SESSION_ID"},"viewCursor":"v1"}}\n' "$id"
      ;;
    *'"method":"session/setApprovalMode"'*)
      printf '{"jsonrpc":"2.0","id":"%s","result":{"sessionId":"SESSION_ID","mode":"allowAll"}}\n' "$id"
      ;;
    *'"method":"session/setModel"'*)
      printf '{"jsonrpc":"2.0","id":"%s","result":{"sessionId":"SESSION_ID","modelId":"new-model"}}\n' "$id"
      ;;
    *'"method":"turn/start"'*)
      printf '{"jsonrpc":"2.0","id":"%s","result":{"startedNewTurn":true,"disposition":"started"}}\n' "$id"
      sleep 0.02
      printf '{"jsonrpc":"2.0","method":"turn/started","params":{"sessionId":"SESSION_ID","turnId":"turn-1"}}\n'
      printf '{"jsonrpc":"2.0","method":"item/started","params":{"sessionId":"SESSION_ID","item":{"itemId":"tool-1","kind":"toolCall","status":"inProgress","tool":"shell","callId":"call-1","args":%s}}}\n' '{}'
      printf '{"jsonrpc":"2.0","method":"item/completed","params":{"sessionId":"SESSION_ID","item":{"itemId":"tool-1","kind":"toolCall","status":"completed","tool":"shell","callId":"call-1","visibleOutput":"a.txt"}}}\n'
      printf '{"jsonrpc":"2.0","method":"item/started","params":{"sessionId":"SESSION_ID","item":{"itemId":"msg-1","kind":"agentMessage","status":"inProgress"}}}\n'
      printf '{"jsonrpc":"2.0","method":"item/delta","params":{"sessionId":"SESSION_ID","itemId":"msg-1","field":"text","delta":"Muse "}}\n'
      printf '{"jsonrpc":"2.0","method":"item/delta","params":{"sessionId":"SESSION_ID","itemId":"msg-1","field":"text","delta":"finished"}}\n'
      printf '{"jsonrpc":"2.0","method":"item/completed","params":{"sessionId":"SESSION_ID","item":{"itemId":"msg-1","kind":"agentMessage","status":"completed","messagePhase":"finalAnswer","text":"Muse finished"}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/tokenUsage","params":{"sessionId":"SESSION_ID","modelId":"muse-wonder-full-1.0","usage":{"inputTokens":120,"outputTokens":8,"cacheReadTokens":20,"cacheWriteTokens":0}}}\n'
      printf '{"jsonrpc":"2.0","method":"turn/completed","params":{"sessionId":"SESSION_ID","turnId":"turn-1","terminal":"completed","durationMs":12}}\n'
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":"%s","error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`

	script = strings.ReplaceAll(script, "ARGS_PATH", args)
	script = strings.ReplaceAll(script, "REQUESTS_PATH", requests)
	script = strings.ReplaceAll(script, "RESUME_BRANCH", resumeBranch)
	script = strings.ReplaceAll(script, "SESSION_ID", fakeMuseSessionID)

	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake muse: %v", err)
	}
	return bin, requests, args
}

func readMuseRequests(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recorded requests: %v", err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("recorded request is not JSON: %v", err)
		}
		out = append(out, entry)
	}
	return out
}

func museRequestByMethod(requests []map[string]any, method string) map[string]any {
	for _, entry := range requests {
		if entry["method"] == method {
			return entry
		}
	}
	return nil
}

func newFakeMuseBackend(t *testing.T, bin string) Backend {
	t.Helper()
	backend, err := New("muse", Config{
		ExecutablePath: bin,
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	if err != nil {
		t.Fatalf("New(muse): %v", err)
	}
	return backend
}

func TestMuseExecuteStreamsTurn(t *testing.T) {
	t.Parallel()
	bin, requestsPath, argsPath := writeFakeMuseServe(t, false)
	backend := newFakeMuseBackend(t, bin)

	session, err := backend.Execute(context.Background(), "do the thing", ExecOptions{
		Cwd:   t.TempDir(),
		Model: "muse-wonder-full-1.0",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var text strings.Builder
	var toolUse, toolResult bool
	for msg := range session.Messages {
		switch msg.Type {
		case MessageText:
			text.WriteString(msg.Content)
		case MessageToolUse:
			toolUse = msg.Tool == "shell" && msg.CallID == "call-1"
		case MessageToolResult:
			toolResult = msg.Tool == "shell" && msg.CallID == "call-1" && msg.Output == "a.txt"
		}
	}

	var result Result
	for res := range session.Result {
		result = res
	}

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed (error %q)", result.Status, result.Error)
	}
	if result.Output != "Muse finished" {
		t.Fatalf("output = %q, want the final-answer text", result.Output)
	}
	if result.SessionID != fakeMuseSessionID {
		t.Fatalf("session id = %q", result.SessionID)
	}
	if got := text.String(); got != "Muse finished" {
		t.Fatalf("streamed text = %q, want the two deltas only", got)
	}
	if !toolUse {
		t.Fatal("tool-use message did not carry tool/callId")
	}
	if !toolResult {
		t.Fatal("tool-result message did not carry visibleOutput")
	}
	want := TokenUsage{InputTokens: 100, OutputTokens: 8, CacheReadTokens: 20}
	if got := result.Usage["muse-wonder-full-1.0"]; got != want {
		t.Fatalf("usage = %+v, want %+v (cache reads subtracted from input)", got, want)
	}

	argv, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	if !strings.Contains(string(argv), "serve") {
		t.Fatalf("argv = %q, want the serve subcommand", string(argv))
	}
	if strings.Contains(string(argv), "--yolo") {
		t.Fatalf("argv = %q, must not pass --yolo; approval is selected on the wire", string(argv))
	}

	requests := readMuseRequests(t, requestsPath)
	start := museRequestByMethod(requests, "session/start")
	if start == nil {
		t.Fatal("no session/start recorded")
	}
	params, _ := start["params"].(map[string]any)
	if params["approvalMode"] != "allowAll" {
		t.Fatalf("approvalMode = %v, want allowAll", params["approvalMode"])
	}
	if params["modelId"] != "muse-wonder-full-1.0" {
		t.Fatalf("modelId = %v, want the agent's model", params["modelId"])
	}
	if params["workspaceRoot"] == "" {
		t.Fatal("session/start carried no workspaceRoot")
	}
	commandID, _ := params["commandId"].(string)
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(commandID) {
		t.Fatalf("commandId %q is not a UUIDv7", commandID)
	}

	turn := museRequestByMethod(requests, "turn/start")
	if turn == nil {
		t.Fatal("no turn/start recorded")
	}
	turnParams, _ := turn["params"].(map[string]any)
	if turnParams["sessionId"] != fakeMuseSessionID {
		t.Fatalf("turn/start sessionId = %v", turnParams["sessionId"])
	}
	parts, _ := turnParams["input"].([]any)
	if len(parts) != 1 {
		t.Fatalf("turn/start input = %v, want one text part", turnParams["input"])
	}
}

func TestMuseExecuteReportsResumeRejection(t *testing.T) {
	t.Parallel()
	bin, _, _ := writeFakeMuseServe(t, true)
	backend := newFakeMuseBackend(t, bin)

	session, err := backend.Execute(context.Background(), "continue", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "01a14870-0000-7000-8000-000000000000",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	var result Result
	for res := range session.Result {
		result = res
	}
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if !result.ResumeRejected {
		t.Fatal("a notFound resume must be reported as ResumeRejected so the daemon retries fresh")
	}
}

func TestMuseUUIDv7IsTimeOrdered(t *testing.T) {
	t.Parallel()
	first := museUUIDv7()
	time.Sleep(2 * time.Millisecond)
	second := museUUIDv7()

	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !pattern.MatchString(first) || !pattern.MatchString(second) {
		t.Fatalf("ids %q / %q are not UUIDv7", first, second)
	}
	if first >= second {
		t.Fatalf("UUIDv7 ids are not time-ordered: %q then %q", first, second)
	}
}

func TestMuseToolInputDecodesArguments(t *testing.T) {
	t.Parallel()
	decoded := museToolInput([]byte(`{"cmd":"ls","cwd":"/w"}`))
	if decoded["cmd"] != "ls" || decoded["cwd"] != "/w" {
		t.Fatalf("decoded = %v, want the model's argument object", decoded)
	}
	// Muse carries args verbatim as a string. A non-object payload must still
	// be visible on the board rather than silently dropped.
	fallback := museToolInput([]byte(`"ls -la"`))
	if fallback["arguments"] != "ls -la" {
		t.Fatalf("fallback = %v, want the raw payload under arguments", fallback)
	}
	if museToolInput(nil) != nil {
		t.Fatal("empty args must decode to nil, not an empty map")
	}
}

func TestMuseModelSelectionIsRuntimeApplied(t *testing.T) {
	t.Parallel()
	if !ModelSelectionSupported("muse") {
		t.Fatal("muse applies agent.model through session/start modelId")
	}
	if version := MinVersions["muse"]; version == "" {
		t.Fatal("muse needs a minimum CLI version")
	}
	if LaunchHeader("muse") == "" {
		t.Fatal("muse needs a launch header for the custom-args preview")
	}
}
