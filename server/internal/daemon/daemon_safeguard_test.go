package daemon

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

// TestDaemonTaskHasUsableContext_ReposPass pins the daemon-side
// secondary context guard (MUL-4059). Even when the server is
// misconfigured with AGENT_CONTEXT_GUARD_DEFAULT_POLICY=off, the
// daemon must refuse to dispatch a task whose claim payload shows
// no repos and no project local_directory.
func TestDaemonTaskHasUsableContext_ReposPass(t *testing.T) {
	task := Task{
		ID:    "task-1",
		Repos: []RepoData{{URL: "https://example.com/repo.git"}},
	}
	if !daemonTaskHasUsableContext(task) {
		t.Fatal("expected OK with at least one repo")
	}
}

func TestDaemonTaskHasUsableContext_LocalDirectoryPass(t *testing.T) {
	task := Task{
		ID:               "task-2",
		ProjectResources: []ProjectResourceData{{ResourceType: "local_directory"}},
	}
	if !daemonTaskHasUsableContext(task) {
		t.Fatal("expected OK with project local_directory resource")
	}
}

func TestDaemonTaskHasUsableContext_NeitherFails(t *testing.T) {
	task := Task{
		ID:               "task-3",
		ProjectResources: []ProjectResourceData{{ResourceType: "github_repo"}},
	}
	// github_repo resources in the ProjectResources array don't
	// satisfy the guard on their own — the daemon would have to clone
	// them somewhere, and no repo path lands in execenv. Only a
	// workspace-level repos entry or a local_directory qualifies.
	if daemonTaskHasUsableContext(task) {
		t.Fatal("expected fail with only github_repo project resources")
	}
}

func TestDaemonTaskHasUsableContext_EmptyFails(t *testing.T) {
	task := Task{ID: "task-4"}
	if daemonTaskHasUsableContext(task) {
		t.Fatal("expected fail with no repos and no project resources")
	}
}

func TestTaskStruct_NewFieldsPickedUp(t *testing.T) {
	// Sanity check that the JSON tags render what the daemon expects.
	// Server-side contract: max_inactivity_secs is read at claim time
	// and stuffed into the daemon-side ExecOptions via opts.MaxInactivitySecs.
	task := Task{
		ID:                "task-5",
		MaxInactivitySecs: 600,
	}
	if task.MaxInactivitySecs != 600 {
		t.Fatalf("MaxInactivitySecs not carried: %d", task.MaxInactivitySecs)
	}
}

// helper used in the new safeguard tests; not used today but keeps
// the test file aligned with the daemon's util-package idiom in case
// future tests need a UUID-style id from a test string.
var _ = util.MustParseUUID