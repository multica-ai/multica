package agent

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFinalizeStreamResultEmptySuccessWithoutAssistantUsesSafeNotice(t *testing.T) {
	t.Parallel()

	status, output, errMsg := finalizeStreamResult(
		"claude",
		time.Second,
		nil,
		nil,
		nil,
		"session-1",
		streamTerminalState{sawResult: true},
		"",
	)
	if status != "completed" || output != emptySuccessfulStreamResult || errMsg != "" {
		t.Fatalf("finalizeStreamResult() = (%q, %q, %q), want completed safe notice", status, output, errMsg)
	}
}

func TestFinalizeStreamResultPreservesErrorResultWhenContextEnds(t *testing.T) {
	t.Parallel()

	for _, runErr := range []error{context.DeadlineExceeded, context.Canceled} {
		status, output, errMsg := finalizeStreamResult(
			"claude",
			time.Second,
			runErr,
			nil,
			nil,
			"session-1",
			streamTerminalState{
				finalResultText: "provider rejected the request",
				sawResult:       true,
				resultIsError:   true,
			},
			"",
		)
		if status != "failed" || output != "" || errMsg != "provider rejected the request" {
			t.Errorf("runErr=%v: finalizeStreamResult() = (%q, %q, %q), want failed provider error", runErr, status, output, errMsg)
		}
	}
}

func TestStreamProtocolObservationDoesNotLogContent(t *testing.T) {
	t.Parallel()

	const (
		assistantSecret = "FIRST-TURN PRIVATE NARRATION"
		resultSecret    = "FINAL PRIVATE RESULT"
		baseURLSecret   = "https://provider.example/private"
	)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	logStreamProtocolObservation(logger, streamProtocolObservation{
		provider:                   "claude",
		cliVersion:                 "2.1.5",
		model:                      "glm-4.6",
		exitCode:                   0,
		eventCount:                 7,
		invalidEventCount:          1,
		assistantEventCount:        2,
		toolUseCount:               1,
		sawResult:                  true,
		resultBytes:                len(resultSecret),
		lastAssistantBytes:         len(assistantSecret),
		lastEventType:              "result",
		anthropicBaseURLConfigured: true,
	})

	got := buf.String()
	for _, required := range []string{
		"provider=claude",
		"cli_version=2.1.5",
		"model=glm-4.6",
		"exit_code=0",
		"event_count=7",
		"saw_result=true",
		"result_bytes=20",
		"last_event_type=result",
		"anthropic_base_url_configured=true",
	} {
		if !strings.Contains(got, required) {
			t.Errorf("observation log %q does not contain %q", got, required)
		}
	}
	for _, forbidden := range []string{assistantSecret, resultSecret, baseURLSecret} {
		if strings.Contains(got, forbidden) {
			t.Errorf("observation log leaked %q: %q", forbidden, got)
		}
	}
}

