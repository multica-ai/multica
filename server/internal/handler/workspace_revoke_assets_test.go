package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// memberAssetsFixture adds, on top of setupRevocationFixture, what a departing
// member leaves running outside their own runtime (MUL-10284): an agent and a
// product agent they own on a colleague's runtime, tasks they started on a
// colleague's agent, an autopilot and a wakeup they created. Each comes with a
// colleague-owned twin that must survive.
type memberAssetsFixture struct {
	revocationFixture
	OwnedAgentID         string
	OwnedAgentTaskID     string
	ProductAgentID       string
	InitiatedTaskID      string
	OriginatedTaskID     string
	ColleagueTaskID      string
	AutopilotID          string
	ColleagueAutopilotID string
	WakeupID             string
}

func setupMemberAssetsFixture(t *testing.T, slug string) memberAssetsFixture {
	t.Helper()
	fx := setupRevocationFixture(t, slug, "daemon-"+slug)

	colleagueRuntimeID := dbfx.Runtime(t, "Colleague Runtime", testutil.Cols{
		"workspace_id": fx.WorkspaceID,
		"visibility":   "public",
	})
	ownedAgentID := dbfx.Agent(t, "Owned elsewhere", colleagueRuntimeID, testutil.Cols{
		"workspace_id": fx.WorkspaceID,
		"visibility":   "workspace",
		"owner_id":     fx.TargetUserID,
	})
	productAgentID := dbfx.Agent(t, "Mika", colleagueRuntimeID, testutil.Cols{
		"workspace_id": fx.WorkspaceID,
		"visibility":   "workspace",
		"owner_id":     fx.TargetUserID,
		"system_key":   "mika",
	})
	colleagueAgentID := dbfx.Agent(t, "Colleague agent", colleagueRuntimeID, testutil.Cols{
		"workspace_id": fx.WorkspaceID,
		"visibility":   "workspace",
	})

	autopilot := func(title, creator string) string {
		return dbfx.Insert(t, "autopilot", testutil.Cols{
			"workspace_id":    fx.WorkspaceID,
			"title":           title,
			"assignee_type":   "agent",
			"assignee_id":     colleagueAgentID,
			"status":          "active",
			"execution_mode":  "run_only",
			"created_by_type": "member",
			"created_by_id":   creator,
		})
	}

	issueID := dbfx.Issue(t, "Wakeup target", testutil.Cols{"workspace_id": fx.WorkspaceID})
	return memberAssetsFixture{
		revocationFixture: fx,
		OwnedAgentID:      ownedAgentID,
		OwnedAgentTaskID:  dbfx.Task(t, ownedAgentID, testutil.Cols{"runtime_id": colleagueRuntimeID}),
		ProductAgentID:    productAgentID,
		InitiatedTaskID: dbfx.Task(t, colleagueAgentID, testutil.Cols{
			"runtime_id":        colleagueRuntimeID,
			"initiator_user_id": fx.TargetUserID,
		}),
		OriginatedTaskID: dbfx.Task(t, colleagueAgentID, testutil.Cols{
			"runtime_id":          colleagueRuntimeID,
			"status":              "running",
			"originator_user_id":  fx.TargetUserID,
			"accountable_user_id": fx.TargetUserID,
		}),
		ColleagueTaskID: dbfx.Task(t, colleagueAgentID, testutil.Cols{
			"runtime_id":          colleagueRuntimeID,
			"initiator_user_id":   testUserID,
			"originator_user_id":  testUserID,
			"accountable_user_id": testUserID,
		}),
		AutopilotID:          autopilot("Departing member's autopilot", fx.TargetUserID),
		ColleagueAutopilotID: autopilot("Colleague's autopilot", testUserID),
		WakeupID: dbfx.Insert(t, "issue_wakeup", testutil.Cols{
			"id":           testutil.Raw("gen_random_uuid()"),
			"workspace_id": fx.WorkspaceID,
			"issue_id":     issueID,
			"agent_id":     colleagueAgentID,
			"created_by":   fx.TargetUserID,
			"instruction":  "wake me",
			"kind":         "event",
			"mode":         "once",
			"event_types":  testutil.Raw("ARRAY['comment.created']"),
		}),
	}
}

