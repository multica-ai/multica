package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestRuntimeProfileBinding_RetiresCopiesAndPreservesHistory(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	id := insertRuntimeProfileFixture(t, context.Background(), "Bind existing "+uuid.NewString(), "codex", "codex")
	target := "keep-" + uuid.NewString()
	kept := dbfx.Runtime(t, "Kept custom runtime", testutil.Cols{"runtime_mode": "local", "daemon_id": target, "profile_id": id})
	removed := dbfx.Runtime(t, "Other machine", testutil.Cols{"runtime_mode": "local", "daemon_id": "remove-" + uuid.NewString(), "profile_id": id})
	archived := dbfx.Agent(t, "Archived agent", removed, testutil.Cols{"archived_at": testutil.Raw("now()")})
	history := dbfx.Task(t, archived, testutil.Cols{"runtime_id": removed, "status": "completed", "completed_at": testutil.Raw("now()")})
	req := withURLParams(newRequest("PATCH", "/", map[string]any{"daemon_id": target, "display_name": "Bound " + uuid.NewString()}), "id", testWorkspaceID, "profileId", id)
	var out RuntimeProfileResponse
	testutil.Call(t, testHandler.UpdateRuntimeProfile, req).Want(http.StatusOK).JSON(&out)
	if out.DaemonID == nil || *out.DaemonID != target {
		t.Fatalf("binding = %+v", out)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_runtime WHERE profile_id=$1`, id); n != 1 {
		t.Fatalf("remaining copies=%d", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_runtime WHERE id=$1`, kept); n != 1 {
		t.Fatal("target instance removed")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent WHERE id=$1 AND runtime_id IS NULL`, archived); n != 1 {
		t.Fatal("archived agent lost")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id=$1 AND runtime_id IS NULL`, history); n != 1 {
		t.Fatal("task history lost")
	}
	// Once bound, a stale daemon cannot restore the removed copy.
	testutil.Call(t, testHandler.DaemonRegister, newOwnerDaemonRequest("POST", "/", map[string]any{
		"workspace_id": testWorkspaceID, "daemon_id": "new-other-" + uuid.NewString(),
		"failed_profiles": []map[string]any{{"profile_id": id, "reason": "missing command"}},
	})).Want(http.StatusOK)
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_runtime WHERE profile_id=$1`, id); n != 1 {
		t.Fatal("stale registration restored a copy")
	}
	move := withURLParams(newRequest("PATCH", "/", map[string]any{"daemon_id": "different-machine"}), "id", testWorkspaceID, "profileId", id)
	testutil.Call(t, testHandler.UpdateRuntimeProfile, move).Want(http.StatusConflict)
	if n := dbfx.Count(t, `SELECT count(*) FROM runtime_profile WHERE id=$1 AND daemon_id=$2`, id, target); n != 1 {
		t.Fatal("bound profile moved to another machine")
	}
}

func TestRuntimeProfileBinding_BlocksActiveDependenciesAndRollsBack(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, kind := range []string{"agent", "task"} {
		t.Run(kind, func(t *testing.T) {
			id := insertRuntimeProfileFixture(t, context.Background(), "In use "+uuid.NewString(), "codex", "codex")
			target := "target-" + uuid.NewString()
			dbfx.Runtime(t, "Target", testutil.Cols{"runtime_mode": "local", "daemon_id": target, "profile_id": id})
			busy := dbfx.Runtime(t, "Busy machine", testutil.Cols{"runtime_mode": "local", "daemon_id": "busy-" + uuid.NewString(), "profile_id": id})
			agentCols := testutil.Cols{}
			if kind == "task" {
				agentCols["archived_at"] = testutil.Raw("now()")
			}
			agent := dbfx.Agent(t, "Busy agent", busy, agentCols)
			if kind == "task" {
				dbfx.Task(t, agent, testutil.Cols{"runtime_id": busy})
			}
			req := withURLParams(newRequest("PATCH", "/", map[string]any{"daemon_id": target, "display_name": "Must roll back"}), "id", testWorkspaceID, "profileId", id)
			testutil.Call(t, testHandler.UpdateRuntimeProfile, req).Want(http.StatusConflict)
			if n := dbfx.Count(t, `SELECT count(*) FROM runtime_profile WHERE id=$1 AND daemon_id IS NULL AND display_name <> 'Must roll back'`, id); n != 1 {
				t.Fatal("failed binding changed profile")
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_runtime WHERE profile_id=$1`, id); n != 2 {
				t.Fatal("failed binding removed an instance")
			}
			// Editing settings without selecting a machine does not retire copies,
			// even when binding would be blocked by work on another machine.
			renamed := "Edited without binding " + uuid.NewString()
			edit := withURLParams(newRequest("PATCH", "/", map[string]any{
				"display_name": renamed, "command_name": "wrapper", "fixed_args": []string{"--model", "custom"},
			}), "id", testWorkspaceID, "profileId", id)
			var out RuntimeProfileResponse
			testutil.Call(t, testHandler.UpdateRuntimeProfile, edit).Want(http.StatusOK).JSON(&out)
			if out.DaemonID != nil || out.DisplayName != renamed || out.CommandName != "wrapper" {
				t.Fatalf("settings-only edit=%+v", out)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_runtime WHERE profile_id=$1`, id); n != 2 {
				t.Fatal("settings-only edit removed an instance")
			}
		})
	}
}

func TestRuntimeProfileBinding_RejectsUnknownOrForeignMachine(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	id := insertRuntimeProfileFixture(t, context.Background(), "Scope "+uuid.NewString(), "codex", "codex")
	ws := dbfx.Workspace(t, "Other workspace", "binding-other-"+uuid.NewString())
	foreign := "foreign-" + uuid.NewString()
	dbfx.Runtime(t, "Foreign", testutil.Cols{"workspace_id": ws, "daemon_id": foreign, "runtime_mode": "local"})
	for _, target := range []string{"", foreign, "missing-" + uuid.NewString()} {
		req := withURLParams(newRequest("PATCH", "/", map[string]any{"daemon_id": target}), "id", testWorkspaceID, "profileId", id)
		testutil.Call(t, testHandler.UpdateRuntimeProfile, req).Want(http.StatusBadRequest)
	}
}
