package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

// TestIsACPResumeRejected pins the widened resume-boundary predicate. The two
// halves that matter are the qodercli frame from GH #8116 — the wording that
// wedged a conversation because no literal in isACPSessionNotFound matched it —
// and the negatives, which are the whole reason this is a phrase predicate and
// not "any error from session/resume".
func TestIsACPResumeRejected(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "qodercli invalid session identifier",
			// The frame that made GH #8116 permanent: qodercli 1.1.25 answers
			// session/resume for an id it never persisted with invalid_params
			// and this wording. The code was already accepted; only the phrase
			// was missing, so the rejection was invisible to the daemon.
			err: &acpRPCError{
				Method:  "session/resume",
				Code:    -32602,
				Message: `Invalid session identifier "27d8031c-9fea-4bda-9d42-37c36fa9aebf".`,
				Data:    `{"sessionId":"27d8031c-9fea-4bda-9d42-37c36fa9aebf"}`,
			},
			want: true,
		},
		{
			name: "everything isACPSessionNotFound already matched still matches",
			err:  &acpRPCError{Method: "session/resume", Code: -32603, Message: "Session not found"},
			want: true,
		},
		{
			name: "session expired",
			err:  &acpRPCError{Method: "session/load", Code: -32602, Message: "session expired"},
			want: true,
		},
		{
			name: "session does not exist",
			err:  &acpRPCError{Method: "session/load", Code: -32603, Message: "Internal error", Data: "the conversation does not exist"},
			want: true,
		},
		{
			name: "unreachable mcp server whose data echoes the session id",
			// The false positive the adjacency window exists to stop. Both
			// halves are present in the string — "Invalid params" and a
			// sessionId in `data` — but they are not about each other, and
			// discarding the pointer here would fork a healthy conversation
			// over a transient MCP outage.
			err: &acpRPCError{
				Method:  "session/resume",
				Code:    -32602,
				Message: "Invalid params: mcpServers[0] transport unreachable",
				Data:    `{"sessionId":"ses_healthy"}`,
			},
			want: false,
		},
		{
			name: "provider rate limit during resume",
			err:  &acpRPCError{Method: "session/resume", Code: -32603, Message: "Internal error", Data: "upstream provider returned HTTP 429"},
			want: false,
		},
		{
			name: "auth failure during resume keeps the session",
			err:  &acpRPCError{Method: "session/load", Code: -32603, Message: "Could not resolve authentication method"},
			want: false,
		},
		{
			name: "session wording under an unrelated code",
			err:  &acpRPCError{Method: "session/resume", Code: -32601, Message: "Invalid session identifier"},
			want: false,
		},
		{
			name: "plain error is never a rejection",
			err:  fmt.Errorf("session/resume: Invalid session identifier (code=-32602)"),
			want: false,
		},
		{
			name: "wrapped rpc error",
			err:  fmt.Errorf("request failed: %w", &acpRPCError{Method: "session/resume", Code: -32602, Message: "Invalid session identifier"}),
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isACPResumeRejected(tc.err); got != tc.want {
				t.Errorf("isACPResumeRejected(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestClassifyACPResumeFailureCancelAndTimeoutAreNotRejections pins the
// ordering that keeps a cancel from being read as a dead session.
// ResumeRejected licenses the daemon to abandon the recorded conversation and
// re-run the task from a fresh session — precisely what a user who just hit
// cancel did not ask for.
func TestClassifyACPResumeFailureCancelAndTimeoutAreNotRejections(t *testing.T) {
	t.Parallel()

	rejection := &acpRPCError{Method: "session/resume", Code: -32602, Message: "Invalid session identifier"}

	t.Run("cancelled", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		status, _, rejected := classifyACPResumeFailure(ctx, "qoder", "session/resume", rejection, time.Minute, nil)
		if status != "aborted" {
			t.Errorf("status = %q, want aborted", status)
		}
		if rejected {
			t.Error("a cancelled resume must not report ResumeRejected")
		}
	})

	t.Run("deadline exceeded", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		status, errText, rejected := classifyACPResumeFailure(ctx, "kiro", "session/load", rejection, 30*time.Second, nil)
		if status != "timeout" {
			t.Errorf("status = %q, want timeout", status)
		}
		if rejected {
			t.Error("a timed-out resume must not report ResumeRejected")
		}
		if errText == "" {
			t.Error("expected a non-empty timeout message")
		}
	})

	t.Run("live context reports the rejection", func(t *testing.T) {
		t.Parallel()
		status, errText, rejected := classifyACPResumeFailure(context.Background(), "qoder", "session/resume", rejection, time.Minute, nil)
		if status != "failed" {
			t.Errorf("status = %q, want failed", status)
		}
		if !rejected {
			t.Error("expected ResumeRejected=true for a rejected session id")
		}
		if want := "qoder session/resume failed: "; len(errText) < len(want) || errText[:len(want)] != want {
			t.Errorf("error text = %q, want it to keep the %q prefix", errText, want)
		}
	})

	t.Run("non-rejection keeps the pointer", func(t *testing.T) {
		t.Parallel()
		netErr := &acpRPCError{Method: "session/resume", Code: -32603, Message: "Internal error", Data: "upstream provider returned HTTP 429"}
		status, _, rejected := classifyACPResumeFailure(context.Background(), "qoder", "session/resume", netErr, time.Minute, nil)
		if status != "failed" {
			t.Errorf("status = %q, want failed", status)
		}
		if rejected {
			t.Error("a rate-limited resume must not discard the conversation pointer")
		}
	})
}

// fakeACPRejectedResumeScript answers initialize (advertising the auth method
// Grok insists on, harmless to the others) and then rejects whichever resume
// RPC the backend uses with qodercli 1.1.25's wording from GH #8116.
//
// The message deliberately carries no inner quotes: `sh` printf mangles
// backslash-escaped quotes inconsistently, and a malformed error frame is
// silently dropped by the ACP client, which shows up as a hung request rather
// than a failed assertion.
func fakeACPRejectedResumeScript(rpc string) string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"authMethods":[{"id":"xai.api_key","name":"API key"}],"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"authenticate"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"` + rpc + `"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"Invalid session identifier ses_ghost.","data":{"sessionId":"ses_ghost"}}}\n' "$id"
      ;;
  esac
done
`
}

// TestACPBackendsReportRejectedResume is the end-to-end guard for GH #8116:
// every ACP backend that resumes must turn its runtime's rejection into
// ResumeRejected, because that flag is the ONLY positive evidence
// shouldRetryWithFreshSession accepts. Without it the daemon reads the bare
// failure as "checked, not a rejection", keeps the dead pointer, and every
// later message in that conversation replays the same rejection forever.
//
// Table-driven across all six on purpose. These call sites were six separate
// copies of the same bug, and the neighbouring history in this package — the
// same resume defect fixed one adapter at a time for Kiro, then Kimi, then
// Anthropic — is what happens when one backend gets a regression test and the
// rest do not.
func TestACPBackendsReportRejectedResume(t *testing.T) {
	t.Parallel()

	cases := []struct {
		agentType string
		binary    string
		rpc       string
		env       map[string]string
	}{
		{agentType: "qoder", binary: "qodercli", rpc: "session/resume"},
		{agentType: "hermes", binary: "hermes", rpc: "session/resume"},
		{agentType: "kimi", binary: "kimi", rpc: "session/resume"},
		{agentType: "kiro", binary: "kiro", rpc: "session/load"},
		{agentType: "traecli", binary: "traecli", rpc: "session/load"},
		// Grok refuses to reach session/load without a credential; supply one
		// through Config so the test does not depend on ambient env.
		{agentType: "grok", binary: "grok", rpc: "session/load", env: map[string]string{"XAI_API_KEY": "test-only-key"}},
	}

	for _, tc := range cases {
		t.Run(tc.agentType, func(t *testing.T) {
			t.Parallel()

			fakePath := filepath.Join(t.TempDir(), tc.binary)
			writeTestExecutable(t, fakePath, []byte(fakeACPRejectedResumeScript(tc.rpc)))

			backend, err := New(tc.agentType, Config{ExecutablePath: fakePath, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Env: tc.env})
			if err != nil {
				t.Fatalf("new %s backend: %v", tc.agentType, err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
				Timeout:         30 * time.Second,
				ResumeSessionID: "ses_ghost",
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
				if !result.ResumeRejected {
					t.Fatalf("expected ResumeRejected=true so the daemon retries from a fresh session, got false (error=%q)", result.Error)
				}
			case <-time.After(20 * time.Second):
				t.Fatal("timeout waiting for result")
			}
		})
	}
}
