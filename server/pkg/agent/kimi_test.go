package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsKimiBackend(t *testing.T) {
	t.Parallel()
	b, err := New("kimi", Config{ExecutablePath: "/nonexistent/kimi"})
	if err != nil {
		t.Fatalf("New(kimi) error: %v", err)
	}
	if _, ok := b.(*kimiBackend); !ok {
		t.Fatalf("expected *kimiBackend, got %T", b)
	}
}

func TestKimiToolNameFromTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		title string
		want  string
	}{
		{"Read file: /tmp/foo.go", "read_file"},
		{"read", "read_file"},
		{"Write: /tmp/bar.go", "write_file"},
		{"Edit", "edit_file"},
		{"Patch: /tmp/x", "edit_file"},
		{"Shell: ls -la", "terminal"},
		{"Bash", "terminal"},
		{"Run command: pwd", "terminal"},
		{"Search: foo", "search_files"},
		{"Glob: *.go", "glob"},
		{"Web search: golang acp", "web_search"},
		{"Fetch: https://example.com", "web_fetch"},
		{"Todo Write", "todo_write"},
		// Fallback: snake_case the title.
		{"Custom Thing", "custom_thing"},
		// Empty input returns empty — caller decides how to react.
		{"", ""},
	}
	for _, tt := range tests {
		got := kimiToolNameFromTitle(tt.title)
		if got != tt.want {
			t.Errorf("kimiToolNameFromTitle(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

// fakeKimiACPScript returns a POSIX-sh script that impersonates
// `kimi acp` for a single short ACP session: it acks initialize /
// session/new and then replies to session/set_model with a JSON-RPC
// error — the scenario the kimiBackend must propagate as a failed
// task rather than silently falling back to the default model.
func fakeKimiACPScript() string {
	return `#!/bin/sh
# Fake ` + "`kimi`" + ` binary — used by TestKimiBackendSetModelFailureFailsTask
# and TestKimiBackendPassesYoloFlag.
#
# Writes the full argv (one arg per line) to $KIMI_ARGS_FILE if that env
# var is set, so tests can assert that the daemon invokes us with the
# right flags (` + "`--yolo acp`" + `, not bare ` + "`acp`" + `).
#
# Then reads one JSON-RPC request per line from stdin, matches on the
# method name, and writes back a canned response. Exits after set_model
# so the kimiBackend cleanup path can run.
if [ -n "$KIMI_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$KIMI_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake"}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"model not available: bogus-model"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// TestKimiBackendSetModelFailureFailsTask pins the "don't silently
// fall back" behaviour that landed in this PR: when kimi rejects the
// caller-selected model via session/set_model, the task result must
// report status=failed with a message that names the model and the
// upstream error — not claim success while actually running on the
// default model.
func TestKimiBackendSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPScript()))

	backend, err := New("kimi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kimi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:   "bogus-model",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Drain message stream so the lifecycle goroutine can progress.
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, `could not switch to model "bogus-model"`) {
			t.Errorf("expected error to name the requested model, got %q", result.Error)
		}
		if !strings.Contains(result.Error, "model not available") {
			t.Errorf("expected error to surface upstream message, got %q", result.Error)
		}
		if result.SessionID != "ses_fake" {
			t.Errorf("expected session id to be preserved on failure, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// fakeKimiACPStaleResumeSetModelScript impersonates kimi-cli when a
// resumed session is gone and the caller picked a model:
// session/resume echoes the requested sessionId back, then
// session/set_model rejects the unknown session the way kimi-cli
// actually does — RequestError.invalid_params (-32602) with
// {"session_id": "Session not found"} in data
// (src/kimi_cli/acp/server.py, set_session_model).
func fakeKimiACPStaleResumeSetModelScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      sid=$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"%s"}}\n' "$id" "$sid"
      ;;
    *'"method":"session/set_model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"Invalid params","data":{"session_id":"Session not found"}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// TestKimiBackendClearsSessionIDWhenSetModelSessionNotFound pins the
// set_model sibling of the resumed-session fix: with a model override,
// session/set_model runs before session/prompt, so a dead resumed
// session surfaces there. The Result must carry an empty SessionID so
// the daemon's fresh-session retry (gated on SessionID == "") fires.
func TestKimiBackendClearsSessionIDWhenSetModelSessionNotFound(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPStaleResumeSetModelScript()))

	backend, err := New("kimi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kimi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: "ses_stale",
		Model:           "kimi-for-coding",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, `could not switch to model "kimi-for-coding"`) {
			t.Errorf("expected error to name the requested model, got %q", result.Error)
		}
		if result.SessionID != "" {
			t.Errorf("expected empty session id so the daemon's fresh-session retry fires, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// TestKimiBackendInvokesACPSubcommand pins the argv for `kimi`. An
// earlier fix tried passing `--yolo` to bypass per-tool approval
// prompts, but the `acp` subcommand in kimi-cli takes no options
// (see cli/__init__.py @cli.command def acp()), so `--yolo` was a
// no-op and the daemon still hung for 5 min on the first Shell call.
// The actual bypass is in hermesClient.handleAgentRequest, which
// auto-approves session/request_permission. This test catches
// accidental re-introduction of the dead flag.
func TestKimiBackendInvokesACPSubcommand(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPScript()))

	backend, err := New("kimi", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"KIMI_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new kimi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Set Model so the fake binary exits on set_model and we don't
	// have to wait for the prompt branch. We only care about argv here.
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:   "bogus-model",
		Timeout: 5 * time.Second,
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
	if len(lines) < 1 {
		t.Fatalf("expected at least 1 arg (acp), got %d: %q", len(lines), lines)
	}
	if lines[0] != "acp" {
		t.Errorf("expected first arg to be acp, got %q (full: %q)", lines[0], lines)
	}
	for _, l := range lines {
		switch l {
		case "--yolo", "--auto-approve", "--yes", "-y":
			t.Errorf("kimi acp doesn't accept %q; auto-approval is handled in hermesClient.handleAgentRequest", l)
		}
	}
}

// TestKimiResumeIncludesMcpServers pins the same contract as the matching
// Hermes test: session/resume must carry the managed MCP set so a resumed
// Kimi task has the same MCP tools as a fresh one.
func TestKimiResumeIncludesMcpServers(t *testing.T) {
	t.Parallel()

	recordPath := filepath.Join(t.TempDir(), "frames.jsonl")
	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeACPRecordingScript(recordPath, "ses_resume", `{}`)))

	backend, err := New("kimi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kimi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: "ses_resume",
		McpConfig:       json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx"}}}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case <-session.Result:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	frame := findRecordedFrame(t, recordPath, "session/resume")
	params := frame["params"].(map[string]any)
	servers, ok := params["mcpServers"].([]any)
	if !ok {
		t.Fatalf("session/resume.mcpServers: got %T, want []any", params["mcpServers"])
	}
	if len(servers) != 1 || servers[0].(map[string]any)["name"] != "fetch" {
		t.Fatalf("session/resume.mcpServers: got %v, want one entry named fetch", servers)
	}
}

// kimiWireRec builds one wire.jsonl usage.record line for tests.
func kimiWireRec(model string, in, out, cacheR, cacheW int64, at time.Time) string {
	return fmt.Sprintf(`{"type":"usage.record","model":%q,"usage":{"inputOther":%d,"output":%d,"inputCacheRead":%d,"inputCacheCreation":%d},"usageScope":"turn","time":%d}`,
		model, in, out, cacheR, cacheW, at.UnixMilli())
}

func TestReadKimiWireUsageSumsRecentRecordsPerModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)

	now := time.Now()
	wire := strings.Join([]string{
		`{"type":"llm.request","kind":"loop","model":"k3"}`,
		"not json at all",
		kimiWireRec("kimi-code/k3", 100, 10, 5, 1, now.Add(-2*time.Second)),           // in window
		kimiWireRec("kimi-code/k3", 50, 5, 2, 0, now.Add(-3*time.Second)),             // in window, same model
		kimiWireRec("kimi-code/kimi-for-coding", 7, 3, 0, 0, now.Add(-4*time.Second)), // in window, second model
		kimiWireRec("kimi-code/k3", 9999, 9999, 0, 0, now.Add(-time.Hour)),            // previous task on a resumed session
		kimiWireRec("", 4, 2, 0, 0, now),                                              // no model — skipped
	}, "\n") + "\n"

	dir := filepath.Join(home, "sessions", "wd_x_deadbeef", "session-abc", "agents", "main")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wire.jsonl"), []byte(wire), 0o644); err != nil {
		t.Fatal(err)
	}

	got, foundWire := readKimiWireUsage("session-abc", now.Add(-10*time.Second), slog.Default())
	if !foundWire {
		t.Fatal("expected foundWire=true with a wire file present")
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 models, got %+v", got)
	}
	u := got["kimi-code/k3"]
	if u.InputTokens != 150 || u.OutputTokens != 15 || u.CacheReadTokens != 7 || u.CacheWriteTokens != 1 {
		t.Errorf("unexpected k3 totals: %+v", u)
	}
	if u := got["kimi-code/kimi-for-coding"]; u.InputTokens != 7 || u.OutputTokens != 3 {
		t.Errorf("unexpected coding totals: %+v", u)
	}
}

func TestReadKimiWireUsageMissingSessionReturnsNil(t *testing.T) {
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	got, foundWire := readKimiWireUsage("session-nope", time.Now().Add(-time.Minute), slog.Default())
	if got != nil {
		t.Fatalf("expected nil for missing wire, got %+v", got)
	}
	if foundWire {
		t.Fatal("expected foundWire=false when no wire file matches")
	}
}

func TestReadKimiWireUsageSumsAcrossAgentWires(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	now := time.Now()
	line := kimiWireRec("kimi-code/k3", 10, 1, 0, 0, now) + "\n"
	for _, agentName := range []string{"main", "task-critic"} {
		dir := filepath.Join(home, "sessions", "wd_x_deadbeef", "session-abc", "agents", agentName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "wire.jsonl"), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, foundWire := readKimiWireUsage("session-abc", now.Add(-10*time.Second), slog.Default())
	if !foundWire {
		t.Fatal("expected foundWire=true with wire files present")
	}
	if u := got["kimi-code/k3"]; u.InputTokens != 20 || u.OutputTokens != 2 {
		t.Fatalf("expected both agent wires summed, got %+v", got)
	}
}

// TestWaitKimiWireUsageSettlesAcrossAgentWires pins the flush-race fix:
// the main agent's usage.record commonly lands a poll tick before a
// sub-agent's. waitKimiWireUsage must not return on the first non-empty
// read — it waits for the totals to hold steady across one more interval,
// so the late second wire is counted too.
func TestWaitKimiWireUsageSettlesAcrossAgentWires(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)

	now := time.Now()
	mainDir := filepath.Join(home, "sessions", "wd_x_deadbeef", "session-abc", "agents", "main")
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The wire exists from session start but holds no usage record yet —
	// kimi flushes it only after answering session/prompt.
	mainWire := filepath.Join(mainDir, "wire.jsonl")
	if err := os.WriteFile(mainWire, []byte(`{"type":"llm.request","kind":"loop","model":"k3"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resultCh := make(chan map[string]TokenUsage, 1)
	go func() {
		resultCh <- waitKimiWireUsage("session-abc", now, slog.Default())
	}()

	// Main agent flushes first…
	time.Sleep(kimiWireUsagePollInterval / 2)
	f, err := os.OpenFile(mainWire, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(kimiWireRec("kimi-code/k3", 100, 10, 0, 0, now) + "\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	// …and the sub-agent's wire appears one tick later. The read that
	// settles the totals must happen after this write.
	time.Sleep(kimiWireUsagePollInterval)
	criticDir := filepath.Join(home, "sessions", "wd_x_deadbeef", "session-abc", "agents", "task-critic")
	if err := os.MkdirAll(criticDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(criticDir, "wire.jsonl"), []byte(kimiWireRec("kimi-code/k3", 7, 3, 0, 0, now)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-resultCh:
		u := got["kimi-code/k3"]
		if u.InputTokens != 107 || u.OutputTokens != 13 {
			t.Fatalf("expected both agent wires summed (107 in / 13 out), got %+v", got)
		}
	case <-time.After(kimiWireUsagePollTimeout + 5*time.Second):
		t.Fatal("waitKimiWireUsage did not return")
	}
}

// TestWaitKimiWireUsageNoWireReturnsImmediately pins the no-wire fast
// path: kimi creates the session wire at session start, long before the
// post-prompt poll, so a first probe matching nothing means this build
// writes none (older CLI, custom data dir). The wait must bail at once,
// not burn the whole poll budget on every completed run.
func TestWaitKimiWireUsageNoWireReturnsImmediately(t *testing.T) {
	t.Setenv("KIMI_CODE_HOME", t.TempDir())

	start := time.Now()
	if got := waitKimiWireUsage("session-nope", start.Add(-time.Minute), slog.Default()); got != nil {
		t.Fatalf("expected nil usage, got %+v", got)
	}
	if elapsed := time.Since(start); elapsed > kimiWireUsagePollTimeout/2 {
		t.Fatalf("expected immediate return on missing wire, took %s", elapsed)
	}
}

// TestAccumulateKimiWireFileReadsPastOversizedLines pins the unbounded
// line reader: a huge tool-output frame must not stop the scan and hide
// usage.record entries appended after it — the old 4 MiB bufio.Scanner
// cap aborted the read at exactly that line.
func TestAccumulateKimiWireFileReadsPastOversizedLines(t *testing.T) {
	t.Parallel()

	now := time.Now()
	// 5 MiB of tool output — over the old 4 MiB scanner cap.
	big := strings.Repeat("x", 5*1024*1024)
	wire := strings.Join([]string{
		kimiWireRec("kimi-code/k3", 100, 10, 0, 0, now),
		`{"type":"tool.output","data":"` + big + `"}`,
		kimiWireRec("kimi-code/k3", 50, 5, 0, 0, now),
	}, "\n") + "\n"

	path := filepath.Join(t.TempDir(), "wire.jsonl")
	if err := os.WriteFile(path, []byte(wire), 0o644); err != nil {
		t.Fatal(err)
	}

	totals := map[string]TokenUsage{}
	if err := accumulateKimiWireFile(path, now.Add(-time.Minute).UnixMilli(), totals); err != nil {
		t.Fatalf("accumulateKimiWireFile: %v", err)
	}
	if u := totals["kimi-code/k3"]; u.InputTokens != 150 || u.OutputTokens != 15 {
		t.Fatalf("expected records on both sides of the oversized line summed, got %+v", totals)
	}
}

// fakeKimiACPPromptOutcomeScript impersonates `kimi acp` for a session
// whose prompt ends with the given canned JSON-RPC fragment — e.g.
// `"error":{"code":-32603,...}` for a failed turn or
// `"result":{"stopReason":"cancelled"}` for a cancelled one.
func fakeKimiACPPromptOutcomeScript(outcome string) string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_wire"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,` + outcome + `}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// TestKimiBackendRecoversWireUsageOnNonCompletedRun pins the failure-path
// usage recovery: a run that consumed tokens but ended failed or
// cancelled must still report the partial turn's usage, recovered with a
// single non-polling wire read at usage-build time — no settle wait on
// these paths. The daemon bills usage even for cancelled tasks (see the
// ReportTaskUsage comment in server/internal/daemon/daemon.go).
func TestKimiBackendRecoversWireUsageOnNonCompletedRun(t *testing.T) {
	tests := []struct {
		name       string
		outcome    string
		wantStatus string
	}{
		{"failed prompt", `"error":{"code":-32603,"message":"upstream boom"}`, "failed"},
		{"cancelled turn", `"result":{"stopReason":"cancelled"}`, "aborted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			home := t.TempDir()
			t.Setenv("KIMI_CODE_HOME", home)

			// Pre-seed the session wire the way kimi leaves it after a
			// turn that consumed tokens but did not complete.
			wireDir := filepath.Join(home, "sessions", "wd_x_deadbeef", "ses_wire", "agents", "main")
			if err := os.MkdirAll(wireDir, 0o755); err != nil {
				t.Fatal(err)
			}
			wire := kimiWireRec("kimi-code/k3", 120, 30, 8, 2, time.Now()) + "\n"
			if err := os.WriteFile(filepath.Join(wireDir, "wire.jsonl"), []byte(wire), 0o644); err != nil {
				t.Fatal(err)
			}

			fakePath := filepath.Join(t.TempDir(), "kimi")
			writeTestExecutable(t, fakePath, []byte(fakeKimiACPPromptOutcomeScript(tt.outcome)))

			backend, err := New("kimi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
			if err != nil {
				t.Fatalf("new kimi backend: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			go func() {
				for range session.Messages {
				}
			}()

			select {
			case result, ok := <-session.Result:
				if !ok {
					t.Fatal("result channel closed without a value")
				}
				if result.Status != tt.wantStatus {
					t.Fatalf("expected status=%q, got %q (error=%q)", tt.wantStatus, result.Status, result.Error)
				}
				u := result.Usage["kimi-code/k3"]
				if u.InputTokens != 120 || u.OutputTokens != 30 || u.CacheReadTokens != 8 || u.CacheWriteTokens != 2 {
					t.Fatalf("expected wire usage on the %s path, got %+v", tt.wantStatus, result.Usage)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("timeout waiting for result")
			}
		})
	}
}

// fakeKimiACPSwallowedFailureScript impersonates `kimi acp` for the
// swallowed-failure scenario (M-95): the turn dies provider-side but the
// ACP server still answers session/prompt with stopReason=end_turn and
// streams no output. Handles both session/new and session/resume so tests
// can cover fresh and poisoned-resume runs.
func fakeKimiACPSwallowedFailureScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_swallowed"}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      sid=$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"%s"}}\n' "$id" "$sid"
      ;;
    *'"method":"session/prompt"'*)
      # The swallowed failure: end_turn, no text, no error — the real
      # failure only exists in the session's logs/kimi-code.log.
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// kimiTurnFailedLogLines builds the kimi-code.log line pair kimi-cli
// writes when a turn dies provider-side while ACP reports end_turn —
// captured verbatim from a real poisoned session (see M-95):
//
//	2026-07-19T15:03:24.931Z ERROR turn failed  turnId=1
//	2026-07-19T15:03:24.933Z WARN  acp: turn ended with failed reason  error="{...}"
func kimiTurnFailedLogLines(at time.Time, turnID int, message string) string {
	ts := at.UTC().Format("2006-01-02T15:04:05.000Z07:00")
	payload := fmt.Sprintf(`{"code":"provider.api_error","message":%s,"name":"APIStatusError","details":{"statusCode":400,"requestId":null,"turnId":%d},"retryable":false}`,
		strconv.Quote(message), turnID)
	return fmt.Sprintf("%s ERROR turn failed  turnId=%d\n%s WARN  acp: turn ended with failed reason  error=%s\n",
		ts, turnID, ts, strconv.Quote(payload))
}

// seedKimiSessionFiles writes the session wire and/or session log under a
// fake KIMI_CODE_HOME with the same on-disk layout kimi-cli uses:
// sessions/<cwd-hash>/<sessionID>/{agents/main/wire.jsonl,logs/kimi-code.log}.
// Empty content skips that file.
func seedKimiSessionFiles(t *testing.T, home, sessionID, wire, log string) {
	t.Helper()
	if wire != "" {
		dir := filepath.Join(home, "sessions", "wd_x_deadbeef", sessionID, "agents", "main")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "wire.jsonl"), []byte(wire), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if log != "" {
		dir := filepath.Join(home, "sessions", "wd_x_deadbeef", sessionID, "logs")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "kimi-code.log"), []byte(log), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// runFakeKimi executes the backend against a fake kimi binary and drains
// the session, returning the final Result.
func runFakeKimi(t *testing.T, fakePath string, opts ExecOptions) Result {
	t.Helper()
	backend, err := New("kimi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kimi backend: %v", err)
	}
	if opts.Timeout == 0 {
		opts.Timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", opts)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		return result
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for result")
		return Result{}
	}
}

// TestKimiBackendSwallowedTurnFailureFailsRun is the M-95 core regression
// test: kimi answers session/prompt with stopReason=end_turn even though
// the turn died provider-side, streaming no output and flushing no
// usage.record. The backend must not report a silent success — it must
// cross-check the session's logs/kimi-code.log, fail the run with the
// real provider error, and drop the session id so the next run starts
// fresh instead of resuming the poisoned context.
func TestKimiBackendSwallowedTurnFailureFailsRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)

	providerMsg := "400 the message at position 168 with role 'assistant' must not be empty"
	// The wire exists (kimi creates it at session start) but the failed
	// turn flushed no usage.record; the log holds the in-window failure.
	seedKimiSessionFiles(t, home, "ses_swallowed",
		`{"type":"llm.request","kind":"loop","model":"k3"}`+"\n",
		kimiTurnFailedLogLines(time.Now(), 1, providerMsg))

	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPSwallowedFailureScript()))

	result := runFakeKimi(t, fakePath, ExecOptions{})

	if result.Status != "failed" {
		t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "provider.api_error") {
		t.Errorf("expected error to name the provider error code, got %q", result.Error)
	}
	if !strings.Contains(result.Error, providerMsg) {
		t.Errorf("expected error to surface the provider message, got %q", result.Error)
	}
	if result.SessionID != "" {
		t.Errorf("expected session id to be dropped so the next run starts fresh, got %q", result.SessionID)
	}
}

// TestKimiBackendSwallowedTurnFailureDropsPoisonedSession covers the
// poisoned-session loop from M-95: a resumed session whose context is
// corrupt fails every turn the same silent way. When the swallowed
// failure is detected on a resume, the Result must carry an empty
// SessionID so the daemon's resume-failure fallback retries with a fresh
// session instead of resuming the brick forever.
func TestKimiBackendSwallowedTurnFailureDropsPoisonedSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)

	providerMsg := "400 the message at position 168 with role 'assistant' must not be empty"
	seedKimiSessionFiles(t, home, "ses_poisoned",
		`{"type":"llm.request","kind":"loop","model":"k3"}`+"\n",
		kimiTurnFailedLogLines(time.Now(), 7, providerMsg))

	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPSwallowedFailureScript()))

	result := runFakeKimi(t, fakePath, ExecOptions{ResumeSessionID: "ses_poisoned"})

	if result.Status != "failed" {
		t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
	}
	if result.SessionID != "" {
		t.Errorf("expected empty session id so the daemon's fresh-session retry fires, got %q", result.SessionID)
	}
}

