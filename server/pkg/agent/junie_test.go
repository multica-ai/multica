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

func TestNewReturnsJunieBackend(t *testing.T) {
	t.Parallel()
	b, err := New("junie", Config{ExecutablePath: "/nonexistent/junie"})
	if err != nil {
		t.Fatalf("New(junie) error: %v", err)
	}
	if _, ok := b.(*junieBackend); !ok {
		t.Fatalf("expected *junieBackend, got %T", b)
	}
}

// TestJunieBlockedArgsProtectsACPFlag verifies custom_args cannot drop or
// override the --acp launch flag, in either inline (--acp=false) or
// two-token (--acp false) form, while unrelated custom args pass through
// unchanged. Mirrors the equivalent coverage in kiro_test.go / reasonix_test.go
// for their own protocol-critical flags.
func TestJunieBlockedArgsProtectsACPFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "inline override is stripped",
			in:   []string{"--acp=false", "--verbose"},
			want: []string{"--verbose"},
		},
		{
			name: "two-token override is stripped along with its value",
			in:   []string{"--acp", "false", "--verbose"},
			want: []string{"--verbose"},
		},
		{
			name: "unrelated args pass through untouched",
			in:   []string{"--verbose", "--log-level=debug"},
			want: []string{"--verbose", "--log-level=debug"},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := filterCustomArgs(tt.in, junieBlockedArgs, slog.Default())
			if len(got) != len(tt.want) {
				t.Fatalf("filterCustomArgs(%v) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("filterCustomArgs(%v) = %v, want %v", tt.in, got, tt.want)
				}
			}
		})
	}
}

