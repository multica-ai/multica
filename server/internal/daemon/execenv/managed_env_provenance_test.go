package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPrepareManagedIssueEnvWritesProvenance verifies Prepare drops the
// reuse-eligibility marker for a normal (non-local) issue env, carrying the
// owning workspace/issue/agent. This is the artifact shouldReusePriorWorkdir
// keys off so a follow-up claimed before .gc_meta.json is written can still
// prove the workdir is a safe reuse target (MUL-4886).
func TestPrepareManagedIssueEnvWritesProvenance(t *testing.T) {
	root := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "ws-prov-001",
		TaskID:         "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentName:      "Prov Agent",
		Task: TaskContextForEnv{
			IssueID: "issue-prov-1",
			AgentID: "agent-prov-1",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	prov, err := ReadManagedEnvProvenance(env.RootDir)
	if err != nil {
		t.Fatalf("read managed env provenance: %v", err)
	}
	if prov.ManagedBy != ManagedEnvProvenanceManagedBy {
		t.Fatalf("managed_by = %q, want %q", prov.ManagedBy, ManagedEnvProvenanceManagedBy)
	}
	if prov.WorkspaceID != "ws-prov-001" || prov.IssueID != "issue-prov-1" || prov.AgentID != "agent-prov-1" {
		t.Fatalf("provenance owner mismatch: %+v", prov)
	}
}

// TestPrepareLocalDirectoryWritesNoProvenance pins the local_directory branch:
// a task bound to a user-supplied path must NOT get a managed-env provenance
// marker, so a squad leader can never treat the user's directory as a reusable
// managed workdir. Absence of the marker is the fail-closed signal.
func TestPrepareLocalDirectoryWritesNoProvenance(t *testing.T) {
	root := t.TempDir()
	localDir := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "ws-prov-local",
		TaskID:         "bbbbbbbb-cccc-dddd-eeee-ffffffffffff",
		AgentName:      "Local Agent",
		LocalWorkDir:   localDir,
		Task: TaskContextForEnv{
			IssueID: "issue-prov-local",
			AgentID: "agent-prov-local",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if _, err := ReadManagedEnvProvenance(env.RootDir); !os.IsNotExist(err) {
		t.Fatalf("local_directory Prepare must not write managed env provenance; got err = %v", err)
	}
	// The user's own directory must never receive the marker either.
	if _, err := os.Stat(filepath.Join(localDir, managedEnvProvenanceFile)); !os.IsNotExist(err) {
		t.Fatal("managed env provenance leaked into the user's local directory")
	}
}

// TestPrepareNonIssueEnvWritesNoProvenance confirms non-issue envs (chat,
// autopilot, quick-create) get no provenance: reuse targets only issue tasks,
// so writing it elsewhere would be dead weight.
func TestPrepareNonIssueEnvWritesNoProvenance(t *testing.T) {
	root := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "ws-prov-chat",
		TaskID:         "cccccccc-dddd-eeee-ffff-000000000000",
		AgentName:      "Chat Agent",
		Task: TaskContextForEnv{
			ChatSessionID: "chat-1",
			AgentID:       "agent-chat",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if _, err := ReadManagedEnvProvenance(env.RootDir); !os.IsNotExist(err) {
		t.Fatalf("non-issue Prepare must not write managed env provenance; got err = %v", err)
	}
}