func assertMemberAssetsRetired(t *testing.T, fx memberAssetsFixture, heir string) {
	t.Helper()

	var ownedArchived bool
	dbfx.QueryRow(t, `SELECT archived_at IS NOT NULL FROM agent WHERE id = $1`, fx.OwnedAgentID).Scan(&ownedArchived)
	if !ownedArchived {
		t.Error("agent the member owns on a colleague's runtime was not archived")
	}

	var productArchived bool
	var productOwner string
	dbfx.QueryRow(t, `SELECT archived_at IS NOT NULL, owner_id::text FROM agent WHERE id = $1`, fx.ProductAgentID).
		Scan(&productArchived, &productOwner)
	if productArchived {
		t.Error("product agent was archived")
	}
	if productOwner != heir {
		t.Errorf("product agent owner = %s, want %s", productOwner, heir)
	}

	taskStatus := func(id string) string {
		var status string
		dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, id).Scan(&status)
		return status
	}
	for name, id := range map[string]string{
		"owned agent's task":   fx.OwnedAgentTaskID,
		"task they initiated":  fx.InitiatedTaskID,
		"task they originated": fx.OriginatedTaskID,
	} {
		if got := taskStatus(id); got != "cancelled" {
			t.Errorf("%s = %q, want cancelled", name, got)
		}
	}
	if got := taskStatus(fx.ColleagueTaskID); got != "queued" {
		t.Errorf("colleague's task = %q, want queued", got)
	}

	var status, reason string
	dbfx.QueryRow(t, `SELECT status, COALESCE(pause_reason, '') FROM autopilot WHERE id = $1`, fx.AutopilotID).
		Scan(&status, &reason)
	if status != "paused" || reason != "member_removed" {
		t.Errorf("member's autopilot = (%q, %q), want (paused, member_removed)", status, reason)
	}
	dbfx.QueryRow(t, `SELECT status FROM autopilot WHERE id = $1`, fx.ColleagueAutopilotID).Scan(&status)
	if status != "active" {
		t.Errorf("colleague's autopilot = %q, want active", status)
	}

	var enabled bool
	dbfx.QueryRow(t, `SELECT enabled FROM issue_wakeup WHERE id = $1`, fx.WakeupID).Scan(&enabled)
	if enabled {
		t.Error("member's wakeup is still enabled")
	}
}

func TestDeleteMember_RetiresAssetsBeyondOwnRuntimes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := setupMemberAssetsFixture(t, "handler-tests-revoke-assets-kick")

	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParams(req, "id", fx.WorkspaceID, "memberId", fx.MemberID)
	testutil.Call(t, testHandler.DeleteMember, req).Want(http.StatusNoContent)

	assertRevoked(t, fx.revocationFixture)
	assertMemberAssetsRetired(t, fx, testUserID)
}

// A member who leaves on their own cannot inherit their own product agent; it
// goes to the remaining owner.
func TestLeaveWorkspace_RetiresAssetsAndHandsProductAgentToOwner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := setupMemberAssetsFixture(t, "handler-tests-revoke-assets-leave")

	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/leave", nil)
	req.Header.Set("X-User-ID", fx.TargetUserID)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParam(req, "id", fx.WorkspaceID)
	testutil.Call(t, testHandler.LeaveWorkspace, req).Want(http.StatusNoContent)

	assertRevoked(t, fx.revocationFixture)
	assertMemberAssetsRetired(t, fx, testUserID)
}