// fakeJunieACPScript is a minimal ACP server that answers the handshake
// junieBackend.Execute drives: initialize, session/new, session/load,
// session/set_model, session/prompt. Mirrors fakeKiroACPScript's shape.
func fakeJunieACPScript() string {
	return `#!/bin/sh
if [ -n "$JUNIE_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$JUNIE_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  if [ -n "$JUNIE_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$JUNIE_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_new","models":{"currentModelId":"auto","availableModels":[{"modelId":"auto","name":"auto"}]}}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      if [ -n "$JUNIE_SESSION_NOT_FOUND" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"session not found"}}\n' "$id"
        exit 0
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      case "$line" in
        *bogus-model*)
          printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"model not available: bogus-model"}}\n' "$id"
          exit 0
          ;;
        *)
          printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
          ;;
      esac
      ;;
    *'"method":"session/prompt"'*)
      case "$line" in
        *'"prompt":'*)
          ;;
        *)
          printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"session/prompt must send prompt"}}\n' "$id"
          exit 0
          ;;
      esac
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_loaded","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"loaded"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":2,"outputTokens":1}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestJunieBackendInvokesACPWithFlag(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "junie")
	writeTestExecutable(t, fakePath, []byte(fakeJunieACPScript()))

	backend, err := New("junie", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"JUNIE_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new junie backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:    5 * time.Second,
		CustomArgs: []string{"--acp=false", "--verbose"},
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
		t.Fatalf("expected completed result, got status=%q error=%q", result.Status, result.Error)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) == 0 || lines[0] != "--acp=true" {
		t.Fatalf("expected --acp=true as the first launch arg, got %q", lines)
	}
	for _, got := range lines {
		if got == "--acp=false" {
			t.Errorf("protocol-critical custom arg override was not filtered: %q", lines)
		}
	}
	found := false
	for _, got := range lines {
		if got == "--verbose" {
			found = true
		}
	}
	if !found {
		t.Errorf("unrelated custom arg was dropped: %q", lines)
	}
}

func TestJunieBackendUsesSessionLoadForResume(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	requestsFile := filepath.Join(tempDir, "requests.jsonl")
	fakePath := filepath.Join(tempDir, "junie")
	writeTestExecutable(t, fakePath, []byte(fakeJunieACPScript()))

	backend, err := New("junie", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"JUNIE_REQUESTS_FILE": requestsFile},
	})
	if err != nil {
		t.Fatalf("new junie backend: %v", err)
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
		t.Fatalf("expected completed result, got status=%q error=%q", result.Status, result.Error)
	}
	if result.Output != "loaded" {
		t.Fatalf("output = %q, want loaded", result.Output)
	}
	if result.SessionID != "ses_existing" {
		t.Fatalf("session id = %q, want ses_existing", result.SessionID)
	}
	// Negative control for TestJunieSessionLoadNotFoundSignalsResumeRejected:
	// a successful session/load must never claim the resume was rejected.
	if result.ResumeRejected {
		t.Fatalf("ResumeRejected must be false when session/load succeeded")
	}

	raw, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests file: %v", err)
	}
	requests := string(raw)
	if !strings.Contains(requests, `"method":"session/load"`) {
		t.Fatalf("expected session/load request, got:\n%s", requests)
	}
	if strings.Contains(requests, `"method":"session/new"`) {
		t.Fatalf("junie backend must not call session/new on a resume, got:\n%s", requests)
	}
	if !strings.Contains(requests, `"mcpServers":[]`) {
		t.Fatalf("session/load must include mcpServers, got:\n%s", requests)
	}
}

func TestJunieBackendSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "junie")
	writeTestExecutable(t, fakePath, []byte(fakeJunieACPScript()))

	backend, err := New("junie", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new junie backend: %v", err)
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
		if result.SessionID != "ses_new" {
			t.Errorf("expected session id to be preserved on failure, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// TestJunieSessionLoadNotFoundSignalsResumeRejected pins the resume-recovery
// contract at the session/load boundary. When the runtime refuses the session
// outright, the backend must clear the session id and set ResumeRejected so
// shouldRetryWithFreshSession starts a new session; without it the daemon
// reads the zero value as "checked, not a rejection" and replays the dead id
// on every subsequent turn. Mirrors TestQwenpawSessionLoadNotFound.
func TestJunieSessionLoadNotFoundSignalsResumeRejected(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "junie")
	writeTestExecutable(t, fakePath, []byte(fakeJunieACPScript()))

	backend, err := New("junie", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"JUNIE_SESSION_NOT_FOUND": "1"},
	})
	if err != nil {
		t.Fatalf("new junie backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "continue", ExecOptions{
		ResumeSessionID: "ses_gone",
		Timeout:         5 * time.Second,
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
		if !strings.Contains(result.Error, "session/load failed") {
			t.Errorf("expected the error to name session/load, got %q", result.Error)
		}
		if !result.ResumeRejected {
			t.Fatal("expected ResumeRejected=true so the daemon retries from a fresh session")
		}
		if result.SessionID != "" {
			t.Errorf("expected the dead session id to be cleared, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// TestJunieBackendCancelsBeforeWaitingForLingeringProcess proves the run
// goroutine's cleanup reaches cancel() before cmd.Wait(). A child that closes
// stdout/stderr (so the pipe drain completes) but stays alive would otherwise
// pin Wait until the task timeout, leaving the deferred cancel unreachable and
// both channels open.
//
// The fixture must fail on an INIT-PATH request rather than at session/prompt:
// the success path already calls cancel() unconditionally before draining, so
// a happy-path fixture goes green even against the unfixed ordering.
func TestJunieBackendCancelsBeforeWaitingForLingeringProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	cases := []struct {
		name      string
		script    string
		wantError string
	}{
		{
			name: "initialize",
			script: `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"init boom"}}\n' "$id"
      exec 1>&-
      exec 2>&-
      while :; do sleep 1; done
      ;;
  esac
done
`,
			wantError: "junie initialize failed",
		},
		{
			name: "session/new",
			script: `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"new boom"}}\n' "$id"
      exec 1>&-
      exec 2>&-
      while :; do sleep 1; done
      ;;
  esac
done
`,
			wantError: "junie session/new failed",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fakePath := filepath.Join(t.TempDir(), "junie")
			writeTestExecutable(t, fakePath, []byte(tc.script))

			backend, err := New("junie", Config{ExecutablePath: fakePath, Logger: slog.Default()})
			if err != nil {
				t.Fatalf("new junie backend: %v", err)
			}
			// The per-run timeout is deliberately far longer than the
			// assertion windows below: if the deadline could fire first it
			// would unblock Wait on its own and mask the regression.
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 30 * time.Second})
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
				if !strings.Contains(result.Error, tc.wantError) {
					t.Fatalf("expected error %q, got %q", tc.wantError, result.Error)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timeout waiting for result")
			}

			select {
			case _, ok := <-session.Result:
				if ok {
					t.Fatal("result channel produced an unexpected second value")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("result channel did not close; cmd.Wait likely ran before cancel")
			}
		})
	}
}