// TestKimiBackendHealthyEmptyCompletionNotFlagged guards the false-positive
// side: a legit answer with no text (the turn really ran and flushed its
// usage.record) is a valid empty completion and must NOT be failed by the
// kimi-code.log cross-check — even when the log still holds failure
// entries from a previous run on the same session.
func TestKimiBackendHealthyEmptyCompletionNotFlagged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)

	now := time.Now()
	// In-window usage.record for the turn; only stale (previous run)
	// failure entries in the log.
	seedKimiSessionFiles(t, home, "ses_swallowed",
		kimiWireRec("kimi-code/k3", 100, 10, 0, 0, now)+"\n",
		kimiTurnFailedLogLines(now.Add(-time.Hour), 1, "400 the message at position 168 with role 'assistant' must not be empty"))

	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPSwallowedFailureScript()))

	result := runFakeKimi(t, fakePath, ExecOptions{})

	if result.Status != "completed" {
		t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
	}
	if result.SessionID != "ses_swallowed" {
		t.Errorf("expected session id preserved on a healthy run, got %q", result.SessionID)
	}
	if u := result.Usage["kimi-code/k3"]; u.InputTokens != 100 || u.OutputTokens != 10 {
		t.Errorf("expected wire usage on the healthy run, got %+v", result.Usage)
	}
}

