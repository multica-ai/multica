package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func accountRequest(fx revocationFixture) AccountActionRequest {
	return AccountActionRequest{UserID: fx.TargetUserID, Actor: "ops", Reason: "test incident"}
}

// accountTokens gives the target a personal access token and a daemon token
// bound to them on a daemon with no runtime here, which only the account-wide
// revocation reaches.
func accountTokens(t *testing.T, fx revocationFixture) (patID, ownerTokenHash string) {
	t.Helper()
	patID = dbfx.Insert(t, "personal_access_token", testutil.Cols{
		"user_id":      fx.TargetUserID,
		"name":         "cli",
		"token_hash":   "pat-hash-" + fx.TargetUserID,
		"token_prefix": "mul_test",
	})
	ownerTokenHash = "owner-daemon-hash-" + fx.TargetUserID
	dbfx.Exec(t, `
INSERT INTO daemon_token (token_hash, workspace_id, daemon_id, expires_at, owner_id)
VALUES ($1, $2, 'elsewhere-daemon', now() + interval '1 day', $3)
`, ownerTokenHash, fx.WorkspaceID, fx.TargetUserID)
	return patID, ownerTokenHash
}

func TestIncidentResponse_RevokesAndStopsButKeepsMembership(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := setupMemberAssetsFixture(t, "handler-tests-incident-response")
	patID, ownerTokenHash := accountTokens(t, fx.revocationFixture)
	ctx := context.Background()

	// A dry run reports and changes nothing.
	dry := accountRequest(fx.revocationFixture)
	dry.DryRun = true
	report, err := testHandler.IncidentResponse(ctx, dry)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !report.DryRun || report.DryReport["workspaces"].(map[string]incidentWorkspace)[fx.WorkspaceID].TasksCancelled == 0 {
		t.Fatalf("dry run report = %+v, want this workspace's cancelled tasks", report.DryReport)
	}
	var suspended, revoked bool
	dbfx.QueryRow(t, `SELECT suspended_at IS NOT NULL FROM "user" WHERE id = $1`, fx.TargetUserID).Scan(&suspended)
	dbfx.QueryRow(t, `SELECT revoked FROM personal_access_token WHERE id = $1`, patID).Scan(&revoked)
	var archived bool
	dbfx.QueryRow(t, `SELECT archived_at IS NOT NULL FROM agent WHERE id = $1`, fx.AgentID).Scan(&archived)
	if suspended || revoked || archived {
		t.Fatalf("dry run changed state: suspended=%v pat revoked=%v agent archived=%v", suspended, revoked, archived)
	}

	report, err = testHandler.IncidentResponse(ctx, accountRequest(fx.revocationFixture))
	if err != nil {
		t.Fatalf("IncidentResponse: %v", err)
	}
	if report.User.SuspendedAt == nil || report.User.TokenVersion != 1 {
		t.Fatalf("user = %+v, want suspended with token_version 1", report.User)
	}
	if report.Event == nil || report.Event.Status != "completed" {
		t.Fatalf("event = %+v, want completed", report.Event)
	}
	var stored struct {
		Credentials incidentCredentials          `json:"credentials"`
		Workspaces  map[string]incidentWorkspace `json:"workspaces"`
	}
	if err := json.Unmarshal(report.Event.Report, &stored); err != nil {
		t.Fatalf("report: %v", err)
	}
	if stored.Credentials.PersonalAccessTokensRevoked != 1 || stored.Credentials.DaemonTokensDeleted != 1 {
		t.Errorf("credentials = %+v, want 1 PAT and 1 owner-bound daemon token", stored.Credentials)
	}
	if entry, ok := stored.Workspaces[fx.WorkspaceID]; !ok || len(entry.Runtimes) != 1 || len(entry.AutopilotsPaused) != 1 {
		t.Errorf("workspace entry = %+v (present %v), want one runtime and one autopilot", entry, ok)
	}

	dbfx.QueryRow(t, `SELECT revoked FROM personal_access_token WHERE id = $1`, patID).Scan(&revoked)
	if !revoked {
		t.Error("personal access token was not revoked")
	}
	var ownerTokenExists bool
	dbfx.QueryRow(t, `SELECT EXISTS (SELECT 1 FROM daemon_token WHERE token_hash = $1)`, ownerTokenHash).Scan(&ownerTokenExists)
	if ownerTokenExists {
		t.Error("daemon token bound to the account was not deleted")
	}
	var memberExists bool
	dbfx.QueryRow(t, `SELECT EXISTS (SELECT 1 FROM member WHERE id = $1)`, fx.MemberID).Scan(&memberExists)
	if !memberExists {
		t.Fatal("incident response removed the membership")
	}
	var runtimeStatus string
	dbfx.QueryRow(t, `SELECT status FROM agent_runtime WHERE id = $1`, fx.RuntimeID).Scan(&runtimeStatus)
	if runtimeStatus != "offline" {
		t.Errorf("runtime = %q, want offline", runtimeStatus)
	}
	assertMemberAssetsRetired(t, fx, testUserID)

	state, err := testHandler.AccountStates.Get(ctx, fx.TargetUserID)
	if err != nil || !state.Suspended || state.TokenVersion != 1 {
		t.Fatalf("account state = %+v (%v), want suspended at version 1", state, err)
	}
}

