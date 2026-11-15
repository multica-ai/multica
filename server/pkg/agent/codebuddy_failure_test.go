package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestCodebuddyFailureDetails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixtures are POSIX-only")
	}
	tests := []struct {
		name       string
		frame      string
		exitCode   int
		wantReason taskfailure.Reason
		want       []string
	}{
		{
			name:       "errors array",
			frame:      `{"type":"result","is_error":true,"errors":["unknown model review-model"]}`,
			wantReason: taskfailure.ReasonAgentModelNotFoundOrUnavailable,
			want:       []string{"unknown model review-model", "saw_result=true", "result_is_error=true", "result_bytes=0"},
		},
		{
			name:       "structured details with null entries",
			frame:      `{"type":"result","is_error":true,"errors_info":[null,{"status":404,"details":"unknown model review-model","code":"MODEL_NOT_FOUND"}]}`,
			wantReason: taskfailure.ReasonAgentModelNotFoundOrUnavailable,
			want:       []string{"unknown model review-model", "HTTP 404"},
		},
		{
			name:       "all error sources",
			frame:      `{"type":"result","is_error":true,"result":"request rejected","errors":["unknown model review-model"],"errors_info":[{"status":404,"details":"use an available model"}]}`,
			wantReason: taskfailure.ReasonAgentModelNotFoundOrUnavailable,
			want:       []string{"request rejected", "unknown model review-model", "use an available model", "HTTP 404"},
		},
		{
			name:       "HTTP auth without prose",
			frame:      `{"type":"result","is_error":true,"errors_info":[{"status":401,"code":10006}]}`,
			wantReason: taskfailure.ReasonAgentProviderAuthOrAccess,
			want:       []string{"HTTP 401"},
		},
		{
			name:       "HTTP quota",
			frame:      `{"type":"result","is_error":true,"errors_info":[{"status":402,"details":"balance is too low"}]}`,
			wantReason: taskfailure.ReasonAgentProviderQuotaLimit,
			want:       []string{"HTTP 402", "balance is too low"},
		},
		{
			name:       "HTTP capacity",
			frame:      `{"type":"result","is_error":true,"errors_info":[{"status":429}]}`,
			wantReason: taskfailure.ReasonAgentProviderCapacityOrRateLimit,
			want:       []string{"HTTP 429"},
		},
		{
			name:       "HTTP server failure",
			frame:      `{"type":"result","is_error":true,"errors_info":[{"status":503}]}`,
			wantReason: taskfailure.ReasonAgentProviderServerError,
			want:       []string{"HTTP 503"},
		},
		{
			name:       "null diagnostics",
			frame:      `{"type":"result","is_error":true,"errors":null,"errors_info":[null,{}]}`,
			wantReason: taskfailure.ReasonAgentUnknown,
			want:       []string{"returned an error result without details", "event_count=1", "stderr_bytes=0"},
		},
		{
			name:       "business code is not HTTP status",
			frame:      `{"type":"result","is_error":true,"errors_info":[{"code":401}]}`,
			wantReason: taskfailure.ReasonAgentUnknown,
			want:       []string{"returned an error result without details"},
		},
		{
			name:       "explicit error keeps actual exit code",
			frame:      `{"type":"result","is_error":true}`,
			exitCode:   7,
			wantReason: taskfailure.ReasonAgentUnknown,
			want:       []string{"returned an error result without details", "exit_code=7"},
		},
		{
			name:       "no terminal event",
			frame:      `{"type":"system","session_id":"review-session"}`,
			wantReason: taskfailure.ReasonAgentUnknown,
			want:       []string{"stream ended without terminal result", "saw_result=false", "event_count=1"},
		},
		{
			name:       "empty stdout",
			wantReason: taskfailure.ReasonAgentUnknown,
			want:       []string{"stdout_bytes=0", "event_count=0", "invalid_event_count=0", "saw_result=false"},
		},
		{
			name:       "malformed stdout",
			frame:      "not-json",
			wantReason: taskfailure.ReasonAgentUnknown,
			want:       []string{"stdout_bytes=9", "event_count=0", "invalid_event_count=1"},
		},
		{
			name:       "unparseable optional error fields preserve terminal event",
			frame:      `{"type":"result","is_error":true,"errors":"unexpected","errors_info":[{"status":"unexpected","details":"unknown model review-model"}]}`,
			wantReason: taskfailure.ReasonAgentModelNotFoundOrUnavailable,
			want:       []string{"saw_result=true", "unknown model review-model"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			script := ""
			if tt.frame != "" {
				script = fmt.Sprintf("printf '%%s\\n' '%s'\n", tt.frame)
			}
			script += fmt.Sprintf("exit %d\n", tt.exitCode)
			result := runCodebuddyFailureFixture(t, script)
			if result.Status != "failed" || result.Output != "" {
				t.Fatalf("outcome = %s/%q, error=%q", result.Status, result.Output, result.Error)
			}
			for _, want := range tt.want {
				if !strings.Contains(result.Error, want) {
					t.Errorf("error %q does not contain %q", result.Error, want)
				}
			}
			if got := taskfailure.Classify(result.Error); got != tt.wantReason {
				t.Errorf("Classify = %s, want %s; error=%q", got, tt.wantReason, result.Error)
			}
		})
	}
}