// Members removed before the asset rules existed are found by
// ListDepartedMemberAssetHolders and retired the same way, which is what
// cmd/backfill_departed_members runs.
func TestRetireMemberAssets_BackfillsDepartedMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := setupMemberAssetsFixture(t, "handler-tests-revoke-assets-backfill")
	dbfx.Exec(t, `DELETE FROM member WHERE id = $1`, fx.MemberID)
	ctx := context.Background()

	listed := func() bool {
		t.Helper()
		holders, err := testHandler.Queries.ListDepartedMemberAssetHolders(ctx)
		if err != nil {
			t.Fatalf("ListDepartedMemberAssetHolders: %v", err)
		}
		for _, h := range holders {
			if util.UUIDToString(h.WorkspaceID) == fx.WorkspaceID && util.UUIDToString(h.UserID) == fx.TargetUserID {
				return true
			}
		}
		return false
	}
	if !listed() {
		t.Fatal("departed member with live assets is not listed")
	}

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	qtx := testHandler.Queries.WithTx(tx)
	workspaceID, userID := parseUUID(fx.WorkspaceID), parseUUID(fx.TargetUserID)
	heir, err := service.MemberAssetHeir(ctx, qtx, workspaceID, userID, pgtype.UUID{})
	if err != nil {
		t.Fatalf("MemberAssetHeir: %v", err)
	}
	if _, err := service.RetireMemberAssets(ctx, qtx, workspaceID, userID, pgtype.UUID{}, heir); err != nil {
		t.Fatalf("RetireMemberAssets: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	assertMemberAssetsRetired(t, fx, testUserID)
	if listed() {
		t.Error("departed member is still listed after the backfill")
	}
}

// A daemon connection outlives the membership it was opened under. Once the
// re-check interval has passed, its heartbeat must report the runtime gone
// instead of keeping it alive.
func TestHandleDaemonWSHeartbeat_RemovedMemberRuntimeIsGone(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := setupRevocationFixture(t, "handler-tests-ws-heartbeat-removed", "daemon-ws-heartbeat-removed")
	ctx := context.Background()

	heartbeat := func() (*protocol.DaemonHeartbeatAckPayload, *daemonws.RuntimeLease) {
		t.Helper()
		lease := daemonws.NewRuntimeLease(fx.WorkspaceID, "online", time.Now(), true)
		lease.MarkMembershipVerified(time.Now().Add(-2 * daemonWSMembershipRecheckInterval))
		ack, err := testHandler.HandleDaemonWSHeartbeat(ctx, daemonws.ClientIdentity{
			UserID:        fx.TargetUserID,
			WorkspaceIDs:  []string{fx.WorkspaceID},
			RuntimeIDs:    []string{fx.RuntimeID},
			RuntimeLeases: map[string]*daemonws.RuntimeLease{fx.RuntimeID: lease},
		}, fx.RuntimeID, protocol.DaemonHeartbeatRequestPayload{})
		if err != nil {
			t.Fatalf("HandleDaemonWSHeartbeat: %v", err)
		}
		return ack, lease
	}

	ack, lease := heartbeat()
	if ack == nil || ack.RuntimeGone {
		t.Fatalf("member heartbeat ack = %+v, want a live ack", ack)
	}
	if !lease.MembershipVerifiedWithin(time.Now(), daemonWSMembershipRecheckInterval) {
		t.Error("a successful re-check did not refresh the lease")
	}

	dbfx.Exec(t, `DELETE FROM member WHERE id = $1`, fx.MemberID)
	testHandler.MembershipCache.Invalidate(ctx, fx.TargetUserID, fx.WorkspaceID)

	ack, _ = heartbeat()
	if ack == nil || !ack.RuntimeGone || ack.RuntimeID != fx.RuntimeID {
		t.Fatalf("removed member heartbeat ack = %+v, want runtime_gone for %s", ack, fx.RuntimeID)
	}
}

// A qoder-managed connection has no human principal and is not membership
// checked; an unknown user is.
func TestDaemonConnectionPrincipalIsMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	cases := []struct {
		name     string
		identity daemonws.ClientIdentity
		want     bool
	}{
		{"member user", daemonws.ClientIdentity{UserID: testUserID}, true},
		{"unknown user", daemonws.ClientIdentity{UserID: uuid.NewString()}, false},
		{"token bound to a member", daemonws.ClientIdentity{DaemonID: "d", DaemonTokenOwnerID: testUserID}, true},
		{"token bound to a non-member", daemonws.ClientIdentity{DaemonID: "d", DaemonTokenOwnerID: uuid.NewString()}, false},
		{"qoder-managed token", daemonws.ClientIdentity{DaemonID: "qoder-managed-x"}, true},
	}
	for _, tc := range cases {
		got, err := testHandler.daemonConnectionPrincipalIsMember(ctx, tc.identity, testWorkspaceID)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
