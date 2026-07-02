package daemon

import (
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// TestClassifyTaskResult is the table-driven coverage for the status switch
// extracted from runTask (F1a). Each case pins the TaskResult disposition for
// one branch of classifyTaskResult so regressions in the completed / empty-
// output / poisoned / timeout / idle_watchdog / cancelled / default routing
// surface as a unit test instead of only end-to-end via d.runner.
func TestClassifyTaskResult(t *testing.T) {
	const (
		provider    = "claude"
		workDir     = "/wd"
		envRoot     = "/root"
		sessionID   = "sess-123"
		agentTO     = 5 * time.Minute
		idleWatch   = 30 * time.Minute
	)
	env := &execenv.Environment{WorkDir: workDir, RootDir: envRoot}
	usage := []TaskUsageEntry{{Provider: provider, Model: "claude-3", InputTokens: 10, OutputTokens: 20}}
	discardLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &Daemon{cfg: Config{AgentTimeout: agentTO, AgentIdleWatchdog: idleWatch}}

	// expectedComment is resolved per-case so derived messages (timeout /
	// idle_watchdog / unknown-status fallbacks) stay in lockstep with the
	// production formatters without hardcoding their output verbatim.
	type expect struct {
		status        string
		comment       string
		failureReason string
	}

	cases := []struct {
		name     string
		provider string
		result   agent.Result
		want     expect
	}{
		{
			name:   "completed empty output",
			result: agent.Result{Status: "completed", Output: "", SessionID: sessionID},
			want:   expect{status: "completed", comment: ""},
		},
		{
			name:   "completed normal output",
			result: agent.Result{Status: "completed", Output: "hello world", SessionID: sessionID},
			want:   expect{status: "completed", comment: "hello world"},
		},
		{
			name:   "completed poisoned iteration limit",
			result: agent.Result{Status: "completed", Output: "I reached the iteration limit", SessionID: sessionID},
			want:   expect{status: "blocked", comment: "I reached the iteration limit", failureReason: FailureReasonIterationLimit},
		},
		{
			name:   "completed poisoned agent fallback message",
			result: agent.Result{Status: "completed", Output: "put your final update inside the content string", SessionID: sessionID},
			want:   expect{status: "blocked", comment: "put your final update inside the content string", failureReason: FailureReasonAgentFallbackMsg},
		},
		{
			name:   "timeout with explicit error keeps error as comment",
			result: agent.Result{Status: "timeout", Error: "backend slow", SessionID: sessionID},
			want:   expect{status: "blocked", comment: "backend slow", failureReason: "timeout"},
		},
		{
			name:   "timeout with empty error synthesises comment from config",
			result: agent.Result{Status: "timeout", Error: "", SessionID: sessionID},
			want:   expect{status: "blocked", comment: fmt.Sprintf("%s timed out after %s", provider, agentTO), failureReason: "timeout"},
		},
		{
			// classifyResumeUnsafeTimeout is provider-gated to "codex", so this
			// case must run under provider=codex (not the shared default).
			name:     "codex resume-unsafe timeout overrides failure reason",
			provider: "codex",
			result:   agent.Result{Status: "timeout", Error: "codex semantic inactivity timeout mid-run", SessionID: sessionID},
			want:     expect{status: "blocked", comment: "codex semantic inactivity timeout mid-run", failureReason: FailureReasonCodexSemanticInactivity},
		},
		{
			name:   "idle_watchdog with explicit error",
			result: agent.Result{Status: "idle_watchdog", Error: "frozen child process", SessionID: sessionID},
			want:   expect{status: "blocked", comment: "frozen child process", failureReason: "idle_watchdog"},
		},
		{
			name:   "idle_watchdog with empty error synthesises comment",
			result: agent.Result{Status: "idle_watchdog", Error: "", SessionID: sessionID},
			want:   expect{status: "blocked", comment: idleWatchdogReason(idleWatch), failureReason: "idle_watchdog"},
		},
		{
			name:   "cancelled preserves status string",
			result: agent.Result{Status: "cancelled", SessionID: sessionID},
			want:   expect{status: "cancelled", comment: "task cancelled by server"},
		},
		{
			name:   "default failed with classified provider auth error",
			result: agent.Result{Status: "failed", Error: "API Error: 401 Unauthorized", SessionID: sessionID},
			want:   expect{status: "blocked", comment: "API Error: 401 Unauthorized", failureReason: "agent_error.provider_auth_or_access"},
		},
		{
			name:   "default failed with poisoned API invalid request",
			result: agent.Result{Status: "failed", Error: `API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"Could not process image"}}`, SessionID: sessionID},
			want:   expect{status: "blocked", comment: `API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"Could not process image"}}`, failureReason: FailureReasonAPIInvalidRequest},
		},
		{
			name:   "default failed with empty error synthesises message and falls back to unknown taxonomy",
			result: agent.Result{Status: "failed", Error: "", SessionID: sessionID},
			want:   expect{status: "blocked", comment: fmt.Sprintf("%s execution %s", provider, "failed"), failureReason: "agent_error.unknown"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caseProvider := tc.provider
			if caseProvider == "" {
				caseProvider = provider
			}
			got := d.classifyTaskResult(tc.result, env, caseProvider, usage, discardLog)

			if got.Status != tc.want.status {
				t.Errorf("Status = %q, want %q", got.Status, tc.want.status)
			}
			if got.Comment != tc.want.comment {
				t.Errorf("Comment = %q, want %q", got.Comment, tc.want.comment)
			}
			if got.FailureReason != tc.want.failureReason {
				t.Errorf("FailureReason = %q, want %q", got.FailureReason, tc.want.failureReason)
			}
			// Identity-forwarded fields must be plumbed through on every branch.
			if got.SessionID != sessionID {
				t.Errorf("SessionID = %q, want %q", got.SessionID, sessionID)
			}
			if got.WorkDir != workDir {
				t.Errorf("WorkDir = %q, want %q", got.WorkDir, workDir)
			}
			if got.EnvRoot != envRoot {
				t.Errorf("EnvRoot = %q, want %q", got.EnvRoot, envRoot)
			}
			if len(got.Usage) != len(usage) {
				t.Errorf("len(Usage) = %d, want %d", len(got.Usage), len(usage))
			}
		})
	}
}

// TestClassifyTaskResultPreservesUsage asserts the usage slice is forwarded by
// value, not silently dropped, regardless of branch — a regression here would
// lose per-model token accounting on the server.
func TestClassifyTaskResultPreservesUsage(t *testing.T) {
	d := &Daemon{cfg: Config{AgentTimeout: time.Minute, AgentIdleWatchdog: time.Minute}}
	env := &execenv.Environment{WorkDir: "/wd", RootDir: "/root"}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	usage := []TaskUsageEntry{
		{Provider: "claude", Model: "m1", InputTokens: 1},
		{Provider: "claude", Model: "m2", OutputTokens: 2},
	}
	got := d.classifyTaskResult(agent.Result{Status: "completed", Output: "done"}, env, "claude", usage, log)
	if len(got.Usage) != 2 || got.Usage[0].InputTokens != 1 || got.Usage[1].OutputTokens != 2 {
		t.Errorf("usage not forwarded verbatim: %+v", got.Usage)
	}
}
