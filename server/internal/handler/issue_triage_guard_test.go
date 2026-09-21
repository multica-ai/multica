package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Write protection for an issue in Triage (MUL-7189 §2.2).
//
// Triage lives in issue.triage_state, not in the status, so the only rule left
// here is the one parent field a triager may not set. A Triage entry can only
// be made by Triage intake, which does not exist yet, so these tests set the
// column directly.

// The number comes from the workspace counter, not the fixture's MAX+1, so an
// HTTP create later in the same test cannot be handed the same number.
func triageIssueForTest(t *testing.T, title string) string {
	t.Helper()
	return dbfx.Issue(t, title, testutil.Cols{
		"triage_state": "pending",
		"number":       nextWorkspaceIssueNumber(t),
	})
}

func issueStatusOf(t *testing.T, issueID string) string {
	t.Helper()
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status)
	return status
}

func triageStateOf(t *testing.T, issueID string) string {
	t.Helper()
	var state pgtype.Text
	dbfx.QueryRow(t, `SELECT triage_state FROM issue WHERE id = $1`, issueID).Scan(&state)
	return state.String
}

func wantErrorCode(t *testing.T, resp *testutil.Response, code string) {
	t.Helper()
	if got := resp.Want(http.StatusBadRequest).Map()["code"]; got != code {
		t.Fatalf("error code = %v, want %q: %s", got, code, resp.Text())
	}
}

// `triage` is an ordinary status key again (MUL-7400). It was reserved while
// the server read `status = 'triage'` as "in Triage"; since Triage moved to its
// own column nothing reads the key that way, so a workspace may name a custom
// status with it and issues may be moved onto it like any other.
func TestTriageIsAnOrdinaryCustomStatusKey(t *testing.T) {
	seedTestCatalog(t)

	var created IssueStatusResponse
	testutil.Call(t, testHandler.CreateIssueStatus, newRequest(http.MethodPost, "/api/issue-statuses", map[string]any{
		"name": "Triage", "category": "unstarted", "color": "#123456",
	})).Want(http.StatusCreated).JSON(&created)
	if created.Key != "triage" {
		t.Fatalf("custom status key = %q, want triage — the name no longer needs disambiguating", created.Key)
	}
	dbfx.Cleanup(t, `DELETE FROM issue_status WHERE id = $1`, parseUUID(created.ID))

	issueID := dbfx.Issue(t, "moved onto the custom triage status")
	var updated IssueResponse
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(
		newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{"status": "triage"}),
		"id", issueID)).Want(http.StatusOK).JSON(&updated)
	if updated.Status != "triage" {
		t.Fatalf("status after update = %q, want triage", updated.Status)
	}
	if got := issueStatusOf(t, issueID); got != "triage" {
		t.Fatalf("stored status = %q, want triage", got)
	}
	// The status write is not Triage intake: the marker stays untouched.
	if got := triageStateOf(t, issueID); got != "" {
		t.Fatalf("triage_state after a status write = %q, want empty", got)
	}
}

// Status, project and parent on a Triage entry are the triager's proposal, and
// accept is what confirms them — so ordinary writes reach them like any other
// issue. What no write can reach is the Triage marker itself: it is not a field
// of the issue API, so there is nothing to lock.
func TestIssueInTriageAcceptsOrdinaryFieldWrites(t *testing.T) {
	seedTestCatalog(t)
	issueID := triageIssueForTest(t, "waiting in triage")
	projectID := dbfx.Project(t, "triage target project")

	var resp IssueResponse
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(
		newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
			"status": "in_progress", "project_id": projectID, "priority": "high",
		}), "id", issueID)).Want(http.StatusOK).JSON(&resp)
	if resp.Status != "in_progress" || resp.Priority != "high" {
		t.Fatalf("proposal write = {status:%q priority:%q}, want it applied", resp.Status, resp.Priority)
	}
	// The write moved the proposal, not the entry: it is still in Triage.
	if got := triageStateOf(t, issueID); got != "pending" {
		t.Fatalf("triage_state after an ordinary write = %q, want pending", got)
	}
}

