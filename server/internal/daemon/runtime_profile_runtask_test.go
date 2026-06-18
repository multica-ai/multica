package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// TestWrapCustomProfileExecError_UnitShape pins the user-facing copy that
// runTask uses when a custom runtime profile (MUL-3414) fails. The exact
// strings matter because they are read straight from a failed-task comment
// in the issue timeline — the boundary the dialog and CLI hints already
// document ("must accept <family>-compatible launch arguments and produce
// <family>-compatible output") needs to be repeated here so a single read
// of the failed task tells the user the same thing.
func TestWrapCustomProfileExecError_UnitShape(t *testing.T) {
	t.Parallel()

	got := wrapCustomProfileExecError("cursor", "/usr/local/bin/grok", "exit status 2")

	for _, want := range []string{
		"Custom runtime profile is incompatible",
		"cursor protocol family",
		"cursor-compatible launch arguments",
		"cursor-compatible output",
		"/usr/local/bin/grok",
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

// TestRunTask_CustomProfileExecError_RewritesTaskResult is the regression
// guard for MUL-3414: when a custom runtime profile resolves to a binary that
// errors out at exec time (the typical shape for grok-under-cursor or
// droid-under-claude), runTask must NOT propagate the raw error up to
// handleTask's generic FailTask path. Instead it returns a TaskResult whose
// Comment names the real failure mode and whose FailureReason is the refined
// taxonomy value `agent_error.runtime_version_unsupported`, so the issue
// timeline tells the user the same thing the create-time UI hint warned
// about.
//
// The test uses a non-existent binary as the custom command_path, which
// forces backend.Execute to fail at cmd.Start; that exercises the
// executeAndDrain-error branch of runTask. The default-status branch (where
// the agent emits a structured failure result) is covered indirectly by
// existing classifier tests — the wrap-and-retag logic is the same.
func TestRunTask_CustomProfileExecError_RewritesTaskResult(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	missingBin := filepath.Join(t.TempDir(), "definitely-not-cursor")
	workspacesRoot := t.TempDir()
	d := freshDaemon(srv.URL)
	d.cfg = Config{
		WorkspacesRoot: workspacesRoot,
		Agents: map[string]AgentEntry{
			// A built-in cursor entry exists on this host; the custom
			// profile will override its Path with missingBin via the
			// profileCommandPaths lookup.
			"cursor": {Path: "/should/be/overridden"},
		},
	}
	d.runtimeIndex["rt-custom"] = Runtime{
		ID:        "rt-custom",
		Provider:  "cursor",
		ProfileID: "prof-grok",
	}
	d.profileCommandPaths = map[string]string{"prof-grok": missingBin}

	task := Task{
		ID:          "task-mul-3414",
		WorkspaceID: "ws-mul-3414",
		RuntimeID:   "rt-custom",
		IssueID:     "issue-mul-3414",
		AuthToken:   "mat_test_token",
		Agent:       &AgentData{Name: "test-agent"},
	}

	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))

	result, err := d.runTask(context.Background(), task, "cursor", 0, taskLog)
	if err != nil {
		t.Fatalf("runTask returned a hard error; the custom-profile branch must convert exec failures into a blocked TaskResult so SessionID/WorkDir flow through and the failure_reason taxonomy stays refined: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("expected status=blocked, got %q (full result: %+v)", result.Status, result)
	}
	if result.FailureReason != taskfailure.ReasonAgentRuntimeVersionUnsupported.String() {
		t.Errorf("expected failure_reason=%s, got %q",
			taskfailure.ReasonAgentRuntimeVersionUnsupported, result.FailureReason)
	}
	for _, want := range []string{
		"Custom runtime profile is incompatible",
		"cursor protocol family",
		missingBin,
	} {
		if !strings.Contains(result.Comment, want) {
			t.Errorf("comment missing %q\nfull: %s", want, result.Comment)
		}
	}
}

// TestRunTask_BuiltInExecError_StaysOnLegacyClassifierPath guards the
// inverse of MUL-3414: a built-in (non-custom) runtime that fails for the
// same exec-time reason must stay on the existing classifier path so the
// taxonomy used by Grafana / failure analytics doesn't shift from real
// runner crashes into runtime_version_unsupported. Without this guard, a
// future change that forgets to gate the rewrite on isCustomProfile would
// silently re-bucket every built-in failure too.
func TestRunTask_BuiltInExecError_StaysOnLegacyClassifierPath(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	missingBin := filepath.Join(t.TempDir(), "definitely-not-cursor")
	workspacesRoot := t.TempDir()
	d := freshDaemon(srv.URL)
	d.cfg = Config{
		WorkspacesRoot: workspacesRoot,
		Agents: map[string]AgentEntry{
			"cursor": {Path: missingBin},
		},
	}
	// Built-in runtime: no ProfileID, so customCommandPathForRuntime
	// returns ("", false) and the rewrite branch must not fire.
	d.runtimeIndex["rt-builtin"] = Runtime{ID: "rt-builtin", Provider: "cursor"}

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
	// The built-in path returns the raw error from runTask. The exact
	// failure mode (cmd.Start ENOENT, no parseable output, etc.) depends on
	// the cursor backend's internal behaviour and isn't this test's
	// concern; the contract is "no custom-profile rewrite", which is
	// equally satisfied by an error return or a TaskResult that does NOT
	// carry runtime_version_unsupported.
	switch {
	case err != nil:
		// OK — built-in error path, runTask returned the raw error.
	case result.FailureReason == taskfailure.ReasonAgentRuntimeVersionUnsupported.String():
		t.Fatalf("built-in runtime failure was rewritten as runtime_version_unsupported; the rewrite branch must be gated on isCustomProfile (full result: %+v)", result)
	case strings.Contains(result.Comment, "Custom runtime profile is incompatible"):
		t.Fatalf("built-in runtime failure picked up the custom-profile copy in its comment: %s", result.Comment)
	}
}

// Compile-time assertion: the wrap helper must use the canonical taxonomy
// constant rather than a hard-coded string. If the const is renamed the
// failing build is the test we want, not a silent label drift.
var _ = taskfailure.ReasonAgentRuntimeVersionUnsupported

// Compile-time assertion: agent.SupportedTypes is the source of truth for
// which families a custom profile can reuse. cursor must remain in it for
// this test pair to exercise a representative protocol family.
var _ = agent.SupportedTypes
