package agent

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A successful run that produced no deliverable text must report an EMPTY
// output, never a placeholder sentence. Empty is what the issue / direct-chat /
// channel delivery paths each branch on to pick their own no-response behavior;
// any non-empty prose here defeats all three at once (GH #6462).
func TestFinalizeStreamResultEmptySuccessWithoutAssistantReturnsEmptyOutput(t *testing.T) {
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
	if status != "completed" || output != "" || errMsg != "" {
		t.Fatalf("finalizeStreamResult() = (%q, %q, %q), want completed with empty output", status, output, errMsg)
	}
}

// resolveFallback is the single policy every stream-json backend applies per
// assistant event, so pin all four outcomes here rather than only through the
// per-backend stream fixtures.
func TestAssistantTurnResolveFallback(t *testing.T) {
	t.Parallel()

	const prior = "AN ANSWER THE MODEL ALREADY GAVE"
	tests := []struct {
		name string
		turn assistantTurn
		want string
	}{
		{
			name: "text-only turn becomes the fallback",
			turn: assistantTurn{text: "NEW ANSWER", understood: true},
			want: "NEW ANSWER",
		},
		{
			name: "tool-using turn is intermediate and clears it",
			turn: assistantTurn{text: "I WILL USE A TOOL", toolUses: 1, understood: true},
			want: "",
		},
		{
			name: "understood silent turn leaves it alone",
			turn: assistantTurn{understood: true},
			want: prior,
		},
		{
			name: "unreadable turn clears it",
			turn: assistantTurn{understood: false},
			want: "",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.turn.resolveFallback(prior); got != tt.want {
				t.Fatalf("resolveFallback(%q) = %q, want %q", prior, got, tt.want)
			}
		})
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
			name:       "empty successful result after tool-using turn returns empty output",
			scriptBody: toolLastStream + `printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"sess-boundary","result":""}'` + "\n",
			wantStatus: "completed",
			wantOutput: "",
			forbiddenOutput: []string{
				"PRE-TOOL NARRATION",
				"I WILL USE A TOOL",
				"TOOL TRACE",
			},
		},
		{
			// A thinking-only trailing event carries neither an answer nor an
			// intermediacy signal, so it must not discard the answer the model
			// already delivered. Regression for the overwrite that turned a
			// complete reply into a no-response turn.
			name:       "text-less trailing assistant event preserves the last answer",
			scriptBody: multiTurnStream + `printf '%s\n' '{"type":"assistant","message":{"role":"assistant","model":"test-model","content":[{"type":"thinking","text":"SILENT DELIBERATION"}]}}'` + "\n" + `printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"sess-boundary","result":""}'` + "\n",
			wantStatus: "completed",
			wantOutput: "LAST ASSISTANT ANSWER",
			forbiddenOutput: []string{
				"FIRST-TURN NARRATION",
				"TOOL TRACE",
				"SILENT DELIBERATION",
			},
		},
		{
			// An assistant event whose body does not match the expected shape
			// is unreadable, not silent: the model may have answered inside it.
			// The earlier narration must NOT be promoted to the final answer.
			name:       "unparseable trailing assistant event drops the fallback",
			scriptBody: multiTurnStream + `printf '%s\n' '{"type":"assistant","message":"NOT AN OBJECT"}'` + "\n" + `printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"sess-boundary","result":""}'` + "\n",
			wantStatus: "completed",
			wantOutput: "",
			forbiddenOutput: []string{
				"FIRST-TURN NARRATION",
				"TOOL TRACE",
				"LAST ASSISTANT ANSWER",
			},
		},
		{
			// Same rule for a content block type we do not render: we cannot
			// claim the turn was silent, so the fallback must not survive it.
			name:       "unknown content block drops the fallback",
			scriptBody: multiTurnStream + `printf '%s\n' '{"type":"assistant","message":{"role":"assistant","model":"test-model","content":[{"type":"server_tool_use","id":"st-1","name":"web_search"}]}}'` + "\n" + `printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"sess-boundary","result":""}'` + "\n",
			wantStatus: "completed",
			wantOutput: "",
			forbiddenOutput: []string{
				"FIRST-TURN NARRATION",
				"TOOL TRACE",
				"LAST ASSISTANT ANSWER",
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
			// The production scanner caps a single event at
			// agentStreamMaxLineBytes. A larger token produces
			// bufio.ErrTooLong while the child still exits 0. The count is
			// derived from the constant so raising the cap cannot silently
			// turn this case into a plain-oversized-line pass.
			scriptBody: fmt.Sprintf(
				`dd if=/dev/zero bs=1048576 count=%d 2>/dev/null | tr '\000' x; printf '\n'`,
				agentStreamMaxLineBytes/(1024*1024)+1,
			) + "\n",
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
