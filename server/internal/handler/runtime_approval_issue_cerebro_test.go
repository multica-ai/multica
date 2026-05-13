// CEREBRO-PATCH(runtime-approval-issue-test): JEH-1152 — coverage for the
// approval-issue side-effect attached to cerebroRequireRuntimeAccess. Two
// integration scenarios: (1) first denial auto-creates an issue, (2) a
// second denial reuses the existing open issue (no duplicate).
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// runRuntimeAccessDeniedScenario demotes the test user to member, plugs in a
// stub GroupPermissions that denies the runtime check, fires CreateAgent,
// and returns the decoded 403 body for assertions. Restores the owner role
// and unwires the stub on cleanup so other tests aren't affected.
func runRuntimeAccessDeniedScenario(t *testing.T, payload map[string]any) runtimeAccessDeniedBody {
	t.Helper()
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	if _, err := testPool.Exec(ctx,
		`UPDATE member SET role = 'member' WHERE workspace_id = $1 AND user_id = $2`,
		testWorkspaceID, testUserID,
	); err != nil {
		t.Fatalf("demote to member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx,
			`UPDATE member SET role = 'owner' WHERE workspace_id = $1 AND user_id = $2`,
			testWorkspaceID, testUserID,
		)
	})

	prev := testHandler.GroupPermissions
	testHandler.GroupPermissions = &stubGroupPermissions{
		canAG: func(context.Context, GroupPermissionsViewer, pgtype.UUID) (bool, error) {
			// create_agent capability must pass so the request reaches the
			// runtime gate — otherwise we'd 403 at the capability layer
			// with the old "no group grants this capability" message.
			return true, nil
		},
		canUseR: func(context.Context, GroupPermissionsViewer, pgtype.UUID) (bool, error) {
			return false, nil
		},
	}
	t.Cleanup(func() { testHandler.GroupPermissions = prev })

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", payload))
	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateAgent: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	var body runtimeAccessDeniedBody
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode 403 body: %v", err)
	}
	return body
}

// countOpenApprovalIssues returns how many open approval issues exist for the
// (workspace, runtime) pair. Used by both tests to assert the dedupe rule.
func countOpenApprovalIssues(t *testing.T, runtimeID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM issue
		WHERE workspace_id = $1
		  AND origin_type = 'runtime_approval'
		  AND origin_id = $2
		  AND status NOT IN ('done', 'cancelled')
	`, testWorkspaceID, runtimeID).Scan(&n); err != nil {
		t.Fatalf("count approval issues: %v", err)
	}
	return n
}

func TestCreateAgent_RuntimeAccessDenied_CreatesApprovalIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)
	// Clean up issues we create so re-runs of the test aren't affected by
	// the dedupe rule.
	t.Cleanup(func() {
		testPool.Exec(ctx,
			`DELETE FROM issue WHERE workspace_id = $1 AND origin_type = 'runtime_approval'`,
			testWorkspaceID,
		)
	})

	body := runRuntimeAccessDeniedScenario(t, map[string]any{
		"name":                 "agent-needs-approval",
		"description":          "blocked on runtime approval",
		"runtime_id":           runtimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	})

	if body.Code != runtimeAccessDeniedCode {
		t.Errorf("code: want %q, got %q", runtimeAccessDeniedCode, body.Code)
	}
	if !strings.Contains(body.Error, "must be approved") {
		t.Errorf("error message: expected new copy, got %q", body.Error)
	}
	if body.ApprovalIssue == nil {
		t.Fatalf("approval_issue: expected non-nil ref on 403 body, got nil")
	}
	if body.ApprovalIssue.ID == "" || body.ApprovalIssue.Identifier == "" {
		t.Errorf("approval_issue: ID and identifier must be set, got %+v", body.ApprovalIssue)
	}
	if !strings.HasPrefix(body.ApprovalIssue.Title, "Approve runtime:") {
		t.Errorf("approval_issue title: expected \"Approve runtime: ...\", got %q", body.ApprovalIssue.Title)
	}

	if got := countOpenApprovalIssues(t, runtimeID); got != 1 {
		t.Errorf("approval issues in DB: want 1, got %d", got)
	}

	// Subscriber check: the workspace owner (testUserID, which we demoted
	// just for the duration of this test but who is still listed as a
	// member) must be present as a subscriber so the requester sees the
	// follow-up. The fixture has a single workspace member so the admin
	// subscriber list is empty — the requester subscription is the one we
	// can assert on.
	var subscribers int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM issue_subscriber
		WHERE issue_id = $1 AND user_type = 'member' AND user_id = $2
	`, body.ApprovalIssue.ID, testUserID).Scan(&subscribers); err != nil {
		t.Fatalf("count subscribers: %v", err)
	}
	if subscribers != 1 {
		t.Errorf("requester subscribers: want 1, got %d", subscribers)
	}
}

func TestCreateAgent_RuntimeAccessDenied_ReusesExistingApprovalIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)
	t.Cleanup(func() {
		testPool.Exec(ctx,
			`DELETE FROM issue WHERE workspace_id = $1 AND origin_type = 'runtime_approval'`,
			testWorkspaceID,
		)
	})

	// First denial creates the issue.
	first := runRuntimeAccessDeniedScenario(t, map[string]any{
		"name":                 "agent-A-needs-approval",
		"description":          "",
		"runtime_id":           runtimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	})
	if first.ApprovalIssue == nil {
		t.Fatalf("first denial must produce an approval issue ref")
	}
	firstIssueID := first.ApprovalIssue.ID

	// Second denial — different agent name, same runtime — must reuse the
	// existing issue instead of creating a duplicate.
	second := runRuntimeAccessDeniedScenario(t, map[string]any{
		"name":                 "agent-B-needs-approval",
		"description":          "",
		"runtime_id":           runtimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	})
	if second.ApprovalIssue == nil {
		t.Fatalf("second denial must still produce an approval issue ref")
	}
	if second.ApprovalIssue.ID != firstIssueID {
		t.Errorf("expected reuse of issue %s, got %s", firstIssueID, second.ApprovalIssue.ID)
	}

	if got := countOpenApprovalIssues(t, runtimeID); got != 1 {
		t.Errorf("open approval issues after dedupe: want 1, got %d", got)
	}
}
