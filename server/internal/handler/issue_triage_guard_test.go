package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Triage write protection (MUL-7212). A triage issue can only be made by
// Triage intake, which does not exist yet, so these tests insert one directly.

// The number comes from the workspace counter, not the fixture's MAX+1, so an
// HTTP create later in the same test cannot be handed the same number.
func triageIssueForTest(t *testing.T, title string) string {
	t.Helper()
	return dbfx.Issue(t, title, testutil.Cols{
		"status": issuestatus.Triage,
		"number": nextWorkspaceIssueNumber(t),
	})
}

func issueStatusOf(t *testing.T, issueID string) string {
	t.Helper()
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status)
	return status
}

func wantErrorCode(t *testing.T, resp *testutil.Response, code string) {
	t.Helper()
	if got := resp.Want(http.StatusBadRequest).Map()["code"]; got != code {
		t.Fatalf("error code = %v, want %q: %s", got, code, resp.Text())
	}
}

func TestIssueWritesRefuseTheReservedTriageStatus(t *testing.T) {
	seedTestCatalog(t)

	t.Run("create", func(t *testing.T) {
		for _, status := range []string{"triage", "  TRIAGE "} {
			resp := testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", map[string]any{
				"title": "create into triage", "status": status,
			}))
			wantErrorCode(t, resp, "status_reserved_for_triage")
		}
	})

	t.Run("update", func(t *testing.T) {
		issueID := dbfx.Issue(t, "update into triage")
		resp := testutil.Call(t, testHandler.UpdateIssue, withURLParam(
			newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{"status": "triage"}),
			"id", issueID))
		wantErrorCode(t, resp, "status_reserved_for_triage")
		if got := issueStatusOf(t, issueID); got != "todo" {
			t.Errorf("status after refused update = %q, want todo", got)
		}
	})

	t.Run("batch", func(t *testing.T) {
		issueID := dbfx.Issue(t, "batch into triage")
		resp := testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPatch,
			"/api/issues/batch?workspace_id="+testWorkspaceID, map[string]any{
				"issue_ids": []string{issueID},
				"updates":   map[string]any{"status": "triage"},
			}))
		wantErrorCode(t, resp, "status_reserved_for_triage")
		if got := issueStatusOf(t, issueID); got != "todo" {
			t.Errorf("status after refused batch = %q, want todo", got)
		}
	})

	// Every create entry shares the rule, not only the HTTP handler that
	// resolves the status first.
	t.Run("service create", func(t *testing.T) {
		_, err := testHandler.IssueService.Create(context.Background(), service.IssueCreateParams{
			WorkspaceID: parseUUID(testWorkspaceID),
			Title:       "service create into triage",
			Status:      issuestatus.Triage,
			Priority:    "none",
			CreatorType: "member",
			CreatorID:   parseUUID(testUserID),
		}, service.IssueCreateOpts{})
		if !errors.Is(err, service.ErrStatusReservedForTriage) {
			t.Fatalf("IssueService.Create(triage) = %v, want ErrStatusReservedForTriage", err)
		}
	})
}

