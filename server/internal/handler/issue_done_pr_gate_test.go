package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestManualDoneTransitionRequiresTerminalWorkingPRs pins the manual half of
// the PR close contract. Webhook auto-advance already waits for every visible
// working PR to leave open/draft; a human, agent, or batch status write must
// not be able to bypass that same gate.
func TestManualDoneTransitionRequiresTerminalWorkingPRs(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "manual-done-pr-gate-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	create := func(title string) IssueResponse {
		t.Helper()
		w := testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title": title, "status": "in_progress",
		})).Want(http.StatusCreated)
		var issue IssueResponse
		if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
			t.Fatalf("decode created issue: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, parseUUID(issue.ID))
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, parseUUID(issue.ID))
		})
		return issue
	}
	updateStatus := func(issue IssueResponse, status string, description bool) *testutil.Response {
		t.Helper()
		body := map[string]any{"status": status}
		if description {
			body["description"] = "status update with an atomic description write"
		}
		return testutil.Call(t, testHandler.UpdateIssue, withURLParam(
			newRequest(http.MethodPatch, "/api/issues/"+issue.ID, body), "id", issue.ID))
	}
	assertStatus := func(issue IssueResponse, want string) {
		t.Helper()
		got, err := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
		if err != nil {
			t.Fatalf("get issue %s: %v", issue.Identifier, err)
		}
		if got.Status != want {
			t.Fatalf("issue %s status = %q, want %q", issue.Identifier, got.Status, want)
		}
	}
	assertBlocked := func(w *testutil.Response) {
		t.Helper()
		if w.Code != http.StatusConflict {
			t.Fatalf("done transition status = %d, want 409: %s", w.Code, w.Body.String())
		}
		var body struct {
			Code                 string `json:"code"`
			OpenPullRequestCount int64  `json:"open_pull_request_count"`
		}
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode conflict: %v", err)
		}
		if body.Code != "open_pull_requests_block_done" || body.OpenPullRequestCount != 1 {
			t.Fatalf("conflict = %+v, want open_pull_requests_block_done with count 1", body)
		}
	}

	const installationID int64 = 30264979
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "manual-done-pr-gate-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE pull_request_id IN (SELECT id FROM github_pull_request WHERE installation_id = $1)`, installationID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE installation_id = $1`, installationID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
	})

	builtIn := create("built-in done gate")
	custom := create("custom done gate")
	batchClear := create("batch all-or-nothing gate")
	customDone := createTestCustomStatus(t, "verified_done_gate", issuestatus.Done)

	// One visible working PR links both delivery issues and blocks built-in and
	// custom done-category transitions. The custom case also exercises the
	// atomic description-update path.
	firePRWebhook(t, secret, installationID, 9101,
		"Work on "+builtIn.Identifier+" and "+custom.Identifier,
		"No close intent yet", "feat/manual-done-gate", "opened")

	assertBlocked(updateStatus(builtIn, "done", false))
	assertStatus(builtIn, "in_progress")
	assertBlocked(updateStatus(custom, customDone.Key, true))
	assertStatus(custom, "in_progress")

	// The preflight pass checks the whole batch before the first write. Put the
	// unblocked issue first so this fails if the implementation regresses to a
	// partial, per-item gate.
	batch := testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPatch, "/api/issues/batch", map[string]any{
		"issue_ids": []string{batchClear.ID, builtIn.ID},
		"updates":   map[string]any{"status": "done"},
	}))
	assertBlocked(batch)
	assertStatus(batchClear, "in_progress")
	assertStatus(builtIn, "in_progress")

	// Closing without merging is still a terminal PR outcome. The source issue
	// may then be closed manually after the user has decided the work is no
	// longer required.
	firePRWebhook(t, secret, installationID, 9101,
		"Work on "+builtIn.Identifier+" and "+custom.Identifier,
		"No close intent yet", "feat/manual-done-gate", "closed")
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(
		newRequest(http.MethodPatch, "/api/issues/"+builtIn.ID, map[string]any{"status": "done"}),
		"id", builtIn.ID)).Want(http.StatusOK)
	assertStatus(builtIn, "done")

	// A bare body mention is reference-only and hidden from the issue PR list;
	// it must not become an invisible blocker for a manual done transition.
	referenceOnly := create("reference-only link stays non-blocking")
	firePRWebhook(t, secret, installationID, 9102,
		"Unrelated cleanup", "Context: see "+referenceOnly.Identifier,
		"feat/unrelated-cleanup", "opened")
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(
		newRequest(http.MethodPatch, "/api/issues/"+referenceOnly.ID, map[string]any{"status": "done"}),
		"id", referenceOnly.ID)).Want(http.StatusOK)
	assertStatus(referenceOnly, "done")
}
