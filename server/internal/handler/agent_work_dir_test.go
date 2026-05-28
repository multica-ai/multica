package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
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
		taskID = "5c57b65b-ee7a-4603-a72d-a548b2390cb2"
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
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/5c57b65b/workdir",
			wsID:     wsID,
			taskID:   taskID,
			expected: wsID + "/5c57b65b/workdir",
		},
		{
			name:     "standard envRoot path without trailing workdir",
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/5c57b65b",
			wsID:     wsID,
			taskID:   taskID,
			expected: wsID + "/5c57b65b",
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
			workDir:  `C:\Users\alice\multica_workspaces\` + wsID + `\5c57b65b\workdir`,
			wsID:     wsID,
			taskID:   taskID,
			expected: wsID + "/5c57b65b/workdir",
		},
		{
			name:     "missing workspace_id under home strips home prefix instead of envRoot",
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/5c57b65b/workdir",
			wsID:     "",
			taskID:   taskID,
			expected: "multica_workspaces/" + wsID + "/5c57b65b/workdir",
		},
		{
			name:     "missing task_id under home strips home prefix instead of envRoot",
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/5c57b65b/workdir",
			wsID:     wsID,
			taskID:   "",
			expected: "multica_workspaces/" + wsID + "/5c57b65b/workdir",
		},
		{
			name:     "trailing slash on envRoot path is preserved in returned suffix",
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/5c57b65b/workdir/",
			wsID:     wsID,
			taskID:   taskID,
			expected: wsID + "/5c57b65b/workdir/",
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

// TestShortTaskIDMatchesDaemon pins shortTaskID() to execenv.PredictRootDir's
// path layout. Both helpers consume the same task UUID; if the daemon's
// shortID logic drifts, this test trips loudly instead of letting the UI
// silently fall back to the "tail two segments" branch. Without this guard,
// a daemon-side change to, say, a 12-char prefix would not break a build —
// it would just quietly degrade every standard-task work_dir chip into the
// local_directory fallback.
func TestShortTaskIDMatchesDaemon(t *testing.T) {
	const (
		workspacesRoot = "/tmp/workspaces"
		workspaceID    = "a05b0e10-ee7a-4603-a72d-a548b2390cb2"
		taskID         = "5c57b65b-ee7a-4603-a72d-a548b2390cb2"
	)
	daemonRoot := execenv.PredictRootDir(workspacesRoot, workspaceID, taskID)
	expected := workspacesRoot + "/" + workspaceID + "/" + shortTaskID(taskID)
	if daemonRoot != expected {
		t.Fatalf("daemon PredictRootDir = %q, handler-side reconstruction = %q — shortTaskID is out of sync with execenv.shortID", daemonRoot, expected)
	}
}
