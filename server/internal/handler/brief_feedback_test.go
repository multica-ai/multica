package handler

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// publishFeedbackBrief publishes a brief in e's workspace as the default owner
// and returns its id.
func (e digestEnv) publishFeedbackBrief(t *testing.T) string {
	t.Helper()
	var brief BriefResponse
	e.publish(t, "", "", briefRequestBody(nil)).Want(http.StatusCreated).JSON(&brief)
	return brief.ID
}

// feedbackRequest builds a request on a brief's feedback path in e's workspace,
// as the given user, or as the default owner when userID is empty.
func (e digestEnv) feedbackRequest(t *testing.T, method, briefID, userID string, body map[string]any) *http.Request {
	t.Helper()
	req := withURLParam(newRequest(method, "/api/briefs/"+briefID+"/feedback", body), "id", briefID)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	if userID != "" {
		req = asUser(req, userID)
	}
	return req
}

func (e digestEnv) submitFeedback(t *testing.T, userID, briefID string, body map[string]any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.SubmitBriefFeedback, e.feedbackRequest(t, "POST", briefID, userID, body))
}

func (e digestEnv) myFeedback(t *testing.T, userID, briefID string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.GetMyBriefFeedback, e.feedbackRequest(t, "GET", briefID, userID, nil))
}

// workspaceRead calls one of the workspace-wide feedback reads. agentID, when
// set, makes the call as that agent; otherwise it is made as userID, or as the
// default owner when userID is empty.
func (e digestEnv) workspaceRead(t *testing.T, handler http.HandlerFunc, path, userID, agentID string) *testutil.Response {
	t.Helper()
	req := newRequest("GET", path, nil)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	switch {
	case agentID != "":
		taskID := dbfx.Task(t, agentID, testutil.Cols{"status": "running", "runtime_id": e.RuntimeID})
		req = asAgent(req, agentID, taskID)
	case userID != "":
		req = asUser(req, userID)
	}
	return testutil.Call(t, handler, req)
}

// insertRating writes a rating row directly, so a test can place it at a chosen
// updated_at.
func (e digestEnv) insertRating(t *testing.T, briefID, memberID, rating string, updatedAt time.Time) {
	t.Helper()
	dbfx.Insert(t, "brief_feedback", testutil.Cols{
		"workspace_id": e.WorkspaceID,
		"brief_id":     briefID,
		"member_id":    memberID,
		"rating":       rating,
		"tags":         []string{},
		"note":         "",
		"updated_at":   updatedAt,
	})
}