// TestReadKimiTurnFailure pins the kimi-code.log cross-check itself:
// in-window failures are found with the provider error extracted, stale
// entries from previous runs are ignored, and a missing log means "no
// signal" (older kimi builds), never a false positive.
func TestReadKimiTurnFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)

	now := time.Now()
	providerMsg := "400 the message at position 168 with role 'assistant' must not be empty"

	tests := []struct {
		name        string
		log         string
		wantFound   bool
		wantMsgPart string
	}{
		{
			name:        "in-window failure surfaces the provider error",
			log:         kimiTurnFailedLogLines(now, 3, providerMsg),
			wantFound:   true,
			wantMsgPart: providerMsg,
		},
		{
			name:        "stale failure from a previous run is ignored",
			log:         kimiTurnFailedLogLines(now.Add(-time.Hour), 3, providerMsg),
			wantFound:   false,
			wantMsgPart: "",
		},
		{
			name: "mixed stale and in-window failures finds the new one",
			log: kimiTurnFailedLogLines(now.Add(-time.Hour), 1, "stale boom") +
				kimiTurnFailedLogLines(now, 2, providerMsg),
			wantFound:   true,
			wantMsgPart: providerMsg,
		},
		{
			name:        "bare turn failed without detail line still flags",
			log:         fmt.Sprintf("%s ERROR turn failed  turnId=4\n", now.UTC().Format("2006-01-02T15:04:05.000Z07:00")),
			wantFound:   true,
			wantMsgPart: "turn failed",
		},
		{
			name:        "healthy log with no failure entries",
			log:         fmt.Sprintf("%s INFO  llm response  turnStep=0/1 outputTokens=42\n", now.UTC().Format("2006-01-02T15:04:05.000Z07:00")),
			wantFound:   false,
			wantMsgPart: "",
		},
		{
			name: "malformed lines are skipped",
			log: "not a log line at all\n" +
				"ERROR turn failed without timestamp\n" +
				kimiTurnFailedLogLines(now, 5, providerMsg),
			wantFound:   true,
			wantMsgPart: providerMsg,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seedKimiSessionFiles(t, home, "ses_log", "", tt.log)
			msg, found := readKimiTurnFailure("ses_log", now.Add(-time.Minute), slog.Default())
			if found != tt.wantFound {
				t.Fatalf("found = %v, want %v (msg=%q)", found, tt.wantFound, msg)
			}
			if tt.wantMsgPart != "" && !strings.Contains(msg, tt.wantMsgPart) {
				t.Fatalf("expected message to contain %q, got %q", tt.wantMsgPart, msg)
			}
			// Clean up so subtests don't see each other's files.
			if err := os.RemoveAll(filepath.Join(home, "sessions")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestReadKimiTurnFailureMissingLog pins the no-signal case: kimi builds
// that write no session log (or a custom data dir) must not false-positive.
func TestReadKimiTurnFailureMissingLog(t *testing.T) {
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	msg, found := readKimiTurnFailure("session-nope", time.Now().Add(-time.Minute), slog.Default())
	if found {
		t.Fatalf("expected found=false for missing log, got msg=%q", msg)
	}
}

// fakeKimiACPSetConfigRecordingScript impersonates `kimi acp` and writes the
// JSON-RPC request bodies it receives to requestsFile so tests can assert the
// shape of session/set_config_option calls.
func fakeKimiACPSetConfigRecordingScript(requestsFile string) string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  printf '%s\n' "$line" >> ` + strconv.Quote(requestsFile) + `
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_thinking"}}\n' "$id"
      ;;
    *'"method":"session/set_config_option"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[]}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// TestKimiBackendSetsThinkingLevel verifies that a non-empty ThinkingLevel
// is forwarded via session/set_config_option with configId "thinking" and
// the chosen value before session/prompt.
func TestKimiBackendSetsThinkingLevel(t *testing.T) {
	t.Parallel()

	requestsFile := filepath.Join(t.TempDir(), "requests.jsonl")
	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPSetConfigRecordingScript(requestsFile)))

	result := runFakeKimi(t, fakePath, ExecOptions{ThinkingLevel: "max"})
	if result.Status != "completed" {
		t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
	}

	raw, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests file: %v", err)
	}

	var found bool
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if !strings.Contains(line, `"method":"session/set_config_option"`) {
			continue
		}
		found = true
		if !strings.Contains(line, `"configId":"thinking"`) {
			t.Errorf("set_config_option missing configId=thinking: %s", line)
		}
		if !strings.Contains(line, `"value":"max"`) {
			t.Errorf("set_config_option missing value=max: %s", line)
		}
		if !strings.Contains(line, `"sessionId":"ses_thinking"`) {
			t.Errorf("set_config_option missing sessionId: %s", line)
		}
	}
	if !found {
		t.Fatalf("session/set_config_option call not recorded")
	}
}

