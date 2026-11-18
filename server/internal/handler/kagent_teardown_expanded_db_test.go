package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestKagentExpandedProfileTeardown(t *testing.T) {
	for _, enrolled := range []bool{true, false} {
		t.Run(map[bool]string{true: "orphan-tombstone", false: "unenrolled"}[enrolled], func(t *testing.T) {
			f, ws, rt := teardownGuardFixture(t, "claude")
			profile := f.Insert(t, "runtime_profile", testutil.Cols{"workspace_id": ws, "display_name": "Guard profile", "protocol_family": "claude", "command_name": "guard-profile", "created_by": testUserID})
			f.Exec(t, `UPDATE agent_runtime SET profile_id=$1 WHERE id=$2`, profile, rt)
			a := f.Agent(t, "Archived", rt, testutil.Cols{"archived_at": testutil.Raw("now()")})
			task := f.Task(t, a, testutil.Cols{"runtime_id": rt})
			if enrolled {
				teardownGuardEnrollment(t, f, ws, rt, uuid.NewString(), "absent", "deleted")
			}
			req := withURLParams(newRequest("DELETE", "/profiles/"+profile, nil), "id", ws, "profileId", profile)
			req.Header.Set("X-Workspace-ID", ws)
			want := http.StatusNoContent
			if enrolled {
				want = http.StatusConflict
			}
			response := testutil.Call(t, testHandler.DeleteRuntimeProfile, req).Want(want)
			if enrolled && !strings.Contains(response.Text(), kagentTeardownBlockedMessage) {
				t.Fatal(response.Text())
			}
			var intact bool
			f.QueryRow(t, `SELECT EXISTS(SELECT 1 FROM runtime_profile WHERE id=$1) AND EXISTS(SELECT 1 FROM agent_runtime WHERE id=$2) AND EXISTS(SELECT 1 FROM agent WHERE id=$3 AND runtime_id=$2) AND EXISTS(SELECT 1 FROM agent_task_queue WHERE id=$4 AND runtime_id=$2 AND status='queued')`, profile, rt, a, task).Scan(&intact)
			if intact != enrolled {
				t.Fatalf("intact=%v enrolled=%v", intact, enrolled)
			}
		})
	}
}

func TestKagentExpandedMergeTeardown(t *testing.T) {
	for _, location := range []string{"old", "target", "none"} {
		t.Run(location, func(t *testing.T) {
			f, ws, old := teardownGuardFixture(t, "claude")
			target := f.Runtime(t, "Merge target", testutil.Cols{"provider": "claude", "runtime_mode": "local"})
			a := f.Agent(t, "Merge agent", old)
			task := f.Task(t, a, testutil.Cols{"runtime_id": old, "status": "completed"})
			if location != "none" {
				enrolled := old
				if location == "target" {
					enrolled = target
				}
				teardownGuardEnrollment(t, f, ws, enrolled, uuid.NewString(), "absent", "deleted")
			}
			err := testHandler.mergeLegacyRuntime(context.Background(), parseUUID(target), parseUUID(old), "legacy-guard", "claude", parseUUID(testUserID))
			if location != "none" && !errors.Is(err, service.ErrRuntimeKagentEnrollment) {
				t.Fatalf("merge error=%v", err)
			}
			if location == "none" && err != nil {
				t.Fatal(err)
			}
			expected := old
			if location == "none" {
				expected = target
			}
			var agentRT, taskRT string
			f.QueryRow(t, `SELECT runtime_id FROM agent WHERE id=$1`, a).Scan(&agentRT)
			f.QueryRow(t, `SELECT runtime_id FROM agent_task_queue WHERE id=$1`, task).Scan(&taskRT)
			if agentRT != expected || taskRT != expected {
				t.Fatalf("agent=%s task=%s expected=%s", agentRT, taskRT, expected)
			}
			var exists bool
			f.QueryRow(t, `SELECT EXISTS(SELECT 1 FROM agent_runtime WHERE id=$1)`, old).Scan(&exists)
			if exists != (location != "none") {
				t.Fatalf("old runtime retained=%v", exists)
			}
		})
	}
}

func TestKagentExpandedQoderRefusesBeforeStop(t *testing.T) {
	for _, scope := range []string{"disconnect", "workspace"} {
		t.Run(scope, func(t *testing.T) {
			f, ws, rt := teardownGuardFixture(t, "qoder_cloud")
			token := f.Insert(t, "daemon_token", testutil.Cols{"workspace_id": ws, "daemon_id": "guard-disconnect", "expires_at": testutil.Raw("now() + interval '1 hour'"), "token_hash": "guard-" + uuid.NewString()})
			f.InsertNoID(t, "cloud_agent_connection", testutil.Cols{"workspace_id": ws, "provider": "qoder_cloud", "owner_id": testUserID, "token_id": token, "config_encrypted": "untouched-config", "enabled": true, "journal_encrypted": "untouched-journal"}, "workspace_id=$1 AND provider='qoder_cloud'", ws)
			teardownGuardEnrollment(t, f, ws, rt, uuid.NewString(), "absent", "deleted")
			req := withURLParam(newRequest("DELETE", "/qoder", map[string]any{"expected_active_agent_ids": []string{}}), "id", ws)
			req.Header.Set("X-Workspace-ID", ws)
			handle := testHandler.DisconnectQoderConnection
			if scope == "workspace" {
				handle = testHandler.DeleteWorkspace
				req = teardownGuardRequest("workspace", ws, rt, nil)
			}
			response := testutil.Call(t, handle, req).Want(http.StatusConflict)
			if !strings.Contains(response.Text(), kagentTeardownBlockedMessage) {
				t.Fatal(response.Text())
			}
			var intact bool
			f.QueryRow(t, `SELECT enabled AND revision=1 AND config_encrypted='untouched-config' AND journal_encrypted='untouched-journal' AND EXISTS(SELECT 1 FROM daemon_token WHERE id=$2) AND EXISTS(SELECT 1 FROM agent_runtime WHERE id=$3) FROM cloud_agent_connection WHERE workspace_id=$1 AND provider='qoder_cloud'`, ws, token, rt).Scan(&intact)
			if !intact {
				t.Fatal("blocked disconnect stopped connection, revoked token, or changed journal/runtime")
			}
		})
	}
}