func TestStreamJSONBackendsFinalOutputBoundaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixtures are POSIX-only")
	}

	const multiTurnStream = `printf '%s\n' '{"type":"system","session_id":"sess-boundary"}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","model":"test-model","content":[{"type":"text","text":"FIRST-TURN NARRATION"}]}}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","model":"test-model","content":[{"type":"tool_use","id":"tool-1","name":"Read","input":{"path":"README.md"}}]}}'
printf '%s\n' '{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"TOOL TRACE"}]}}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","model":"test-model","content":[{"type":"text","text":"LAST ASSISTANT ANSWER"}]}}'
`
	const toolLastStream = `printf '%s\n' '{"type":"system","session_id":"sess-boundary"}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","model":"test-model","content":[{"type":"text","text":"PRE-TOOL NARRATION"}]}}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","model":"test-model","content":[{"type":"text","text":"I WILL USE A TOOL"},{"type":"tool_use","id":"tool-1","name":"Read","input":{"path":"README.md"}}]}}'
printf '%s\n' '{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"TOOL TRACE"}]}}'
`

	tests := []struct {
		name            string
		scriptBody      string
		wantStatus      string
		wantOutput      string
		wantError       string
		forbiddenOutput []string
	}{
		{
			name:       "non-empty result is authoritative",
			scriptBody: multiTurnStream + `printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"sess-boundary","result":"AUTHORITATIVE RESULT"}'` + "\n",
			wantStatus: "completed",
			wantOutput: "AUTHORITATIVE RESULT",
			forbiddenOutput: []string{
				"FIRST-TURN NARRATION",
				"TOOL TRACE",
				"LAST ASSISTANT ANSWER",
			},
		},
		{
			name:       "empty successful result uses only last assistant message",
			scriptBody: multiTurnStream + `printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"sess-boundary","result":""}'` + "\n",
			wantStatus: "completed",
			wantOutput: "LAST ASSISTANT ANSWER",
			forbiddenOutput: []string{
				"FIRST-TURN NARRATION",
				"TOOL TRACE",
			},
		},
		{
			name:       "empty successful result after tool-using turn uses safe notice",
			scriptBody: toolLastStream + `printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"sess-boundary","result":""}'` + "\n",
			wantStatus: "completed",
			wantOutput: emptySuccessfulStreamResult,
			forbiddenOutput: []string{
				"PRE-TOOL NARRATION",
				"I WILL USE A TOOL",
				"TOOL TRACE",
			},
		},
		{
			name:       "clean exit without result fails closed",
			scriptBody: multiTurnStream,
			wantStatus: "failed",
			wantOutput: "",
			wantError:  "stream ended without terminal result",
			forbiddenOutput: []string{
				"FIRST-TURN NARRATION",
				"TOOL TRACE",
				"LAST ASSISTANT ANSWER",
			},
		},
		{
			name: "scanner error fails closed",
			// The production scanner caps a single event at 10 MiB. A larger
			// token produces bufio.ErrTooLong while the child still exits 0.
			scriptBody: `dd if=/dev/zero bs=1048576 count=11 2>/dev/null | tr '\000' x; printf '\n'` + "\n",
			wantStatus: "failed",
			wantOutput: "",
			wantError:  "stdout read error",
		},
	}

	for _, provider := range []string{"claude", "codebuddy"} {
		provider := provider
		for _, tt := range tests {
			tt := tt
			t.Run(provider+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				fakePath := filepath.Join(t.TempDir(), provider)
				script := "#!/bin/sh\nIFS= read -r _\n" + tt.scriptBody
				writeTestExecutable(t, fakePath, []byte(script))

				backend, err := New(provider, Config{
					ExecutablePath: fakePath,
					Env:            map[string]string{"IS_SANDBOX": "1"},
					Logger:         slog.Default(),
				})
				if err != nil {
					t.Fatalf("New(%s): %v", provider, err)
				}

				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				session, err := backend.Execute(ctx, "test prompt", ExecOptions{Timeout: 10 * time.Second})
				if err != nil {
					t.Fatalf("Execute(%s): %v", provider, err)
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
						t.Fatalf("status = %q, want %q (error=%q, output=%q)", result.Status, tt.wantStatus, result.Error, result.Output)
					}
					if result.Output != tt.wantOutput {
						t.Fatalf("output = %q, want %q", result.Output, tt.wantOutput)
					}
					if tt.wantError != "" && !strings.Contains(result.Error, tt.wantError) {
						t.Fatalf("error = %q, want substring %q", result.Error, tt.wantError)
					}
					for _, forbidden := range tt.forbiddenOutput {
						if strings.Contains(result.Output, forbidden) {
							t.Fatalf("output leaked %q: %q", forbidden, result.Output)
						}
					}
				case <-ctx.Done():
					t.Fatalf("timed out waiting for %s result: %v", provider, ctx.Err())
				}
			})
		}
	}
}
