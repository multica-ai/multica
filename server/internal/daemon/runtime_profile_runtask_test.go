package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// Tests in this file pin the MUL-3414 contract that custom runtime profiles
// produce a clear, redacted, narrowly-scoped failure message — and that the
// rewrite never bleeds into built-in runtimes or genuine
// auth/quota/network errors. Reviewer feedback on PR #4301 (GPT-Boy)
// surfaced three regressions in the first cut that these tests now guard:
//
//   1. The user-visible comment must NOT carry the daemon's local absolute
//      command path (issue/chat is a server-rendered surface; that path is
//      local-machine state per `runtime profile set-path`).
//   2. The rewrite must NOT swallow real auth / missing-config / quota /
//      network failures into "incompatible". Same-protocol wrappers can
//      hit those exactly like the upstream CLI does.
//   3. The droid-shape (binary launches, stays silent, gets killed by
//      timeout / idle watchdog) must produce a compatibility hint. The
//      original PR only handled exec-error and explicit failed-result
//      shapes.

// TestSafeProfileCommandLabel exhaustively pins the redaction rules. The
// label is what users see in failed-task comments — a regression here is
// a privacy regression (leaks home dirs / usernames / install layout).
func TestSafeProfileCommandLabel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"posix abs path", "/Users/alice/.local/bin/grok", "grok"},
		{"system abs path", "/usr/local/bin/droid", "droid"},
		{"relative path", "./bin/grok", "grok"},
		{"bare command", "grok", "grok"},
		{"empty string falls back", "", "the configured command"},
		{"whitespace-only falls back", "   ", "the configured command"},
		{"root falls back", "/", "the configured command"},
		{"trailing slash falls back to dot, then placeholder", ".", "the configured command"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := safeProfileCommandLabel(tc.in)
			if got != tc.want {
				t.Errorf("safeProfileCommandLabel(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestWrapCustomProfileExecError_RedactsAbsolutePath is the privacy
// regression guard for review point #1: the daemon must not echo the
// host's absolute command path back into the user-visible failure
// comment, because that comment is rendered server-side in the issue /
// chat timeline. Only the binary basename should appear; the absolute
// path stays in the structured daemon log via taskLog fields.
func TestWrapCustomProfileExecError_RedactsAbsolutePath(t *testing.T) {
	t.Parallel()

	absPath := "/Users/alice/.local/bin/grok"
	got := wrapCustomProfileExecError("cursor", absPath, "exit status 2")

	if strings.Contains(got, absPath) {
		t.Errorf("wrap output leaked the absolute command path; users would see local FS layout in the issue timeline\nfull: %s", got)
	}
	if strings.Contains(got, "/Users/alice") {
		t.Errorf("wrap output leaked the homedir prefix; users would see the daemon owner's username in the issue timeline\nfull: %s", got)
	}
	if !strings.Contains(got, "grok") {
		t.Errorf("wrap output dropped the binary basename; users need at least the command name to debug\nfull: %s", got)
	}
	for _, want := range []string{
		"Custom runtime profile is incompatible",
		"cursor protocol family",
		"cursor-compatible launch arguments",
		"cursor-compatible output",
		"first-class provider",
		"Original error: exit status 2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("wrap output missing %q\nfull: %s", want, got)
		}
	}
}

// TestWrapCustomProfileExecError_Defaults guards the empty-input edges so a
// backend that returns an empty Result.Error or a misconfigured daemon with
// an empty command path still produces a readable comment.
func TestWrapCustomProfileExecError_Defaults(t *testing.T) {
	t.Parallel()

	got := wrapCustomProfileExecError("claude", "", "")

	if !strings.Contains(got, "the configured command") {
		t.Errorf("empty command path should fall back to placeholder, got: %s", got)
	}
	if !strings.Contains(got, "no error detail captured") {
		t.Errorf("empty raw error should fall back to placeholder, got: %s", got)
	}
}

// TestShouldRewriteAsCustomProfileIncompatible exhaustively pins the
// narrow-rewrite policy from review point #2. The predicate is the only
// thing standing between "user sees the real auth/quota/network error"
// and "user gets sent on a wild goose chase looking for a non-existent
// protocol mismatch". A drift here would also re-bucket failure_reason
// rows that the analytics dashboards key off, so the test enumerates
// every reason in the canonical set rather than spot-checking.
func TestShouldRewriteAsCustomProfileIncompatible(t *testing.T) {
	t.Parallel()

	rewriteAllowed := map[taskfailure.Reason]bool{
		taskfailure.ReasonAgentProcessFailure:            true,
		taskfailure.ReasonAgentEmptyOrUnparseableOutput:  true,
		taskfailure.ReasonAgentUnknown:                   true,
	}

	for _, reason := range taskfailure.AllReasons() {
		want := rewriteAllowed[reason]
		got := shouldRewriteAsCustomProfileIncompatible(reason)
		if got != want {
			t.Errorf("shouldRewriteAsCustomProfileIncompatible(%s) = %v, want %v\nProtocol-shape failures (process_failure / empty_or_unparseable_output / unknown) MUST be rewritten as runtime_version_unsupported. Every other reason — auth, quota, network, server, capacity, context_overflow, missing_config, missing_executable, model_not_found, runtime_version_unsupported (already correct), poisoned API 400, platform-side reasons — MUST pass through unchanged so the user sees the real error and analytics dashboards don't drift.",
				reason, got, want)
		}
	}
}

// TestAppendCustomProfileSilenceHint covers review point #3: the hint that
// gets appended to timeout / idle_watchdog comments when a custom-profile
// runtime hangs before establishing a session. Verifies the hint preserves
// the base comment, mentions only the binary basename (not the absolute
// path), and points the user at the family-compatibility check.
func TestAppendCustomProfileSilenceHint(t *testing.T) {
	t.Parallel()

	base := "claude timed out after 30m0s"
	got := appendCustomProfileSilenceHint("claude", "/opt/homebrew/bin/droid", base)

	if !strings.HasPrefix(got, base) {
		t.Errorf("hint must preserve the original timeout comment as a prefix\nfull: %s", got)
	}
	if strings.Contains(got, "/opt/homebrew/bin/droid") {
		t.Errorf("hint leaked the absolute command path: %s", got)
	}
	if !strings.Contains(got, "droid") {
		t.Errorf("hint should still name the binary (basename) so users know which command to check: %s", got)
	}
	for _, want := range []string{
		"custom runtime profile",
		"claude protocol family",
		"first-class provider",
		"watchdog fired",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("hint missing %q\nfull: %s", want, got)
		}
	}

	// Empty base comment: hint stands alone, no leading newlines.
	standalone := appendCustomProfileSilenceHint("claude", "droid", "")
	if strings.HasPrefix(standalone, "\n") {
		t.Errorf("standalone hint should not start with a newline: %q", standalone)
	}
}

// fakeAgentBackend is a stub agent.Backend that returns a canned
// agent.Result on Execute. Used to drive runTask end-to-end through the
// post-executeAndDrain switch (timeout / idle_watchdog / failed-result
// branches) without spawning a real CLI.
type fakeAgentBackend struct {
	result    agent.Result
	executeOK bool
	execErr   error
}

func (b *fakeAgentBackend) Execute(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	if !b.executeOK {
		return nil, b.execErr
	}
	msgCh := make(chan agent.Message)
	resCh := make(chan agent.Result, 1)
	close(msgCh)
	resCh <- b.result
	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

// stubAgentNew swaps the package-level agentNew hook so runTask uses the
// supplied backend instead of spawning a real CLI. The cleanup restores the
// production agent.New so test order is irrelevant.
func stubAgentNew(t *testing.T, backend agent.Backend) {
	t.Helper()
	orig := agentNew
	agentNew = func(_ string, _ agent.Config) (agent.Backend, error) {
		return backend, nil
	}
	t.Cleanup(func() { agentNew = orig })
}

// runTaskFixture builds the minimum daemon state (with a custom runtime
// profile registered or not) for a runTask end-to-end test. Returns the
// daemon and the canonical task to feed into runTask.
type runTaskFixture struct {
	d              *Daemon
	task           Task
	customCmdPath  string
}

func newCustomProfileFixture(t *testing.T) *runTaskFixture {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	customCmd := filepath.Join(t.TempDir(), "custom-cli")
	d := freshDaemon(srv.URL)
	d.cfg = Config{
		WorkspacesRoot: t.TempDir(),
		Agents: map[string]AgentEntry{
			"cursor": {Path: "/should/be/overridden"},
		},
		// Tight timeout values aren't required for stub-backed tests; runTask
		// only formats them into messages when the backend reports timeout.
		AgentTimeout: 30,
	}
	d.runtimeIndex["rt-custom"] = Runtime{
		ID:        "rt-custom",
		Provider:  "cursor",
		ProfileID: "prof-custom",
	}
	d.profileCommandPaths = map[string]string{"prof-custom": customCmd}

	task := Task{
		ID:          "task-mul-3414",
		WorkspaceID: "ws-mul-3414",
		RuntimeID:   "rt-custom",
		IssueID:     "issue-mul-3414",
		AuthToken:   "mat_test_token",
		Agent:       &AgentData{Name: "test-agent"},
	}
	return &runTaskFixture{d: d, task: task, customCmdPath: customCmd}
}

// TestRunTask_CustomProfileExecError_RewritesTaskResult is the regression
// guard for the original MUL-3414 grok-under-cursor shape: a backend
// Execute error whose classifier reason is process_failure /
// empty_or_unparseable_output / unknown gets rewritten to
// runtime_version_unsupported, with a redacted user-visible comment.
func TestRunTask_CustomProfileExecError_RewritesTaskResult(t *testing.T) {
	// Not parallel: stubAgentNew swaps a package-level hook and parallel
	// runTask integration tests would interleave each other's stubs.

	fx := newCustomProfileFixture(t)
	stubAgentNew(t, &fakeAgentBackend{
		executeOK: false,
		// "exit status 2" classifies as ReasonAgentProcessFailure — the
		// shape grok produces when cursor.go feeds it --output-format
		// stream-json and grok rejects the value.
		execErr: errors.New("cursor-agent exited with error: exit status 2"),
	})

	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := fx.d.runTask(context.Background(), fx.task, "cursor", 0, taskLog)
	if err != nil {
		t.Fatalf("runTask returned a hard error; the custom-profile branch must convert exec failures into a blocked TaskResult so SessionID/WorkDir flow through: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("expected status=blocked, got %q (full result: %+v)", result.Status, result)
	}
	if result.FailureReason != taskfailure.ReasonAgentRuntimeVersionUnsupported.String() {
		t.Errorf("expected failure_reason=%s, got %q",
			taskfailure.ReasonAgentRuntimeVersionUnsupported, result.FailureReason)
	}
	if !strings.Contains(result.Comment, "Custom runtime profile is incompatible") {
		t.Errorf("comment missing custom-profile copy: %s", result.Comment)
	}
	if strings.Contains(result.Comment, fx.customCmdPath) {
		t.Errorf("comment leaked absolute command path %q\nfull: %s", fx.customCmdPath, result.Comment)
	}
	if !strings.Contains(result.Comment, filepath.Base(fx.customCmdPath)) {
		t.Errorf("comment dropped binary basename: %s", result.Comment)
	}
}

// TestRunTask_CustomProfileExecError_AuthErrorPassesThrough is the review
// point #2 regression guard. A custom profile that hits an auth failure
// (e.g. the same-protocol wrapper passes through to the upstream CLI which
// returns 401 because the user's token expired) must NOT be rewritten as
// "incompatible with the protocol family" — that would send the user
// looking for a phantom protocol issue and skew failure analytics.
func TestRunTask_CustomProfileExecError_AuthErrorPassesThrough(t *testing.T) {
	// Not parallel: stubAgentNew swaps a package-level hook.

	fx := newCustomProfileFixture(t)
	stubAgentNew(t, &fakeAgentBackend{
		executeOK: false,
		// Classifier maps "401 unauthorized" to ReasonAgentProviderAuthOrAccess
		// (see classify.go rule 3). A real same-protocol wrapper can produce
		// this verbatim because it just shells out to the upstream CLI.
		execErr: errors.New("API Error: 401 unauthorized — invalid api key"),
	})

	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := fx.d.runTask(context.Background(), fx.task, "cursor", 0, taskLog)

	// Non-protocol-shape failures keep the original (TaskResult{}, err)
	// contract so handleTask classifies via FailTask exactly as it would
	// for a built-in runtime.
	if err == nil {
		t.Fatalf("auth failure must propagate as an error so handleTask runs the canonical FailTask classifier; got TaskResult instead: %+v", result)
	}
	if !strings.Contains(err.Error(), "401 unauthorized") {
		t.Errorf("error must preserve the original 401 wording so handleTask's classifier sees the real shape: %v", err)
	}
	if strings.Contains(err.Error(), "Custom runtime profile is incompatible") {
		t.Errorf("auth error must not be rewritten as incompatible; got: %v", err)
	}
}

// TestRunTask_CustomProfileFailedResult_AuthErrorPreservesReason mirrors
// the auth-passthrough guard above for the default-status branch (where
// the backend completes with Status=failed and the auth error is in
// Result.Error rather than the Execute return). Same policy: the
// classifier reason must survive untouched.
func TestRunTask_CustomProfileFailedResult_AuthErrorPreservesReason(t *testing.T) {
	// Not parallel: stubAgentNew swaps a package-level hook.

	fx := newCustomProfileFixture(t)
	stubAgentNew(t, &fakeAgentBackend{
		executeOK: true,
		result: agent.Result{
			Status: "failed",
			Error:  "Anthropic API: 401 unauthorized — please login again",
		},
	})

	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := fx.d.runTask(context.Background(), fx.task, "cursor", 0, taskLog)
	if err != nil {
		t.Fatalf("runTask: unexpected hard error: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("expected status=blocked, got %q", result.Status)
	}
	if result.FailureReason != taskfailure.ReasonAgentProviderAuthOrAccess.String() {
		t.Errorf("expected failure_reason=%s (the real classifier output), got %q — auth errors must not be rewritten as runtime_version_unsupported on custom profiles",
			taskfailure.ReasonAgentProviderAuthOrAccess, result.FailureReason)
	}
	if strings.Contains(result.Comment, "Custom runtime profile is incompatible") {
		t.Errorf("comment must not be rewritten as incompatible for an auth failure; users need to see the real 401: %s", result.Comment)
	}
	if !strings.Contains(result.Comment, "401 unauthorized") {
		t.Errorf("comment must preserve the original error so the user can debug: %s", result.Comment)
	}
}

// TestRunTask_CustomProfileFailedResult_ProtocolShapeRewrites pins the
// positive case for the default-status branch: when the agent emits
// "returned no parseable output" (the droid-under-claude shape), the
// classifier maps that to empty_or_unparseable_output, which IS a
// protocol-shape failure → rewrite to runtime_version_unsupported.
func TestRunTask_CustomProfileFailedResult_ProtocolShapeRewrites(t *testing.T) {
	// Not parallel: stubAgentNew swaps a package-level hook.

	fx := newCustomProfileFixture(t)
	stubAgentNew(t, &fakeAgentBackend{
		executeOK: true,
		result: agent.Result{
			Status: "failed",
			Error:  "claude returned no parseable output",
		},
	})

	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := fx.d.runTask(context.Background(), fx.task, "cursor", 0, taskLog)
	if err != nil {
		t.Fatalf("runTask: unexpected hard error: %v", err)
	}
	if result.FailureReason != taskfailure.ReasonAgentRuntimeVersionUnsupported.String() {
		t.Errorf("expected failure_reason=%s, got %q",
			taskfailure.ReasonAgentRuntimeVersionUnsupported, result.FailureReason)
	}
	if !strings.Contains(result.Comment, "Custom runtime profile is incompatible") {
		t.Errorf("comment missing custom-profile copy: %s", result.Comment)
	}
}

// TestRunTask_CustomProfileTimeout_NoSession_AppendsHint is review point
// #3's positive case: a timeout on a custom-profile runtime that never
// established a session (the droid-under-claude shape) must surface the
// compatibility hint in the user-visible comment, while keeping
// failure_reason=timeout for the operator dashboards.
func TestRunTask_CustomProfileTimeout_NoSession_AppendsHint(t *testing.T) {
	// Not parallel: stubAgentNew swaps a package-level hook.

	fx := newCustomProfileFixture(t)
	stubAgentNew(t, &fakeAgentBackend{
		executeOK: true,
		result: agent.Result{
			Status:    "timeout",
			Error:     "claude timed out after 30m0s",
			SessionID: "", // no session = strong signal the protocol never engaged
		},
	})

	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := fx.d.runTask(context.Background(), fx.task, "cursor", 0, taskLog)
	if err != nil {
		t.Fatalf("runTask: unexpected hard error: %v", err)
	}
	if result.FailureReason != "timeout" {
		t.Errorf("failure_reason must stay timeout (platform-side reason for runtime sweepers), got %q",
			result.FailureReason)
	}
	if !strings.Contains(result.Comment, "claude timed out") {
		t.Errorf("base timeout message must be preserved: %s", result.Comment)
	}
	if !strings.Contains(result.Comment, "custom runtime profile") {
		t.Errorf("comment must include the custom-profile compatibility hint: %s", result.Comment)
	}
	if strings.Contains(result.Comment, fx.customCmdPath) {
		t.Errorf("hint leaked absolute command path: %s", result.Comment)
	}
}

// TestRunTask_CustomProfileTimeout_WithSession_NoHint is the negative
// case: a timeout that DID establish a session is most likely a
// legitimate hang (long tool call against a frozen child), not a
// protocol-mismatch shape. The hint would be misleading, so it must NOT
// be appended.
func TestRunTask_CustomProfileTimeout_WithSession_NoHint(t *testing.T) {
	// Not parallel: stubAgentNew swaps a package-level hook.

	fx := newCustomProfileFixture(t)
	stubAgentNew(t, &fakeAgentBackend{
		executeOK: true,
		result: agent.Result{
			Status:    "timeout",
			Error:     "claude timed out after 30m0s",
			SessionID: "sess-existed", // session was established → protocol DID engage
		},
	})

	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := fx.d.runTask(context.Background(), fx.task, "cursor", 0, taskLog)
	if err != nil {
		t.Fatalf("runTask: unexpected hard error: %v", err)
	}
	if strings.Contains(result.Comment, "custom runtime profile") {
		t.Errorf("hint must NOT fire when a session was established (legitimate hang, not protocol mismatch): %s", result.Comment)
	}
}

// TestRunTask_CustomProfileIdleWatchdog_NoSession_AppendsHint mirrors the
// timeout case for the idle_watchdog branch.
func TestRunTask_CustomProfileIdleWatchdog_NoSession_AppendsHint(t *testing.T) {
	// Not parallel: stubAgentNew swaps a package-level hook.

	fx := newCustomProfileFixture(t)
	stubAgentNew(t, &fakeAgentBackend{
		executeOK: true,
		result: agent.Result{
			Status:    "idle_watchdog",
			Error:     "agent went silent for longer than 5m0s",
			SessionID: "",
		},
	})

	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := fx.d.runTask(context.Background(), fx.task, "cursor", 0, taskLog)
	if err != nil {
		t.Fatalf("runTask: unexpected hard error: %v", err)
	}
	if result.FailureReason != "idle_watchdog" {
		t.Errorf("failure_reason must stay idle_watchdog, got %q", result.FailureReason)
	}
	if !strings.Contains(result.Comment, "custom runtime profile") {
		t.Errorf("comment must include the custom-profile compatibility hint: %s", result.Comment)
	}
}

// TestRunTask_BuiltInExecError_StaysOnLegacyClassifierPath guards the
// inverse: a built-in (non-custom) runtime that fails with a process_failure
// shape must stay on the existing classifier path. Without this guard, a
// future change that forgets to gate the rewrite on isCustomProfile would
// silently re-bucket every built-in failure too.
func TestRunTask_BuiltInExecError_StaysOnLegacyClassifierPath(t *testing.T) {
	// Not parallel: stubAgentNew swaps a package-level hook.

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := freshDaemon(srv.URL)
	d.cfg = Config{
		WorkspacesRoot: t.TempDir(),
		Agents: map[string]AgentEntry{
			"cursor": {Path: "/usr/local/bin/cursor"},
		},
	}
	d.runtimeIndex["rt-builtin"] = Runtime{ID: "rt-builtin", Provider: "cursor"}

	stubAgentNew(t, &fakeAgentBackend{
		executeOK: false,
		execErr:   errors.New("cursor-agent exited with error: exit status 2"),
	})

	task := Task{
		ID:          "task-builtin-fail",
		WorkspaceID: "ws-builtin-fail",
		RuntimeID:   "rt-builtin",
		IssueID:     "issue-builtin-fail",
		AuthToken:   "mat_test_token",
		Agent:       &AgentData{Name: "test-agent"},
	}

	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := d.runTask(context.Background(), task, "cursor", 0, taskLog)

	// Built-in path must propagate the raw error so handleTask runs the
	// canonical FailTask classifier — the rewrite must be gated on
	// isCustomProfile.
	if err == nil {
		t.Fatalf("built-in runtime exec failure must propagate as an error, not a TaskResult: %+v", result)
	}
	if strings.Contains(err.Error(), "Custom runtime profile is incompatible") {
		t.Fatalf("built-in runtime exec failure picked up the custom-profile copy: %v", err)
	}
}

// Compile-time assertions: the wrap helper must use the canonical taxonomy
// constants rather than hard-coded strings. If any const is renamed the
// failing build is the test we want, not a silent label drift.
var _ = taskfailure.ReasonAgentRuntimeVersionUnsupported
var _ = taskfailure.ReasonAgentProcessFailure
var _ = taskfailure.ReasonAgentEmptyOrUnparseableOutput
var _ = taskfailure.ReasonAgentUnknown
var _ = taskfailure.ReasonAgentProviderAuthOrAccess
var _ = atomic.Int32{} // keep sync/atomic import wired even if future edits drop the only use