func TestCodebuddyFailureDiagnosticsAreBoundedAndRedacted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	result := runCodebuddyFailureFixture(t, `printf '%s\n' '{"type":"result","is_error":true,"errors":["unknown model review-model; api_key=private-key"]}'
printf '%s\n' 'Authorization: Bearer private-token' >&2
`)
	for _, secret := range []string{"private-key", "private-token"} {
		if strings.Contains(result.Error, secret) {
			t.Errorf("persisted error contains %q: %s", secret, result.Error)
		}
	}
	if !strings.Contains(result.Error, "[REDACTED]") || !strings.Contains(result.Error, "stderr_bytes=36") {
		t.Errorf("missing redaction or original stderr byte count: %s", result.Error)
	}
	large := runCodebuddyFailureFixture(t, "printf '%s\\n' '{\"type\":\"result\",\"is_error\":true,\"errors\":[\""+strings.Repeat("界", 10000)+"\"]}'\n")
	if !utf8.ValidString(large.Error) || len(large.Error) > 9000 || !strings.Contains(large.Error, "[truncated]") || !strings.Contains(large.Error, "saw_result=true") {
		t.Errorf("invalid or unbounded diagnostic: bytes=%d, valid_utf8=%v", len(large.Error), utf8.ValidString(large.Error))
	}
}

func TestCodebuddySuccessIgnoresErrorMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	result := runCodebuddyFailureFixture(t, `printf '%s\n' '{"type":"result","is_error":false,"result":"answer","errors":["stale error"],"errors_info":[{"status":401}]}'`+"\n")
	if result.Status != "completed" || result.Output != "answer" || result.Error != "" {
		t.Fatalf("successful result changed: %+v", result)
	}
}

func TestCodebuddyFailureDiagnosticsContextEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixtures are POSIX-only")
	}
	for _, outcome := range []string{"aborted", "timeout"} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			fakePath := filepath.Join(t.TempDir(), "codebuddy")
			writeTestExecutable(t, fakePath, []byte("#!/bin/sh\nIFS= read -r _\n"+`printf '%s\n' '{"type":"system","session_id":"context-end"}'`+"\nsleep 60\n"))
			b := &codebuddyBackend{cfg: Config{ExecutablePath: fakePath, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			opts := ExecOptions{}
			if outcome == "timeout" {
				opts.Timeout = 300 * time.Millisecond
			}
			session, err := b.Execute(ctx, "fixture", opts)
			if err != nil {
				t.Fatal(err)
			}
			for msg := range session.Messages {
				if outcome == "aborted" && msg.Type == MessageStatus {
					cancel()
				}
			}
			result := <-session.Result
			if result.Status != outcome || !strings.Contains(result.Error, "scanner_error=false") {
				t.Fatalf("context shutdown reported as a scanner error: %+v", result)
			}
		})
	}
}

func runCodebuddyFailureFixture(t *testing.T, body string) Result {
	t.Helper()
	fakePath := filepath.Join(t.TempDir(), "codebuddy")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\nIFS= read -r _\n"+body))
	b := &codebuddyBackend{cfg: Config{ExecutablePath: fakePath, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	session, err := b.Execute(ctx, "fixture", ExecOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	result, ok := <-session.Result
	if !ok {
		t.Fatal("result channel closed without a value")
	}
	return result
}
