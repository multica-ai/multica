package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPrepareRollsBackSidecarsWhenPrepareFailsInPlace is the regression test for
// MUL-6132: a local_directory Prepare that fails partway must not leave the
// daemon task marker (or any other sidecar) in the user's own repository.
//
// Before the fix, Prepare wrote the marker early and persisted the manifest that
// records it last, and the caller receives no Environment on a failure — so a
// failed Prepare left a marker no code path knew how to remove, which disabled
// every human-local multica command in that directory tree until someone deleted
// the file by hand.
//
// The induced failure is a pre-existing .cursor/mcp.json, which the managed
// cursor MCP path refuses to overwrite. It is a pure filesystem failure that
// needs no agent CLI on the machine, and it lands after the marker is written.
func TestPrepareRollsBackSidecarsWhenPrepareFailsInPlace(t *testing.T) {
	workspacesRoot := t.TempDir()
	userDir := t.TempDir()

	// The user's own content: the file that makes Prepare fail, plus a plain
	// file that must survive the rollback untouched.
	cursorDir := filepath.Join(userDir, ".cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatalf("create .cursor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cursorDir, "mcp.json"), []byte(`{"mine":true}`), 0o644); err != nil {
		t.Fatalf("seed user mcp.json: %v", err)
	}
	userFile := filepath.Join(userDir, "README.md")
	if err := os.WriteFile(userFile, []byte("user content"), 0o644); err != nil {
		t.Fatalf("seed user file: %v", err)
	}

	_, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-rollback-001",
		TaskID:         "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentName:      "Test Agent",
		Provider:       "cursor",
		LocalWorkDir:   userDir,
		McpConfig:      json.RawMessage(`{"fetch":{"command":"uvx","args":["mcp-server-fetch"]}}`),
		Task: TaskContextForEnv{
			IssueID: "11111111-2222-3333-4444-555555555555",
			AgentID: "99999999-8888-7777-6666-555555555555",
		},
	}, testLogger())
	if err == nil {
		t.Fatal("Prepare succeeded; expected the pre-existing .cursor/mcp.json to fail it")
	}

	markerPath := filepath.Join(userDir, TaskContextMarkerRelPath)
	if _, statErr := os.Stat(markerPath); !os.IsNotExist(statErr) {
		t.Fatalf("daemon task marker survived a failed Prepare at %s (stat err: %v)", markerPath, statErr)
	}
	if _, statErr := os.Stat(filepath.Join(userDir, ".agent_context")); !os.IsNotExist(statErr) {
		t.Fatalf(".agent_context survived a failed Prepare (stat err: %v)", statErr)
	}

	// The rollback must be scoped to what Prepare created: the user's own
	// files, including the one that caused the failure, stay exactly as they
	// were.
	if data, readErr := os.ReadFile(userFile); readErr != nil || string(data) != "user content" {
		t.Fatalf("rollback touched user content: data=%q err=%v", string(data), readErr)
	}
	if data, readErr := os.ReadFile(filepath.Join(cursorDir, "mcp.json")); readErr != nil || string(data) != `{"mine":true}` {
		t.Fatalf("rollback touched the user's mcp.json: data=%q err=%v", string(data), readErr)
	}
}

// TestPrepareRollsBackWhenWriteContextFilesFailsAfterMarker covers the earliest
// failure window there is: writeContextFiles lays the marker down as its very
// first act, then writes skills and project resources — either of which can
// fail with the marker already on disk.
//
// The first version of the MUL-6132 fix armed the rollback only after
// writeContextFiles returned, so exactly these failures still stranded a marker
// in the user's repository with nothing else beside it (MUL-6132 review). The
// induced failure is a plain file where .agent_context must be a directory;
// the task carries a skill because the default provider's skills tree
// (.agent_context/skills) is now the only thing that opens that directory.
func TestPrepareRollsBackWhenWriteContextFilesFailsAfterMarker(t *testing.T) {
	workspacesRoot := t.TempDir()
	userDir := t.TempDir()

	// A regular file at the path writeContextFiles needs as a directory, one
	// step after it writes the marker.
	blocker := filepath.Join(userDir, ".agent_context")
	if err := os.WriteFile(blocker, []byte("user file"), 0o644); err != nil {
		t.Fatalf("seed .agent_context blocker: %v", err)
	}

	_, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-rollback-003",
		TaskID:         "cccccccc-dddd-eeee-ffff-000000000000",
		AgentName:      "Test Agent",
		LocalWorkDir:   userDir,
		Task: TaskContextForEnv{
			IssueID:     "33333333-4444-5555-6666-777777777777",
			AgentID:     "77777777-6666-5555-4444-333333333333",
			AgentSkills: []SkillContextForEnv{{Name: "Go Conventions", Content: "Follow Go conventions."}},
		},
	}, testLogger())
	if err == nil {
		t.Fatal("Prepare succeeded; expected the .agent_context blocker to fail it")
	}

	markerPath := filepath.Join(userDir, TaskContextMarkerRelPath)
	if _, statErr := os.Stat(markerPath); !os.IsNotExist(statErr) {
		t.Fatalf("daemon task marker survived a failure inside writeContextFiles at %s (stat err: %v)", markerPath, statErr)
	}
	if data, readErr := os.ReadFile(blocker); readErr != nil || string(data) != "user file" {
		t.Fatalf("rollback touched the user's own file: data=%q err=%v", string(data), readErr)
	}
}

// TestPrepareSucceedsInPlaceAndLeavesMarker guards the other half of the
// contract: the rollback fires only on failure. A Prepare that completes must
// still leave the marker in place — it is the guard that makes a CLI invocation
// from inside the task fail closed — and must persist the manifest that the
// task teardown later uses to remove it.
func TestPrepareSucceedsInPlaceAndLeavesMarker(t *testing.T) {
	workspacesRoot := t.TempDir()
	userDir := t.TempDir()

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-rollback-002",
		TaskID:         "bbbbbbbb-cccc-dddd-eeee-ffffffffffff",
		AgentName:      "Test Agent",
		LocalWorkDir:   userDir,
		Task: TaskContextForEnv{
			IssueID: "22222222-3333-4444-5555-666666666666",
			AgentID: "88888888-7777-6666-5555-444444444444",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(userDir, TaskContextMarkerRelPath)); statErr != nil {
		t.Fatalf("successful Prepare did not leave the daemon task marker: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(env.RootDir, sidecarManifestFile)); statErr != nil {
		t.Fatalf("successful Prepare did not persist the sidecar manifest: %v", statErr)
	}

	// And the recorded manifest is what makes the marker removable later.
	if err := CleanupSidecars(env.RootDir); err != nil {
		t.Fatalf("CleanupSidecars: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(userDir, TaskContextMarkerRelPath)); !os.IsNotExist(statErr) {
		t.Fatalf("marker survived CleanupSidecars (stat err: %v)", statErr)
	}
}
