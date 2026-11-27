package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// hookedTxStarter runs hook before starting a transaction, to schedule work
// inside a handler's decision→write window.
type hookedTxStarter struct {
	base txStarter
	hook func() error
}

func (h hookedTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	if err := h.hook(); err != nil {
		return nil, err
	}
	return h.base.Begin(ctx)
}

// A PR linked between the auto-complete decision and the status write must
// keep the issue open: the write re-checks the linked PRs (MUL-7429 review).
func TestPRAutoComplete_PRLinkedDuringCompletionKeepsIssueOpen(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	secret := "pr-linked-during-completion"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	const inst int64 = 8758002
	issue := prAutoCompleteTestIssue(t, "PR linked during completion", inst)
	firePRWebhook(t, secret, inst, 1, "Closes "+issue.Identifier, "", "fix/a", "opened")
	original := testHandler.TxStarter
	t.Cleanup(func() { testHandler.TxStarter = original })
	// Schedule the second webhook after the first decision but before its write.
	testHandler.TxStarter = hookedTxStarter{base: original, hook: func() error {
		testHandler.TxStarter = original
		firePRWebhook(t, secret, inst, 2, issue.Identifier, "", "fix/b", "opened")
		return nil
	}}
	firePRWebhook(t, secret, inst, 1, "Closes "+issue.Identifier, "", "fix/a", "merged")
	if n := linkedPRCountForTest(t, issue.ID); n != 2 {
		t.Fatalf("want 2 linked PRs, got %d", n)
	}
	if got := issueStatusForTest(t, issue.ID); got != "in_progress" {
		t.Fatalf("completed with linked open PR: got %s, want in_progress", got)
	}
}

// fireTimedPRWebhook delivers a pull_request event that closes identifier,
// with an explicit updated_at.
func fireTimedPRWebhook(t *testing.T, inst int64, identifier, state, timestamp string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"action":       "edited",
		"installation": map[string]any{"id": inst},
		"repository":   map[string]any{"name": "widget", "owner": map[string]any{"login": "acme"}},
		"pull_request": map[string]any{"number": 1, "html_url": "https://github.com/acme/widget/pull/1", "title": "Closes " + identifier, "state": state, "merged": state == "closed", "created_at": "2026-09-22T00:00:00Z", "updated_at": timestamp, "head": map[string]any{"ref": "fix/a"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	testHandler.handlePullRequestEvent(context.Background(), raw)
}

// GitHub can deliver an old event late. It must not roll a merged PR back to
// open, which would make a redelivered merge look new and complete an issue a
// person reopened (MUL-7429 review).
func TestPRAutoComplete_StaleGitHubEventDoesNotRecompleteReopenedIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	const inst int64 = 8758003
	issue := prAutoCompleteTestIssue(t, "stale GitHub event", inst)
	fireTimedPRWebhook(t, inst, issue.Identifier, "open", "2026-09-22T01:00:00Z")
	fireTimedPRWebhook(t, inst, issue.Identifier, "closed", "2026-09-22T02:00:00Z")
	if got := issueStatusForTest(t, issue.ID); got != "done" {
		t.Fatalf("initial merge got %s", got)
	}
	_, err := testHandler.Queries.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{ID: parseUUID(issue.ID), WorkspaceID: parseUUID(testWorkspaceID), Status: "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	fireTimedPRWebhook(t, inst, issue.Identifier, "open", "2026-09-22T01:00:00Z")
	fireTimedPRWebhook(t, inst, issue.Identifier, "closed", "2026-09-22T02:00:00Z")
	if got := issueStatusForTest(t, issue.ID); got != "in_progress" {
		t.Fatalf("old deliveries re-completed reopened issue: got %s", got)
	}
}
