package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// useGuestsFeature points the shared handler at a stub that returns decision
// for the guests paid feature in the test workspace.
func useGuestsFeature(t *testing.T, decision entitlement.FeatureDecision) {
	t.Helper()
	stub := entitlementtest.New()
	stub.SetFeature(uuid.MustParse(testWorkspaceID), entitlement.FeatureGuests, decision)
	prior := testHandler.Features
	testHandler.Features = stub
	clearFeatureAttempts(t)
	t.Cleanup(func() {
		testHandler.Features = prior
		clearFeatureAttempts(t)
	})
}

func wantGuestsLocked(t *testing.T, resp *testutil.Response) {
	t.Helper()
	var body struct {
		Code    string `json:"code"`
		Feature string `json:"feature"`
	}
	resp.Want(http.StatusForbidden).JSON(&body)
	if body.Code != featureLockedCode || body.Feature != string(entitlement.FeatureGuests) {
		t.Fatalf("locked body = %+v, want feature_locked/guests", body)
	}
}

func guestAttempts(t *testing.T) featureAttemptCounts {
	t.Helper()
	var c featureAttemptCounts
	dbfx.QueryRow(t, `
		SELECT COALESCE(SUM(observed_count), 0), COALESCE(SUM(degraded_count), 0), COALESCE(SUM(enforced_count), 0),
		       COALESCE(MAX(last_operation), '')
		FROM entitlement_feature_attempt
		WHERE workspace_id = $1 AND feature = 'guests'`, testWorkspaceID,
	).Scan(&c.observed, &c.degraded, &c.enforced, &c.lastOperation)
	return c
}