// A custom status can neither take the key nor claim Triage as its category:
// either would give the catalog a row that resolves to the reserved status.
func TestCustomStatusCannotClaimTriage(t *testing.T) {
	seedTestCatalog(t)
	cases := map[string]map[string]any{
		"triage category":           {"name": "Foo", "key": "foo", "category": "triage", "color": "#123456"},
		"triage key":                {"name": "Mine", "key": "triage", "category": "todo", "color": "#123456"},
		"name slugging onto triage": {"name": "Triage", "category": "backlog", "color": "#123456"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			testutil.Call(t, testHandler.CreateIssueStatus,
				newRequest(http.MethodPost, "/api/issue-statuses", body)).Want(http.StatusBadRequest)
		})
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM issue_status WHERE workspace_id = $1 AND (key IN ('foo', 'triage') OR category = 'triage')`, testWorkspaceID); n != 0 {
		t.Fatalf("%d catalog row(s) claimed triage", n)
	}
}

func TestIssueInTriageKeepsStatusProjectAndParent(t *testing.T) {
	issueID := triageIssueForTest(t, "waiting in triage")
	projectID := dbfx.Project(t, "triage target project")
	parentID := dbfx.Issue(t, "triage target parent")

	update := func(body map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.UpdateIssue, withURLParam(
			newRequest(http.MethodPut, "/api/issues/"+issueID, body), "id", issueID))
	}

	for name, body := range map[string]map[string]any{
		"status":           {"status": "todo"},
		"status unchanged": {"status": "triage"},
		"project":          {"project_id": projectID},
		"clear project":    {"project_id": nil},
		"parent":           {"parent_issue_id": parentID},
		"status and title": {"status": "done", "title": "sneaky"},
	} {
		t.Run(name, func(t *testing.T) {
			wantErrorCode(t, update(body), "issue_in_triage")
		})
	}
	var status, title string
	var projectSet, parentSet bool
	dbfx.QueryRow(t, `SELECT status, title, project_id IS NOT NULL, parent_issue_id IS NOT NULL FROM issue WHERE id = $1`, issueID).
		Scan(&status, &title, &projectSet, &parentSet)
	if status != issuestatus.Triage || title != "waiting in triage" || projectSet || parentSet {
		t.Fatalf("refused writes changed the issue: status=%q title=%q project=%v parent=%v", status, title, projectSet, parentSet)
	}

	// Everything accept does not decide stays editable.
	var resp IssueResponse
	update(map[string]any{"priority": "high", "title": "retitled in triage"}).Want(http.StatusOK).JSON(&resp)
	if resp.Status != issuestatus.Triage || resp.Priority != "high" || resp.Title != "retitled in triage" {
		t.Fatalf("allowed update = {status:%q priority:%q title:%q}", resp.Status, resp.Priority, resp.Title)
	}
	// A reserved key is its own category, with no catalog read.
	if resp.StatusCategory != issuestatus.Triage {
		t.Errorf("status_category = %q, want triage", resp.StatusCategory)
	}
}

// A batch that would move an issue out of Triage is refused whole, before any
// write, rather than updating the rest and skipping it silently.
func TestBatchUpdateRefusesIssuesInTriage(t *testing.T) {
	triageID := triageIssueForTest(t, "batch triage member")
	todoID := dbfx.Issue(t, "batch todo member")

	batch := func(updates map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPatch,
			"/api/issues/batch?workspace_id="+testWorkspaceID, map[string]any{
				"issue_ids": []string{todoID, triageID},
				"updates":   updates,
			}))
	}

	wantErrorCode(t, batch(map[string]any{"status": "done"}), "issue_in_triage")
	wantErrorCode(t, batch(map[string]any{"project_id": nil}), "issue_in_triage")
	if got := issueStatusOf(t, todoID); got != "todo" {
		t.Errorf("refused batch still updated its other issue: status = %q", got)
	}

	var out struct {
		Updated int `json:"updated"`
	}
	batch(map[string]any{"priority": "urgent"}).Want(http.StatusOK).JSON(&out)
	if out.Updated != 2 {
		t.Errorf("priority batch updated %d issue(s), want 2", out.Updated)
	}
	if got := issueStatusOf(t, triageID); got != issuestatus.Triage {
		t.Errorf("triage issue status after priority batch = %q, want triage", got)
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
	if got := issueStatusOf(t, issueID); got != issuestatus.Triage {
		t.Fatalf("status after merged PR = %q, want triage", got)
	}
}

// Triage has no board column; in a status sort it leads, ahead of Backlog.
func TestStatusOrderRanksTriageFirst(t *testing.T) {
	rank := func(status string) int {
		var got int
		dbfx.QueryRow(t, `SELECT `+statusOrderExpression("$1::text"), status).Scan(&got)
		return got
	}
	if triage, backlog := rank(issuestatus.Triage), rank(issuestatus.Backlog); triage >= backlog {
		t.Fatalf("status rank triage=%d backlog=%d, want triage first", triage, backlog)
	}
}
