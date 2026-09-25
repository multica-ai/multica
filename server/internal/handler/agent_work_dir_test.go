package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestRelativeWorkDir covers the privacy-safe display derivation that
// agent-transcript dialogs render in the work_dir chip. Two regression
// concerns drive the table:
//
//  1. Standard tasks must strip the daemon's workspaces root so the chip
//     doesn't expose the user's home directory or username (the bug in
//     PR #3379 that this fix replaces).
//  2. local_directory tasks have a work_dir outside the envRoot layout —
//     we must NOT leak `/Users/<name>/...`, `/home/<name>/...`, or
//     `<drive>:/Users/<name>/...` even on shallow paths like
//     `/Users/alice/foo`. The function strips recognised home prefixes
//     and otherwise falls back to the basename, which can never carry a
//     username segment.
func TestRelativeWorkDir(t *testing.T) {
	const (
		wsID   = "a05b0e10-ee7a-4603-a72d-a548b2390cb2"
		taskID = "5c57b65b-ee7a-4603-a72d-b659c34a1dc3"
		// The env-root segment is the TAIL of the task id, not a leading
		// prefix: UUIDv7 puts a timestamp in front, so a leading slice is
		// shared by every task created in the same ~65.5s window (#7326).
		wsSeg   = "a548b2390cb2"
		taskSeg = "b659c34a1dc3"
	)

	tests := []struct {
		name     string
		workDir  string
		wsID     string
		taskID   string
		expected string
	}{
		{
			name:     "empty work_dir returns empty",
			workDir:  "",
			wsID:     wsID,
			taskID:   taskID,
			expected: "",
		},
		{
			name:     "standard envRoot path strips workspaces root",
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/" + taskSeg + "/workdir",
			wsID:     wsID,
			taskID:   taskID,
			expected: wsID + "/" + taskSeg + "/workdir",
		},
		{
			name:     "standard envRoot path without trailing workdir",
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/" + taskSeg,
			wsID:     wsID,
			taskID:   taskID,
			expected: wsID + "/" + taskSeg,
		},
		{
			name:     "readable envRoot path strips workspaces root",
			workDir:  "/Users/alice/multica_workspaces/asset-feed-" + wsSeg + "/mul-6063-" + taskSeg + "/workdir",
			wsID:     wsID,
			taskID:   taskID,
			expected: "asset-feed-" + wsSeg + "/mul-6063-" + taskSeg + "/workdir",
		},
		{
			name:     "legacy readable envRoot keeps privacy-safe display",
			workDir:  "/Users/alice/multica_workspaces/asset-feed-a05b0e10/mul-6063-5c57b65b/workdir",
			wsID:     wsID,
			taskID:   taskID,
			expected: "asset-feed-a05b0e10/mul-6063-5c57b65b/workdir",
		},
		{
			name:     "legacy opaque envRoot keeps privacy-safe display",
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/5c57b65b/workdir",
			wsID:     wsID,
			taskID:   taskID,
			expected: wsID + "/5c57b65b/workdir",
		},
		{
			name:     "local_directory path under /Users home is stripped",
			workDir:  "/Users/df007df/repos/foo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "repos/foo",
		},
		{
			name:     "local_directory deep path under home keeps full remainder",
			workDir:  "/Users/df007df/code/work/projects/multica/foo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "code/work/projects/multica/foo",
		},
		{
			name:     "shallow /Users home path strips username segment",
			workDir:  "/Users/alice/foo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "foo",
		},
		{
			name:     "shallow Linux /home path strips username segment",
			workDir:  "/home/alice/project",
			wsID:     wsID,
			taskID:   taskID,
			expected: "project",
		},
		{
			name:     "shallow Windows /Users path strips username segment",
			workDir:  `C:\Users\alice\foo`,
			wsID:     wsID,
			taskID:   taskID,
			expected: "foo",
		},
		{
			name:     "exact home directory returns empty (would only render username)",
			workDir:  "/Users/alice",
			wsID:     wsID,
			taskID:   taskID,
			expected: "",
		},
		{
			name:     "exact home directory with trailing slash returns empty",
			workDir:  "/Users/alice/",
			wsID:     wsID,
			taskID:   taskID,
			expected: "",
		},
		{
			name:     "Windows local_directory path under home strips username",
			workDir:  `C:\Users\alice\repos\foo`,
			wsID:     wsID,
			taskID:   taskID,
			expected: "repos/foo",
		},
		{
			name:     "non-home local path falls back to basename only",
			workDir:  "/opt/foo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "foo",
		},
		{
			name:     "non-home deep local path falls back to basename only",
			workDir:  "/srv/git/repo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "repo",
		},
		{
			name:     "single-segment local path returns the segment",
			workDir:  "/foo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "foo",
		},
		{
			name:     "Windows backslash separators are normalized",
			workDir:  `C:\Users\alice\multica_workspaces\` + wsID + `\` + taskSeg + `\workdir`,
			wsID:     wsID,
			taskID:   taskID,
			expected: wsID + "/" + taskSeg + "/workdir",
		},
		{
			name:     "missing workspace_id under home strips home prefix instead of envRoot",
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/" + taskSeg + "/workdir",
			wsID:     "",
			taskID:   taskID,
			expected: "multica_workspaces/" + wsID + "/" + taskSeg + "/workdir",
		},
		{
			name:     "missing task_id under home strips home prefix instead of envRoot",
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/" + taskSeg + "/workdir",
			wsID:     wsID,
			taskID:   "",
			expected: "multica_workspaces/" + wsID + "/" + taskSeg + "/workdir",
		},
		{
			name:     "trailing slash on envRoot path is preserved in returned suffix",
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/" + taskSeg + "/workdir/",
			wsID:     wsID,
			taskID:   taskID,
			expected: wsID + "/" + taskSeg + "/workdir/",
		},
		{
			name:     "wsID prefix appearing elsewhere falls back to basename when not under home",
			workDir:  "/var/" + wsID + "/something/else",
			wsID:     wsID,
			taskID:   taskID,
			expected: "else",
		},
		{
			name:     "case-insensitive /users matches the same as /Users",
			workDir:  "/users/alice/repos/foo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "repos/foo",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := relativeWorkDir(tc.workDir, tc.wsID, tc.taskID)
			if got != tc.expected {
				t.Fatalf("relativeWorkDir(%q, %q, %q) = %q, want %q",
					tc.workDir, tc.wsID, tc.taskID, got, tc.expected)
			}
		})
	}
}

func TestTaskToResponseDerivesPrivateDurableWorkDir(t *testing.T) {
	response := taskToResponse(db.AgentTaskQueue{
		DurableWorkDir: pgtype.Text{
			String: "/Users/alice/repos/multica",
			Valid:  true,
		},
	}, "")

	if response.DurableWorkDir != "/Users/alice/repos/multica" {
		t.Fatalf("durable_work_dir = %q, want absolute clipboard value", response.DurableWorkDir)
	}
	if response.RelativeDurableWorkDir != "repos/multica" {
		t.Fatalf("relative_durable_work_dir = %q, want privacy-safe display value", response.RelativeDurableWorkDir)
	}
}

// TestStableIDSuffixMatchesDaemon pins the handler's path validation to the
// current readable suffixes used by execenv.PredictRootDir. The table above
// separately pins compatibility with both historical layouts.
func TestStableIDSuffixMatchesDaemon(t *testing.T) {
	const (
		workspacesRoot = "/tmp/workspaces"
		workspaceID    = "a05b0e10-ee7a-4603-a72d-a548b2390cb2"
		taskID         = "5c57b65b-ee7a-4603-a72d-b659c34a1dc3"
	)
	daemonRoot := execenv.PredictRootDir(execenv.RootDirParams{
		WorkspacesRoot:  workspacesRoot,
		WorkspaceID:     workspaceID,
		WorkspaceSlug:   "asset-feed",
		TaskID:          taskID,
		IssueIdentifier: "MUL-6063",
	})
	expected := workspacesRoot + "/asset-feed-" + taskDirSegment(workspaceID) + "/mul-6063-" + taskDirSegment(taskID)
	if daemonRoot != expected {
		t.Fatalf("daemon PredictRootDir = %q, handler-side suffix expectation = %q", daemonRoot, expected)
	}
}
