package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// memberLimitWorkspace is a workspace of its own, so its member and
// invitation counts belong to the test alone. The test user owns it, and
// members adds that many more plain members.
func memberLimitWorkspace(t *testing.T, members int) string {
	t.Helper()
	suffix := uuid.NewString()[:8]
	workspaceID := dbfx.Workspace(t, "Member limit "+suffix, "member-limit-"+suffix)
	dbfx.Cleanup(t, `DELETE FROM workspace_invitation WHERE workspace_id = $1`, workspaceID)
	dbfx.Cleanup(t, `DELETE FROM workspace_share_link WHERE workspace_id = $1`, workspaceID)
	dbfx.Cleanup(t, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
	dbfx.Cleanup(t, `DELETE FROM entitlement_limit_overage WHERE workspace_id = $1`, workspaceID)
	dbfx.Member(t, workspaceID, testUserID, "owner")
	for i := 0; i < members; i++ {
		userID := dbfx.User(t, fmt.Sprintf("Member limit %s %d", suffix, i), fmt.Sprintf("member-limit-%s-%d@multica.ai", suffix, i))
		dbfx.Member(t, workspaceID, userID, "member")
	}
	return workspaceID
}

// useMemberLimit points the shared handler at a stub whose members gate in
// workspaceID has action and limit.
func useMemberLimit(t *testing.T, workspaceID string, action entitlement.Action, limit int) *entitlementtest.Stub {
	t.Helper()
	stub := entitlementtest.New()
	stub.Set(uuid.MustParse(workspaceID), entitlement.GateMembers, memberLimitDecision(action, limit))
	prior := testHandler.Entitlements
	testHandler.Entitlements = stub
	t.Cleanup(func() { testHandler.Entitlements = prior })
	return stub
}

func memberLimitDecision(action entitlement.Action, limit int) entitlement.Decision {
	gate := entitlement.Gate{Action: action}
	if action != entitlement.ActionOff {
		gate.Limit = &limit
	}
	return entitlement.Decision{Gate: gate}
}

func pendingInvitation(t *testing.T, workspaceID, email, userID string) string {
	t.Helper()
	cols := testutil.Cols{
		"workspace_id":  workspaceID,
		"inviter_id":    testUserID,
		"invitee_email": email,
		"role":          "member",
		"status":        "pending",
		"expires_at":    testutil.Raw("now() + interval '1 day'"),
	}
	if userID != "" {
		cols["invitee_user_id"] = userID
	}
	return dbfx.Insert(t, "workspace_invitation", cols)
}

type memberLimitOverage struct {
	limit, peak, observed, enforced int64
}

func memberLimitOverageOf(t *testing.T, workspaceID string) memberLimitOverage {
	t.Helper()
	var o memberLimitOverage
	dbfx.QueryRow(t, `
		SELECT COALESCE(MAX(limit_value), 0), COALESCE(MAX(peak_usage), 0),
		       COALESCE(SUM(observed_count), 0), COALESCE(SUM(enforced_count), 0)
		  FROM entitlement_limit_overage
		 WHERE workspace_id = $1 AND gate = 'members'`, workspaceID).
		Scan(&o.limit, &o.peak, &o.observed, &o.enforced)
	return o
}

func wantMemberLimitReached(t *testing.T, resp *testutil.Response) {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	resp.Want(http.StatusForbidden).JSON(&body)
	if body.Code != memberLimitReachedCode {
		t.Fatalf("code = %q, want %q", body.Code, memberLimitReachedCode)
	}
}

func inviteToWorkspace(t *testing.T, workspaceID, email string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/members", CreateMemberRequest{Email: email, Role: "member"})
	req.Header.Set("X-Workspace-ID", workspaceID)
	return testutil.Call(t, testHandler.CreateInvitation, withURLParam(req, "id", workspaceID))
}

func invitationCount(t *testing.T, workspaceID string) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM workspace_invitation WHERE workspace_id = $1`, workspaceID)
}

// A full workspace (owner + 4 members under a limit of 5) is handled by the
// gate's action: off admits without recording, observe admits and records,
// enforce refuses and records without writing the invitation.
func TestCreateInvitationUnderEachMemberLimitAction(t *testing.T) {
	for _, tc := range []struct {
		action entitlement.Action
		want   int
		record memberLimitOverage
	}{
		{entitlement.ActionOff, http.StatusCreated, memberLimitOverage{}},
		{entitlement.ActionObserve, http.StatusCreated, memberLimitOverage{limit: 5, peak: 6, observed: 1}},
		{entitlement.ActionEnforce, http.StatusForbidden, memberLimitOverage{limit: 5, peak: 6, enforced: 1}},
	} {
		t.Run(string(tc.action), func(t *testing.T) {
			workspaceID := memberLimitWorkspace(t, 4)
			useMemberLimit(t, workspaceID, tc.action, 5)

			resp := inviteToWorkspace(t, workspaceID, "sixth-"+uuid.NewString()+"@multica.ai")
			if tc.want == http.StatusForbidden {
				wantMemberLimitReached(t, resp)
				if n := invitationCount(t, workspaceID); n != 0 {
					t.Fatalf("refused invitation wrote %d rows", n)
				}
			} else {
				resp.Want(tc.want)
			}
			if got := memberLimitOverageOf(t, workspaceID); got != tc.record {
				t.Fatalf("overage = %+v, want %+v", got, tc.record)
			}
		})
	}
}

func TestCreateInvitationWithinMemberLimitRecordsNothing(t *testing.T) {
	workspaceID := memberLimitWorkspace(t, 3)
	useMemberLimit(t, workspaceID, entitlement.ActionEnforce, 5)

	inviteToWorkspace(t, workspaceID, "fifth-"+uuid.NewString()+"@multica.ai").Want(http.StatusCreated)
	if got := memberLimitOverageOf(t, workspaceID); got != (memberLimitOverage{}) {
		t.Fatalf("overage = %+v, want none", got)
	}
}

// A pending invitation already holds a slot, so it counts against a new one.
func TestCreateInvitationCountsPendingInvitations(t *testing.T) {
	workspaceID := memberLimitWorkspace(t, 3)
	pendingInvitation(t, workspaceID, "pending-"+uuid.NewString()+"@multica.ai", "")
	useMemberLimit(t, workspaceID, entitlement.ActionEnforce, 5)

	wantMemberLimitReached(t, inviteToWorkspace(t, workspaceID, "sixth-"+uuid.NewString()+"@multica.ai"))
}

// Accepting takes the slot the invitation holds: 4 members plus this
// invitation fit a limit of 5 even though another invitation is pending, and
// a refusal leaves the invitation pending for after an upgrade.
func TestAcceptInvitationTakesTheSlotItHolds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		members int
		want    int
	}{
		{"fits", 3, http.StatusOK},
		{"full", 4, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspaceID := memberLimitWorkspace(t, tc.members)
			email := "invitee-" + uuid.NewString() + "@multica.ai"
			userID := dbfx.User(t, "Member limit invitee", email)
			invitationID := pendingInvitation(t, workspaceID, email, userID)
			pendingInvitation(t, workspaceID, "other-"+uuid.NewString()+"@multica.ai", "")
			useMemberLimit(t, workspaceID, entitlement.ActionEnforce, 5)

			req := newRequest(http.MethodPost, "/api/invitations/"+invitationID+"/accept", nil)
			req.Header.Set("X-User-ID", userID)
			resp := testutil.Call(t, testHandler.AcceptInvitation, withURLParam(req, "id", invitationID))
			if tc.want == http.StatusOK {
				resp.Want(http.StatusOK)
				return
			}
			wantMemberLimitReached(t, resp)
			if status, _ := invitationStatusOf(t, invitationID); status != "pending" {
				t.Fatalf("invitation status = %q, want pending", status)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID); n != 0 {
				t.Fatalf("refused accept created %d members", n)
			}
		})
	}
}

func joinWorkspaceByLink(t *testing.T, code, userID string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/share-links/join", JoinByShareLinkRequest{Code: code})
	req.Header.Set("X-User-ID", userID)
	return testutil.Call(t, testHandler.JoinByShareLink, req)
}

func TestJoinShareLinkRefusedWhenWorkspaceIsFull(t *testing.T) {
	workspaceID := memberLimitWorkspace(t, 4)
	link := createTestShareLink(t, workspaceID, "member", 5, 0)
	userID := dbfx.User(t, "Member limit joiner", "joiner-"+uuid.NewString()+"@multica.ai")
	useMemberLimit(t, workspaceID, entitlement.ActionEnforce, 5)

	wantMemberLimitReached(t, joinWorkspaceByLink(t, link.Code, userID))
	if uses := dbfx.Count(t, `SELECT use_count FROM workspace_share_link WHERE id = $1`, link.ID); uses != 0 {
		t.Fatalf("refused join used the link %d times", uses)
	}
	if got := memberLimitOverageOf(t, workspaceID); got.enforced != 1 || got.peak != 6 {
		t.Fatalf("overage = %+v, want one enforced attempt at 6", got)
	}
}

// A joiner who was also invited by email takes the slot that invitation
// holds instead of a new one.
func TestJoinShareLinkTakesTheJoinersInvitationSlot(t *testing.T) {
	workspaceID := memberLimitWorkspace(t, 3)
	link := createTestShareLink(t, workspaceID, "member", 5, 0)
	email := "invited-joiner-" + uuid.NewString() + "@multica.ai"
	userID := dbfx.User(t, "Member limit invited joiner", email)
	invitationID := pendingInvitation(t, workspaceID, email, userID)
	pendingInvitation(t, workspaceID, "other-"+uuid.NewString()+"@multica.ai", "")
	useMemberLimit(t, workspaceID, entitlement.ActionEnforce, 5)

	joinWorkspaceByLink(t, link.Code, userID).Want(http.StatusOK)
	if status, _ := invitationStatusOf(t, invitationID); status != "accepted" {
		t.Fatalf("invitation status = %q, want accepted", status)
	}
}

// A workspace that has just upgraded must not be refused on the policy cached
// before the upgrade: the handler re-reads it before refusing.
func TestMemberLimitRefreshesPolicyBeforeRefusing(t *testing.T) {
	workspaceID := memberLimitWorkspace(t, 4)
	stub := useMemberLimit(t, workspaceID, entitlement.ActionEnforce, 5)
	stub.SetRefreshed(uuid.MustParse(workspaceID), entitlement.GateMembers, memberLimitDecision(entitlement.ActionOff, 0))

	inviteToWorkspace(t, workspaceID, "after-upgrade-"+uuid.NewString()+"@multica.ai").Want(http.StatusCreated)
	if refreshes := stub.Refreshes(); len(refreshes) != 1 || refreshes[0].Gate != entitlement.GateMembers {
		t.Fatalf("refreshes = %+v, want one members refresh", refreshes)
	}
	if got := memberLimitOverageOf(t, workspaceID); got != (memberLimitOverage{}) {
		t.Fatalf("overage = %+v, want none after the refreshed policy admitted it", got)
	}
}

// Two invitations racing for the last slot both pass the early check; the
// count under the workspace's admission lock lets exactly one through.
func TestConcurrentInvitationsTakeTheLastSlotOnce(t *testing.T) {
	workspaceID := memberLimitWorkspace(t, 3)
	useMemberLimit(t, workspaceID, entitlement.ActionEnforce, 5)

	codes := make([]int, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			req := newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/members", CreateMemberRequest{
				Email: fmt.Sprintf("race-%d-%s@multica.ai", i, uuid.NewString()), Role: "member",
			})
			req.Header.Set("X-Workspace-ID", workspaceID)
			rec := httptest.NewRecorder()
			testHandler.CreateInvitation(rec, withURLParam(req, "id", workspaceID))
			codes[i] = rec.Code
		}(i)
	}
	close(start)
	wg.Wait()

	created, refused := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusForbidden:
			refused++
		}
	}
	if created != 1 || refused != 1 {
		t.Fatalf("codes = %v, want one 201 and one 403", codes)
	}
	if n := invitationCount(t, workspaceID); n != 1 {
		t.Fatalf("invitations = %d, want 1", n)
	}
}

func TestGetMemberLimit(t *testing.T) {
	workspaceID := memberLimitWorkspace(t, 2)
	pendingInvitation(t, workspaceID, "pending-"+uuid.NewString()+"@multica.ai", "")

	get := func() MemberLimitResponse {
		req := newRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/member-limit", nil)
		req.Header.Set("X-Workspace-ID", workspaceID)
		var resp MemberLimitResponse
		testutil.Call(t, testHandler.GetMemberLimit, withURLParam(req, "id", workspaceID)).Want(http.StatusOK).JSON(&resp)
		return resp
	}

	if off := get(); off.Action != "off" || off.Limit != nil || off.Members != nil {
		t.Fatalf("without a provider = %+v, want off without counts", off)
	}
	useMemberLimit(t, workspaceID, entitlement.ActionObserve, 5)
	got := get()
	if got.Action != "observe" || got.Limit == nil || *got.Limit != 5 ||
		got.Members == nil || *got.Members != 3 || got.PendingInvitations == nil || *got.PendingInvitations != 1 {
		t.Fatalf("observed = %+v, want limit 5 with 3 members and 1 pending invitation", got)
	}
}

// A workspace that has just upgraded must not be reported full on the policy
// cached before the upgrade: the client would keep offering the upgrade.
func TestGetMemberLimitRefreshesBeforeReportingFull(t *testing.T) {
	workspaceID := memberLimitWorkspace(t, 4)
	stub := useMemberLimit(t, workspaceID, entitlement.ActionEnforce, 5)
	stub.SetRefreshed(uuid.MustParse(workspaceID), entitlement.GateMembers, memberLimitDecision(entitlement.ActionOff, 0))

	req := newRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/member-limit", nil)
	req.Header.Set("X-Workspace-ID", workspaceID)
	var resp MemberLimitResponse
	testutil.Call(t, testHandler.GetMemberLimit, withURLParam(req, "id", workspaceID)).Want(http.StatusOK).JSON(&resp)
	if resp.Action != "off" || len(stub.Refreshes()) != 1 {
		t.Fatalf("resp = %+v, refreshes = %d; want the refreshed off policy", resp, len(stub.Refreshes()))
	}
}

// Within the server's five-second bound on re-reading Cloud, a refusal can
// still rest on the Free policy read just before payment. The held invitation
// survives that (the client retries after the bound); once the refreshed
// policy arrives the same invitation goes through.
func TestHeldInvitationGoesThroughOnceThePolicyCatchesUp(t *testing.T) {
	workspaceID := memberLimitWorkspace(t, 4)
	stub := useMemberLimit(t, workspaceID, entitlement.ActionEnforce, 5)
	email := "held-" + uuid.NewString() + "@multica.ai"

	wantMemberLimitReached(t, inviteToWorkspace(t, workspaceID, email))

	stub.SetRefreshed(uuid.MustParse(workspaceID), entitlement.GateMembers, memberLimitDecision(entitlement.ActionOff, 0))
	inviteToWorkspace(t, workspaceID, email).Want(http.StatusCreated)
}

func TestMemberLimitPeriodIsTheUTCMonth(t *testing.T) {
	start, end := memberLimitPeriod(time.Date(2026, 12, 31, 23, 30, 0, 0, time.FixedZone("UTC-5", -5*3600)))
	if !start.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) || !end.Equal(time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("period = %s..%s", start, end)
	}
}