// A run that stopped part-way is finished by running it again: workspaces it
// already recorded are not stopped twice, and no second incident is opened.
func TestIncidentResponse_ResumesUnfinishedRun(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := setupRevocationFixture(t, "handler-tests-incident-resume", "daemon-incident-resume")
	eventID := dbfx.Insert(t, "user_suspension_event", testutil.Cols{
		"user_id": fx.TargetUserID,
		"action":  "incident_response",
		"actor":   "ops",
		"reason":  "earlier run",
		"status":  "running",
		"report":  `{"workspaces":{"` + fx.WorkspaceID + `":{}}}`,
	})

	report, err := testHandler.IncidentResponse(context.Background(), accountRequest(fx))
	if err != nil {
		t.Fatalf("IncidentResponse: %v", err)
	}
	if report.Event == nil || report.Event.ID != eventID || report.Event.Status != "completed" {
		t.Fatalf("event = %+v, want %s completed", report.Event, eventID)
	}
	var archived bool
	dbfx.QueryRow(t, `SELECT archived_at IS NOT NULL FROM agent WHERE id = $1`, fx.AgentID).Scan(&archived)
	if archived {
		t.Error("a workspace the earlier run recorded was stopped again")
	}
	var events int
	dbfx.QueryRow(t, `SELECT count(*) FROM user_suspension_event WHERE user_id = $1`, fx.TargetUserID).Scan(&events)
	if events != 1 {
		t.Errorf("events = %d, want the resumed one only", events)
	}
}

func TestUnsuspendAccount(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	t.Run("after a plain suspension warns that credentials work again", func(t *testing.T) {
		fx := setupRevocationFixture(t, "handler-tests-unsuspend-plain", "daemon-unsuspend-plain")
		if _, err := testHandler.SuspendAccount(ctx, accountRequest(fx)); err != nil {
			t.Fatalf("SuspendAccount: %v", err)
		}
		if state, _ := testHandler.AccountStates.Get(ctx, fx.TargetUserID); !state.Suspended {
			t.Fatal("account is not suspended")
		}
		report, err := testHandler.UnsuspendAccount(ctx, accountRequest(fx))
		if err != nil {
			t.Fatalf("UnsuspendAccount: %v", err)
		}
		if report.User.SuspendedAt != nil {
			t.Fatal("account is still suspended")
		}
		if len(report.Warnings) == 0 || !strings.Contains(report.Warnings[0], "work again") {
			t.Fatalf("warnings = %v, want the credentials warning", report.Warnings)
		}
		if state, _ := testHandler.AccountStates.Get(ctx, fx.TargetUserID); state.Suspended {
			t.Fatal("account state still suspended")
		}
	})

	t.Run("refused while an incident response is unfinished", func(t *testing.T) {
		fx := setupRevocationFixture(t, "handler-tests-unsuspend-running", "daemon-unsuspend-running")
		dbfx.Exec(t, `UPDATE "user" SET suspended_at = now() WHERE id = $1`, fx.TargetUserID)
		dbfx.Insert(t, "user_suspension_event", testutil.Cols{
			"user_id": fx.TargetUserID, "action": "incident_response", "actor": "ops",
			"status": "running", "report": `{"workspaces":{}}`,
		})
		if _, err := testHandler.UnsuspendAccount(ctx, accountRequest(fx)); !errors.Is(err, errAccountIncidentOn) {
			t.Fatalf("err = %v, want errAccountIncidentOn", err)
		}
	})
}

