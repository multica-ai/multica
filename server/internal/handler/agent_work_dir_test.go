package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// TestRelativeWorkDir covers the privacy-safe display derivation that
// agent-transcript dialogs and the issue "copy workdir" submenu render. The
// derivation prefers a unified home-relative form (`~/<remainder>`) so that
// managed and local_directory tasks share one shape:
//
//  1. Any path under a recognised home layout (`/Users/<name>/...`,
//     `/home/<name>/...`, `<drive>:/Users/<name>/...`) renders as
//     `~/<remainder>` — the username segment is replaced by `~`, never leaked.
//  2. A path NOT under a recognised home (workspacesRoot under `/opt`, `/srv`,
//     a network mount) falls back to the envRoot suffix
//     `<wsUUID>/<taskShort>/workdir` for managed tasks, otherwise the basename.
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
			name:     "managed task under home renders ~/ with full workspace path",
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/5c57b65b/workdir",
			wsID:     wsID,
			taskID:   taskID,
			expected: "~/multica_workspaces/" + wsID + "/5c57b65b/workdir",
		},
		{
			name:     "managed task under home without trailing workdir",
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/5c57b65b",
			wsID:     wsID,
			taskID:   taskID,
			expected: "~/multica_workspaces/" + wsID + "/5c57b65b",
		},
		{
			name:     "local_directory path under /Users home renders ~/",
			workDir:  "/Users/df007df/repos/foo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "~/repos/foo",
		},
		{
			name:     "local_directory deep path under home keeps full remainder under ~/",
			workDir:  "/Users/df007df/code/work/projects/multica/foo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "~/code/work/projects/multica/foo",
		},
		{
			name:     "shallow /Users home path renders ~/<leaf>",
			workDir:  "/Users/alice/foo",
			wsID:     wsID,
			taskID:   taskID,
			expected: "~/foo",
		},
		{
			name:     "shallow Linux /home path renders ~/<leaf>",
			workDir:  "/home/alice/project",
			wsID:     wsID,
			taskID:   taskID,
			expected: "~/project",
		},
		{
			name:     "shallow Windows /Users path renders ~/<leaf>",
			workDir:  `C:\Users\alice\foo`,
			wsID:     wsID,
			taskID:   taskID,
			expected: "~/foo",
		},
		{
			name:     "exact home directory renders ~ (would only render username)",
			workDir:  "/Users/alice",
			wsID:     wsID,
			taskID:   taskID,
			expected: "~",
		},
		{
			name:     "exact home directory with trailing slash renders ~",
			workDir:  "/Users/alice/",
			wsID:     wsID,
			taskID:   taskID,
			expected: "~",
		},
		{
			name:     "Windows local_directory path under home renders ~/",
			workDir:  `C:\Users\alice\repos\foo`,
			wsID:     wsID,
			taskID:   taskID,
			expected: "~/repos/foo",
		},
		{
			name:     "non-home managed task falls back to envRoot suffix",
			workDir:  "/opt/multica/" + wsID + "/5c57b65b/workdir",
			wsID:     wsID,
			taskID:   taskID,
			expected: wsID + "/5c57b65b/workdir",
		},
		{
			name:     "non-home path without envRoot match falls back to basename only",
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
			name:     "Windows backslash separators are normalized under ~/",
			workDir:  `C:\Users\alice\multica_workspaces\` + wsID + `\5c57b65b\workdir`,
			wsID:     wsID,
			taskID:   taskID,
			expected: "~/multica_workspaces/" + wsID + "/5c57b65b/workdir",
		},
		{
			name:     "missing workspace_id under home still renders ~/ (home preferred)",
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/5c57b65b/workdir",
			wsID:     "",
			taskID:   taskID,
			expected: "~/multica_workspaces/" + wsID + "/5c57b65b/workdir",
		},
		{
			name:     "missing task_id under home still renders ~/ (home preferred)",
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/5c57b65b/workdir",
			wsID:     wsID,
			taskID:   "",
			expected: "~/multica_workspaces/" + wsID + "/5c57b65b/workdir",
		},
		{
			name:     "trailing slash on home path is preserved in returned remainder",
			workDir:  "/Users/alice/multica_workspaces/" + wsID + "/5c57b65b/workdir/",
			wsID:     wsID,
			taskID:   taskID,
			expected: "~/multica_workspaces/" + wsID + "/5c57b65b/workdir/",
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
			expected: "~/repos/foo",
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