// fakeKimiACPSetConfigFailureScript impersonates `kimi acp` rejecting the
// thinking-level change. The backend must fail the task (not silently run
// on the default level).
func fakeKimiACPSetConfigFailureScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_thinking_fail"}}\n' "$id"
      ;;
    *'"method":"session/set_config_option"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"Invalid params","data":{"value":"Unsupported thinking level"}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// TestKimiBackendSetThinkingLevelFailureFailsTask pins the fail-hard
// behaviour: if the runtime rejects the requested thinking level, the task
// must fail rather than falling back to the default.
func TestKimiBackendSetThinkingLevelFailureFailsTask(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPSetConfigFailureScript()))

	result := runFakeKimi(t, fakePath, ExecOptions{ThinkingLevel: "max"})
	if result.Status != "failed" {
		t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, `could not set thinking level "max"`) {
		t.Errorf("expected error to name the requested level, got %q", result.Error)
	}
	if !strings.Contains(result.Error, "Unsupported thinking level") {
		t.Errorf("expected error to surface upstream message, got %q", result.Error)
	}
}

// TestKimiBackendOmitsSetConfigOptionWhenNoThinkingLevel verifies that no
// session/set_config_option call is made when ThinkingLevel is empty.
func TestKimiBackendOmitsSetConfigOptionWhenNoThinkingLevel(t *testing.T) {
	t.Parallel()

	requestsFile := filepath.Join(t.TempDir(), "requests.jsonl")
	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPSetConfigRecordingScript(requestsFile)))

	result := runFakeKimi(t, fakePath, ExecOptions{})
	if result.Status != "completed" {
		t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
	}

	raw, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests file: %v", err)
	}
	if strings.Contains(string(raw), `"method":"session/set_config_option"`) {
		t.Fatalf("set_config_option should not be called when ThinkingLevel is empty")
	}
}