func TestAccountAdminHandler_RequiresOneAccountReference(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	body := `{"user_id":"00000000-0000-0000-0000-000000000001","email":"a@b.c","actor":"ops","reason":"r"}`
	req := httptest.NewRequest(http.MethodPost, "/maintenance/accounts/suspend", strings.NewReader(body))
	w := httptest.NewRecorder()
	testHandler.AccountAdminHandler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

// A suspended account keeps its member rows, so the daemon heartbeat's
// membership re-check alone would keep its runtime alive.
func TestHandleDaemonWSHeartbeat_SuspendedOwnerRuntimeIsGone(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := setupRevocationFixture(t, "handler-tests-ws-heartbeat-suspended", "daemon-ws-heartbeat-suspended")
	dbfx.Exec(t, `UPDATE "user" SET suspended_at = now() WHERE id = $1`, fx.TargetUserID)

	lease := daemonws.NewRuntimeLease(fx.WorkspaceID, "online", time.Now(), true)
	lease.MarkMembershipVerified(time.Now().Add(-2 * daemonWSMembershipRecheckInterval))
	ack, err := testHandler.HandleDaemonWSHeartbeat(context.Background(), daemonws.ClientIdentity{
		UserID:        fx.TargetUserID,
		WorkspaceIDs:  []string{fx.WorkspaceID},
		RuntimeIDs:    []string{fx.RuntimeID},
		RuntimeLeases: map[string]*daemonws.RuntimeLease{fx.RuntimeID: lease},
	}, fx.RuntimeID, protocol.DaemonHeartbeatRequestPayload{})
	if err != nil {
		t.Fatalf("HandleDaemonWSHeartbeat: %v", err)
	}
	if ack == nil || !ack.RuntimeGone {
		t.Fatalf("ack = %+v, want runtime_gone", ack)
	}
}

func TestSendCode_RefusesSuspendedAccount(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	const email = "suspended-send-code@multica.ai"
	userID := dbfx.Insert(t, "user", testutil.Cols{"name": "Suspended", "email": email, "suspended_at": testutil.Raw("now()")})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })

	req := httptest.NewRequest("POST", "/auth/send-code", strings.NewReader(`{"email":"`+email+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	testHandler.SendCode(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("SendCode: status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestIssueJWT_CarriesTokenVersion(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := setupRevocationFixture(t, "handler-tests-jwt-token-version", "daemon-jwt-token-version")
	dbfx.Exec(t, `UPDATE "user" SET token_version = 2 WHERE id = $1`, fx.TargetUserID)
	user, err := testHandler.Queries.GetUser(context.Background(), parseUUID(fx.TargetUserID))
	if err != nil {
		t.Fatal(err)
	}
	token, err := testHandler.issueJWT(user)
	if err != nil {
		t.Fatalf("issueJWT: %v", err)
	}
	claims, err := auth.ParseSessionToken(token)
	if err != nil {
		t.Fatalf("ParseSessionToken: %v", err)
	}
	if got := auth.SessionTokenVersion(claims); got != 2 {
		t.Fatalf("token version = %d, want 2", got)
	}

	dbfx.Exec(t, `UPDATE "user" SET suspended_at = now() WHERE id = $1`, fx.TargetUserID)
	user, _ = testHandler.Queries.GetUser(context.Background(), parseUUID(fx.TargetUserID))
	if _, err := testHandler.issueJWT(user); !errors.Is(err, auth.ErrTemporarilyDisabledUser) {
		t.Fatalf("suspended issueJWT err = %v, want ErrTemporarilyDisabledUser", err)
	}
}

// A suspended account keeps owning its agents; the owner shortcut must not
// let it invoke them.
func TestCanMemberInvokeAgent_RefusesSuspendedOwner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := setupRevocationFixture(t, "handler-tests-invoke-suspended", "daemon-invoke-suspended")
	ctx := context.Background()
	agent, err := testHandler.Queries.GetAgent(ctx, parseUUID(fx.AgentID))
	if err != nil {
		t.Fatal(err)
	}
	invoke := func() bool {
		return service.CanMemberInvokeAgent(ctx, testHandler.Queries, agent, parseUUID(fx.TargetUserID), parseUUID(fx.WorkspaceID))
	}
	if !invoke() {
		t.Fatal("owner cannot invoke their own agent")
	}
	dbfx.Exec(t, `UPDATE "user" SET suspended_at = now() WHERE id = $1`, fx.TargetUserID)
	if invoke() {
		t.Fatal("suspended owner can still invoke their agent")
	}
}
