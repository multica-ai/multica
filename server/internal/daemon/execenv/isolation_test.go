package execenv

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"
)

const preparationHelperTestMode = "execenv-preparation-helper"

func preparationHelperTestCommand() []string {
	return []string{
		os.Args[0],
		"-test.run=^TestPreparationHelperProcess$",
		"--",
		preparationHelperTestMode,
	}
}

// TestPreparationHelperProcess is both a no-op parent-side test and the child
// entry point used by isolation tests. Keeping it in the package test binary
// exercises the same stdin/stdout protocol as the real multica helper.
func TestPreparationHelperProcess(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != preparationHelperTestMode {
		return
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := RunPreparationHelper(os.Stdin, os.Stdout, logger); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestPreparationHelperRoundTripsReuse(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	params := PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-helper-reuse",
		TaskID:         "99999999-8888-7777-6666-555555555555",
		Provider:       "claude",
		Task:           TaskContextForEnv{IssueID: "issue-helper-reuse"},
	}
	env, err := PrepareIsolated(ctx, preparationHelperTestCommand(), params, logger)
	if err != nil {
		t.Fatalf("PrepareIsolated: %v", err)
	}
	reused, err := ReuseIsolated(ctx, preparationHelperTestCommand(), ReuseParams{
		WorkspacesRoot: params.WorkspacesRoot,
		WorkDir:        env.WorkDir,
		Provider:       params.Provider,
		Task:           TaskContextForEnv{IssueID: "issue-helper-reuse", NewCommentCount: 1},
	}, logger)
	if err != nil {
		t.Fatalf("ReuseIsolated: %v", err)
	}
	if reused == nil || reused.RootDir != env.RootDir || reused.WorkDir != env.WorkDir {
		t.Fatalf("reused environment = %#v, want root %q workdir %q", reused, env.RootDir, env.WorkDir)
	}
}