// A plan without guests adds none: no guest invitation, no member made a
// guest, no project shared with a guest. Existing guests keep working and can
// be narrowed or made members.
func TestGuestsFeatureLockedAddsNoGuests(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	f := newGuestFixture(t)
	addProjectMemberRow(t, f.other, f.guest)
	useGuestsFeature(t, entitlement.FeatureDecision{Action: entitlement.ActionEnforce})

	invite := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members",
		CreateMemberRequest{Email: f.marker + "-locked@multica.test", Role: "guest", ProjectIDs: []string{f.shared}}), "id", testWorkspaceID)
	wantGuestsLocked(t, testutil.Call(t, testHandler.CreateInvitation, invite))
	if n := dbfx.Count(t, `SELECT count(*) FROM workspace_invitation WHERE workspace_id = $1 AND role = 'guest'`, testWorkspaceID); n != 0 {
		t.Fatalf("guest invitations = %d, want none", n)
	}

	member := createPlainMember(t, f.marker+"-member@multica.test")
	var memberID string
	dbfx.QueryRow(t, `SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, member).Scan(&memberID)
	demote := withURLParams(newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID+"/members/"+memberID,
		UpdateMemberRequest{Role: "guest", ProjectIDs: []string{f.shared}}), "id", testWorkspaceID, "memberId", memberID)
	wantGuestsLocked(t, testutil.Call(t, testHandler.UpdateMember, demote))

	path := "/api/workspaces/" + testWorkspaceID + "/members/" + f.guestMember
	widen := withURLParams(newRequest(http.MethodPut, path+"/projects",
		SetMemberProjectsRequest{ProjectIDs: []string{f.shared, f.other, f.secret}}), "id", testWorkspaceID, "memberId", f.guestMember)
	// The requester is a member of the private project, so sharing it is
	// allowed by permissions and refused only by the plan.
	wantGuestsLocked(t, testutil.Call(t, testHandler.SetMemberProjects, widen))

	narrow := withURLParams(newRequest(http.MethodPut, path+"/projects",
		SetMemberProjectsRequest{ProjectIDs: []string{f.shared}}), "id", testWorkspaceID, "memberId", f.guestMember)
	testutil.Call(t, testHandler.SetMemberProjects, narrow).Want(http.StatusOK)
	if got := guestProjectIDs(t, f.guest); len(got) != 1 || got[0] != f.shared {
		t.Fatalf("guest projects after narrowing = %v, want only shared", got)
	}

	// The existing guest still reads its project.
	rec := guestRequest(t, testHandler.GetIssue, f.guest, http.MethodGet, "/api/issues/"+f.sharedIssue, nil, "id", f.sharedIssue)
	if rec.Code != http.StatusOK {
		t.Fatalf("existing guest GET shared issue = %d, want 200", rec.Code)
	}

	promote := withURLParams(newRequest(http.MethodPatch, path, UpdateMemberRequest{Role: "member"}),
		"id", testWorkspaceID, "memberId", f.guestMember)
	testutil.Call(t, testHandler.UpdateMember, promote).Want(http.StatusOK)

	if got := guestAttempts(t); got.enforced != 3 || got.observed != 0 {
		t.Fatalf("guest attempts = %+v, want 3 enforced", got)
	}
}

// Under observe, guests are added as usual and each addition is recorded.
func TestGuestsFeatureObservedRecordsAdditions(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	f := newGuestFixture(t)
	useGuestsFeature(t, entitlement.FeatureDecision{Action: entitlement.ActionObserve})

	invite := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members",
		CreateMemberRequest{Email: f.marker + "-observed@multica.test", Role: "guest", ProjectIDs: []string{f.shared}}), "id", testWorkspaceID)
	testutil.Call(t, testHandler.CreateInvitation, invite).Want(http.StatusCreated)

	// Setting the same projects again adds nothing and records nothing.
	path := "/api/workspaces/" + testWorkspaceID + "/members/" + f.guestMember
	same := withURLParams(newRequest(http.MethodPut, path+"/projects",
		SetMemberProjectsRequest{ProjectIDs: []string{f.shared}}), "id", testWorkspaceID, "memberId", f.guestMember)
	testutil.Call(t, testHandler.SetMemberProjects, same).Want(http.StatusOK)

	if got := guestAttempts(t); got.observed != 1 || got.enforced != 0 || got.lastOperation != "invite" {
		t.Fatalf("guest attempts = %+v, want one observed invite", got)
	}
}

// A guest takes no member slot, so a full Free workspace still invites one;
// turning that guest into a member takes a slot and is refused while full.
func TestGuestPromotionRespectsMemberLimit(t *testing.T) {
	workspaceID := memberLimitWorkspace(t, 4)
	project := dbfx.Project(t, "Member limit guest project", testutil.Cols{"workspace_id": workspaceID})
	guest := dbfx.User(t, "Member limit guest", "member-limit-guest-"+uuid.NewString()[:8]+"@multica.test")
	guestMember := dbfx.Member(t, workspaceID, guest, "guest")
	useMemberLimit(t, workspaceID, entitlement.ActionEnforce, 5)

	req := newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/members",
		CreateMemberRequest{Email: "member-limit-guest-invitee-" + uuid.NewString()[:8] + "@multica.test", Role: "guest", ProjectIDs: []string{project}})
	req.Header.Set("X-Workspace-ID", workspaceID)
	testutil.Call(t, testHandler.CreateInvitation, withURLParam(req, "id", workspaceID)).Want(http.StatusCreated)

	promote := func() *testutil.Response {
		req := newRequest(http.MethodPatch, "/api/workspaces/"+workspaceID+"/members/"+guestMember, UpdateMemberRequest{Role: "member"})
		req.Header.Set("X-Workspace-ID", workspaceID)
		return testutil.Call(t, testHandler.UpdateMember, withURLParams(req, "id", workspaceID, "memberId", guestMember))
	}
	wantMemberLimitReached(t, promote())
	var role string
	dbfx.QueryRow(t, `SELECT role FROM member WHERE id = $1`, guestMember).Scan(&role)
	if role != "guest" {
		t.Fatalf("role after refused promotion = %q, want guest", role)
	}
	if o := memberLimitOverageOf(t, workspaceID); o.enforced != 1 {
		t.Fatalf("member limit overage = %+v, want one enforced attempt", o)
	}

	useMemberLimit(t, workspaceID, entitlement.ActionEnforce, 6)
	promote().Want(http.StatusOK)
}

// An attachment outside any issue (docs, chat) is hidden from a guest unless
// the guest uploaded it.
func TestGuestSeesOnlyItsOwnAttachmentsOutsideIssues(t *testing.T) {
	f := newGuestFixture(t)
	attachment := func(uploader string) string {
		return dbfx.Insert(t, "attachment", testutil.Cols{
			"workspace_id": testWorkspaceID, "uploader_type": "member", "uploader_id": uploader,
			"filename": "note.png", "url": "https://example.test/" + uuid.NewString() + ".png",
			"content_type": "image/png", "size_bytes": 1,
		})
	}
	others := attachment(testUserID)
	own := attachment(f.guest)

	for _, tc := range []struct {
		name, id string
		want     int
	}{
		{"someone else's", others, http.StatusNotFound},
		{"its own", own, http.StatusOK},
	} {
		rec := guestRequest(t, testHandler.GetAttachmentByID, f.guest, http.MethodGet, "/api/attachments/"+tc.id, nil, "id", tc.id)
		if rec.Code != tc.want {
			t.Errorf("guest GET %s attachment = %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
	member := createPlainMember(t, f.marker+"-reader@multica.test")
	rec := guestRequest(t, testHandler.GetAttachmentByID, member, http.MethodGet, "/api/attachments/"+others, nil, "id", others)
	if rec.Code != http.StatusOK {
		t.Errorf("member GET attachment = %d, want 200", rec.Code)
	}
}