func TestBriefFeedback_SecondSubmissionReplacesTheFirst(t *testing.T) {
	e := newDigestEnv(t)
	briefID := e.publishFeedbackBrief(t)
	member := newWorkspaceMemberUserIn(t, e.WorkspaceID, "member")

	var first BriefFeedbackResponse
	e.submitFeedback(t, member, briefID, map[string]any{
		"rating": "useful", "tags": []string{"too_long"}, "note": "shorter please",
	}).Want(http.StatusOK).JSON(&first)
	if first.Rating != "useful" || len(first.Tags) != 1 || first.Tags[0] != "too_long" {
		t.Fatalf("first rating = %+v", first)
	}

	var second BriefFeedbackResponse
	e.submitFeedback(t, member, briefID, map[string]any{
		"rating": "not_useful", "tags": []string{"wrong", "too_vague", "wrong"}, "note": "check the figures",
	}).Want(http.StatusOK).JSON(&second)
	if second.ID != first.ID {
		t.Fatalf("a second submission must update the row, not add one: %s then %s", first.ID, second.ID)
	}
	if second.Rating != "not_useful" || strings.Join(second.Tags, ",") != "wrong,too_vague" || second.Note != "check the figures" {
		t.Fatalf("second rating = %+v, want not_useful with the deduplicated tags and the new note", second)
	}
	if second.CreatedAt != first.CreatedAt {
		t.Fatalf("created_at moved on resubmission: %s then %s", first.CreatedAt, second.CreatedAt)
	}

	var own BriefFeedbackResponse
	e.myFeedback(t, member, briefID).Want(http.StatusOK).JSON(&own)
	if own.ID != second.ID || own.Rating != "not_useful" || own.Note != "check the figures" {
		t.Fatalf("own rating = %+v, want the replacement", own)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM brief_feedback WHERE brief_id = $1 AND member_id = $2`, briefID, member); n != 1 {
		t.Fatalf("rows for one member and brief = %d, want 1", n)
	}
}

func TestBriefFeedback_RefusesUnknownTagsRatingsAndLongNotes(t *testing.T) {
	e := newDigestEnv(t)
	briefID := e.publishFeedbackBrief(t)
	member := newWorkspaceMemberUserIn(t, e.WorkspaceID, "member")

	unknown := e.submitFeedback(t, member, briefID, map[string]any{"rating": "useful", "tags": []string{"boring"}})
	unknown.Want(http.StatusBadRequest)
	if !strings.Contains(unknown.Text(), "unknown tag") || !strings.Contains(unknown.Text(), "boring") {
		t.Fatalf("refusal should name the unknown tag: %s", unknown.Text())
	}
	e.submitFeedback(t, member, briefID, map[string]any{"rating": "meh"}).Want(http.StatusBadRequest)
	e.submitFeedback(t, member, briefID, map[string]any{}).Want(http.StatusBadRequest)
	e.submitFeedback(t, member, briefID, map[string]any{
		"rating": "useful", "note": strings.Repeat("n", briefFeedbackMaxNote+1),
	}).Want(http.StatusBadRequest)

	if n := dbfx.Count(t, `SELECT count(*) FROM brief_feedback WHERE brief_id = $1`, briefID); n != 0 {
		t.Fatalf("refused submissions wrote %d rows", n)
	}
}

func TestBriefFeedback_OnlyMembersRateAndOnlyExistingBriefs(t *testing.T) {
	e := newDigestEnv(t)
	briefID := e.publishFeedbackBrief(t)

	// An agent, even the Secretary, has no rating of its own.
	e.setSecretary(t, e.AgentID)
	taskID := dbfx.Task(t, e.AgentID, testutil.Cols{"status": "running", "runtime_id": e.RuntimeID})
	req := asAgent(e.feedbackRequest(t, "POST", briefID, "", map[string]any{"rating": "useful"}), e.AgentID, taskID)
	testutil.Call(t, testHandler.SubmitBriefFeedback, req).Want(http.StatusForbidden)

	member := newWorkspaceMemberUserIn(t, e.WorkspaceID, "member")
	e.submitFeedback(t, member, uuid.NewString(), map[string]any{"rating": "useful"}).Want(http.StatusNotFound)
	e.myFeedback(t, member, uuid.NewString()).Want(http.StatusNotFound)
}

func TestBriefFeedback_OwnRatingOnly(t *testing.T) {
	e := newDigestEnv(t)
	briefID := e.publishFeedbackBrief(t)
	alice := newWorkspaceMemberUserIn(t, e.WorkspaceID, "member")
	bob := newWorkspaceMemberUserIn(t, e.WorkspaceID, "member")
	e.submitFeedback(t, alice, briefID, map[string]any{"rating": "useful"}).Want(http.StatusOK)

	// Bob has not rated the brief, so he reads null, not Alice's rating.
	if body := e.myFeedback(t, bob, briefID).Want(http.StatusOK).Text(); strings.TrimSpace(body) != "null" {
		t.Fatalf("bob's rating = %s, want null", body)
	}
	var mine BriefFeedbackResponse
	e.myFeedback(t, alice, briefID).Want(http.StatusOK).JSON(&mine)
	if mine.Rating != "useful" {
		t.Fatalf("alice's rating = %+v", mine)
	}
}

func TestBriefFeedback_WorkspaceReadsAreSecretaryAndAdminOnly(t *testing.T) {
	e := newDigestEnv(t)
	e.setSecretary(t, e.AgentID)
	briefID := e.publishFeedbackBrief(t)
	member := newWorkspaceMemberUserIn(t, e.WorkspaceID, "member")
	admin := newWorkspaceMemberUserIn(t, e.WorkspaceID, "admin")
	e.submitFeedback(t, member, briefID, map[string]any{"rating": "useful"}).Want(http.StatusOK)

	e.workspaceRead(t, testHandler.ListBriefFeedbackRecent, "/api/briefs/feedback/recent", member, "").Want(http.StatusForbidden)
	e.workspaceRead(t, testHandler.GetBriefFeedbackSummary, "/api/briefs/feedback/summary", member, "").Want(http.StatusForbidden)
	e.workspaceRead(t, testHandler.ListBriefFeedbackRecent, "/api/briefs/feedback/recent", "", e.otherAgent(t)).Want(http.StatusForbidden)

	e.workspaceRead(t, testHandler.ListBriefFeedbackRecent, "/api/briefs/feedback/recent", admin, "").Want(http.StatusOK)
	e.workspaceRead(t, testHandler.GetBriefFeedbackSummary, "/api/briefs/feedback/summary", admin, "").Want(http.StatusOK)
	e.workspaceRead(t, testHandler.ListBriefFeedbackRecent, "/api/briefs/feedback/recent", "", e.AgentID).Want(http.StatusOK)
	e.workspaceRead(t, testHandler.GetBriefFeedbackSummary, "/api/briefs/feedback/summary", "", e.AgentID).Want(http.StatusOK)
}

func TestBriefFeedback_RecentDaysAreBounded(t *testing.T) {
	e := newDigestEnv(t)
	for _, bad := range []string{"0", "36", "x"} {
		e.workspaceRead(t, testHandler.ListBriefFeedbackRecent, "/api/briefs/feedback/recent?days="+bad, "", "").Want(http.StatusBadRequest)
	}
	e.workspaceRead(t, testHandler.ListBriefFeedbackRecent, "/api/briefs/feedback/recent?days=35", "", "").Want(http.StatusOK)
}

func TestBriefFeedback_RecentListsTheWorkspaceWithBriefDates(t *testing.T) {
	mine := newDigestEnv(t)
	theirs := newDigestEnv(t)
	briefID := mine.publishFeedbackBrief(t)
	theirBrief := theirs.publishFeedbackBrief(t)
	member := newWorkspaceMemberUserIn(t, mine.WorkspaceID, "member")
	stranger := newWorkspaceMemberUserIn(t, theirs.WorkspaceID, "member")
	mine.submitFeedback(t, member, briefID, map[string]any{"rating": "not_useful", "tags": []string{"too_long"}, "note": "too long"}).Want(http.StatusOK)
	theirs.submitFeedback(t, stranger, theirBrief, map[string]any{"rating": "useful"}).Want(http.StatusOK)

	var entries []BriefFeedbackEntry
	mine.workspaceRead(t, testHandler.ListBriefFeedbackRecent, "/api/briefs/feedback/recent?days=14", "", "").Want(http.StatusOK).JSON(&entries)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want only this workspace's rating", entries)
	}
	got := entries[0]
	if got.BriefID != briefID || got.MemberID != member || got.Rating != "not_useful" || got.Note != "too long" {
		t.Fatalf("entry = %+v", got)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "too_long" || got.BriefKind != "daily" || got.BriefCreatedAt == "" {
		t.Fatalf("entry should carry tags and the brief's kind and date: %+v", got)
	}
}

func TestBriefFeedbackSummary_UsefulnessOverTheLast14Days(t *testing.T) {
	e := newDigestEnv(t)
	briefID := e.publishFeedbackBrief(t)
	now := time.Now()
	// Inside the window: two useful, one not useful, so 2/3.
	e.insertRating(t, briefID, newWorkspaceMemberUserIn(t, e.WorkspaceID, "member"), "useful", now.Add(-24*time.Hour))
	e.insertRating(t, briefID, newWorkspaceMemberUserIn(t, e.WorkspaceID, "member"), "useful", now.Add(-3*24*time.Hour))
	e.insertRating(t, briefID, newWorkspaceMemberUserIn(t, e.WorkspaceID, "member"), "not_useful", now.Add(-13*24*time.Hour))
	// Outside the window: neither counts.
	e.insertRating(t, briefID, newWorkspaceMemberUserIn(t, e.WorkspaceID, "member"), "useful", now.Add(-20*24*time.Hour))
	e.insertRating(t, briefID, newWorkspaceMemberUserIn(t, e.WorkspaceID, "member"), "not_useful", now.Add(-20*24*time.Hour))

	var sum BriefFeedbackSummary
	e.workspaceRead(t, testHandler.GetBriefFeedbackSummary, "/api/briefs/feedback/summary", "", "").Want(http.StatusOK).JSON(&sum)
	if sum.Days != 14 || sum.Ratings != 3 || sum.Useful != 2 {
		t.Fatalf("summary = %+v, want 14 days, 3 ratings, 2 useful", sum)
	}
	if sum.Usefulness == nil || *sum.Usefulness < 0.6666 || *sum.Usefulness > 0.6667 {
		t.Fatalf("usefulness = %v, want 2/3", sum.Usefulness)
	}
}

func TestBriefFeedbackSummary_NullWhenTheWindowIsEmpty(t *testing.T) {
	e := newDigestEnv(t)
	briefID := e.publishFeedbackBrief(t)
	// Only a rating outside the window: the window is still empty.
	e.insertRating(t, briefID, newWorkspaceMemberUserIn(t, e.WorkspaceID, "member"), "useful", time.Now().Add(-30*24*time.Hour))

	body := e.workspaceRead(t, testHandler.GetBriefFeedbackSummary, "/api/briefs/feedback/summary", "", "").Want(http.StatusOK).Text()
	if !strings.Contains(body, `"usefulness":null`) || !strings.Contains(body, `"ratings":0`) {
		t.Fatalf("empty window must read usefulness null with zero ratings, got %s", body)
	}
}

func TestBriefFeedbackSummary_IsolatedPerWorkspace(t *testing.T) {
	mine := newDigestEnv(t)
	theirs := newDigestEnv(t)
	briefID := mine.publishFeedbackBrief(t)
	theirBrief := theirs.publishFeedbackBrief(t)
	mine.insertRating(t, briefID, newWorkspaceMemberUserIn(t, mine.WorkspaceID, "member"), "useful", time.Now())
	theirs.insertRating(t, theirBrief, newWorkspaceMemberUserIn(t, theirs.WorkspaceID, "member"), "not_useful", time.Now())
	theirs.insertRating(t, theirBrief, newWorkspaceMemberUserIn(t, theirs.WorkspaceID, "member"), "not_useful", time.Now())

	var sum BriefFeedbackSummary
	mine.workspaceRead(t, testHandler.GetBriefFeedbackSummary, "/api/briefs/feedback/summary", "", "").Want(http.StatusOK).JSON(&sum)
	if sum.Ratings != 1 || sum.Useful != 1 || sum.Usefulness == nil || *sum.Usefulness != 1 {
		t.Fatalf("summary = %+v, want only this workspace's one useful rating", sum)
	}
}