// The parent is the one field held back. A Triage child is never terminal, so
// it would wedge stageBarrierClosed and land in ChildIssueProgress's
// denominator — both reached through parent_issue_id, so the write is where it
// has to be stopped.
func TestIssueInTriageRefusesAParent(t *testing.T) {
	seedTestCatalog(t)
	issueID := triageIssueForTest(t, "must not become a child")
	parentID := dbfx.Issue(t, "triage target parent")

	update := func(body map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.UpdateIssue, withURLParam(
			newRequest(http.MethodPut, "/api/issues/"+issueID, body), "id", issueID))
	}
	// Both directions count as writing it: null clears, which is equally a
	// parent decision the triager does not get to make.
	for name, body := range map[string]map[string]any{
		"set":                  {"parent_issue_id": parentID},
		"clear":                {"parent_issue_id": nil},
		"alongside a proposal": {"parent_issue_id": parentID, "priority": "high"},
	} {
		t.Run(name, func(t *testing.T) {
			wantErrorCode(t, update(body), "issue_in_triage")
		})
	}
	var parentSet bool
	var priority string
	dbfx.QueryRow(t, `SELECT parent_issue_id IS NOT NULL, priority FROM issue WHERE id = $1`, issueID).Scan(&parentSet, &priority)
	if parentSet {
		t.Fatal("refused write still set a parent")
	}
	if priority == "high" {
		t.Fatal("refused write applied the proposal it was carrying alongside")
	}
}

// The batch is refused whole, before any write, rather than updating the rest
// and skipping the Triage entry with a short count and no reason.
func TestBatchUpdateRefusesAParentForIssuesInTriage(t *testing.T) {
	seedTestCatalog(t)
	triageID := triageIssueForTest(t, "batch triage member")
	todoID := dbfx.Issue(t, "batch todo member")
	parentID := dbfx.Issue(t, "batch target parent")

	batch := func(updates map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPatch,
			"/api/issues/batch?workspace_id="+testWorkspaceID, map[string]any{
				"issue_ids": []string{todoID, triageID},
				"updates":   updates,
			}))
	}

	wantErrorCode(t, batch(map[string]any{"parent_issue_id": parentID}), "issue_in_triage")
	if n := dbfx.Count(t, `SELECT count(*) FROM issue WHERE id = $1 AND parent_issue_id IS NOT NULL`, todoID); n != 0 {
		t.Error("refused batch still re-parented its other issue")
	}

	// Everything else in a batch still reaches a Triage entry: the lock is one
	// field, not the whole row.
	var out struct {
		Updated int `json:"updated"`
	}
	batch(map[string]any{"priority": "urgent"}).Want(http.StatusOK).JSON(&out)
	if out.Updated != 2 {
		t.Errorf("priority batch updated %d issue(s), want 2", out.Updated)
	}
	if got := triageStateOf(t, triageID); got != "pending" {
		t.Errorf("triage_state after priority batch = %q, want pending", got)
	}
}

// An item waiting in Triage has not been taken on, so it must not block anyone
// filing the same work by hand.
func TestTriageIssueIsNotAnActiveDuplicate(t *testing.T) {
	triageIssueForTest(t, "Duplicate guard ignores triage")
	var created IssueResponse
	testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", map[string]any{
		"title": "Duplicate guard ignores triage",
	})).Want(http.StatusCreated).JSON(&created)
	dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, parseUUID(created.ID))
}

// A merged "Closes" PR links to a triage issue but must not move it out.
func TestPullRequestMergeDoesNotAdvanceTriageIssue(t *testing.T) {
	issueID := triageIssueForTest(t, "closed by a PR while in triage")
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	testHandler.advanceIssueToDone(context.Background(), issue, testWorkspaceID)
	if got := issueStatusOf(t, issueID); got == "done" {
		t.Fatalf("merged PR moved a Triage entry to done")
	}
	if got := triageStateOf(t, issueID); got != "pending" {
		t.Fatalf("triage_state after merged PR = %q, want pending", got)
	}
}
