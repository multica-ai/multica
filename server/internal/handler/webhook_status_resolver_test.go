package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/vcs"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type webhookStatusCatalog struct {
	issuestatus.Querier
	reads      map[pgtype.UUID]int
	pointReads int
	fail       bool
}

func (c *webhookStatusCatalog) ListIssueStatusEntries(ctx context.Context, arg db.ListIssueStatusEntriesParams) ([]db.IssueStatus, error) {
	c.reads[arg.WorkspaceID]++
	if c.fail {
		return nil, errors.New("test catalog unavailable")
	}
	return c.Querier.ListIssueStatusEntries(ctx, arg)
}

func (c *webhookStatusCatalog) GetIssueStatusEntryByKey(ctx context.Context, arg db.GetIssueStatusEntryByKeyParams) (db.IssueStatus, error) {
	c.pointReads++
	if c.fail {
		return db.IssueStatus{}, errors.New("test catalog unavailable")
	}
	return c.Querier.GetIssueStatusEntryByKey(ctx, arg)
}

// Exercise both real mirror paths, including their persisted PR close gate.
// Signature parsing is covered by the existing provider webhook suites.
func TestWebhookStatusResolver(t *testing.T) {
	ctx := context.Background()
	for _, provider := range []string{"github", "forgejo"} {
		t.Run(provider, func(t *testing.T) {
			for _, tc := range []struct {
				name           string
				statuses, want []string
				fail           bool
				wantReads      int
			}{
				{"custom", []string{"approved", "dropped", "review", "approved", "done"}, []string{"approved", "dropped", "done", "approved", "done"}, false, 1},
				{"builtins", []string{"done", "cancelled", "in_review"}, []string{"done", "cancelled", "done"}, false, 0},
				// An unresolved key keeps the old nonterminal-gate behavior. This
				// optimization must not introduce a new close-policy decision.
				{"unknown", []string{"missing", "missing"}, []string{"done", "done"}, false, 1},
				{"unavailable", []string{"review", "review"}, []string{"done", "done"}, true, 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					catalog := &webhookStatusCatalog{Querier: testHandler.Queries, reads: map[pgtype.UUID]int{}, fail: tc.fail}
					h := *testHandler
					h.IssueStatusCatalog = catalog
					// The same key is terminal in one workspace and nonterminal in
					// the other; a process-wide resolver would close the wrong set.
					for workspace := 0; workspace < 2; workspace++ {
						ws := dbfx.Workspace(t, "Webhook status resolver", fmt.Sprintf("resolver-%s-%s-%d", provider, tc.name, workspace), testutil.Cols{"issue_prefix": "RSL"})
						wsID := parseUUID(ws)
						fixture := testutil.New(testPool, ws, testUserID)
						for key, category := range map[string]string{"approved": "done", "dropped": "cancelled", "review": "in_review"} {
							if workspace == 1 && key == "approved" {
								category = "in_review"
							}
							cols := testutil.Cols{"workspace_id": ws, "key": key, "name": key, "category": category, "color": "#123456"}
							if key == "approved" {
								cols["archived_at"] = testutil.Raw("now()")
							}
							fixture.Insert(t, "issue_status", cols)
						}
						var ids, closing []string
						for i, status := range tc.statuses {
							ids = append(ids, fixture.Issue(t, "Resolver fixture", testutil.Cols{"status": status, "number": i + 1}))
							closing = append(closing, fmt.Sprintf("Closes RSL-%d", i+1))
						}
						// Mirroring creates rows outside the fixture builders.
						for _, table := range []string{"issue_pull_request", "issue_vcs_pull_request"} {
							fixture.Cleanup(t, "DELETE FROM "+table+" WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id = $1)", ws)
						}
						fixture.Cleanup(t, "DELETE FROM github_pull_request WHERE workspace_id = $1", ws)
						fixture.Cleanup(t, "DELETE FROM vcs_pull_request WHERE workspace_id = $1", ws)
						const timestamp = "2026-09-08T00:00:00Z"
						var mirror func()
						if provider == "github" {
							p := &ghPullRequestPayload{}
							p.Action = "closed"
							p.Repository.Owner.Login, p.Repository.Name = "fixture", "resolver"
							p.PullRequest.Number, p.PullRequest.Title = 1, "Resolve linked issues"
							p.PullRequest.Body = strings.Join(closing, "\n")
							p.PullRequest.State, p.PullRequest.Merged = "closed", true
							p.PullRequest.HTMLURL = "https://github.test/fixture/resolver/pull/1"
							p.PullRequest.CreatedAt, p.PullRequest.UpdatedAt = timestamp, timestamp
							mirror = func() {
								h.mirrorPullRequestForWorkspace(ctx, wsID, int64(91000+workspace), p, closeIntentPolicy{unrestricted: true})
							}
						} else {
							connID := fixture.Insert(t, "vcs_connection", testutil.Cols{"workspace_id": ws, "provider": provider, "instance_url": "https://forgejo.test", "account_login": "fixture", "access_token_encrypted": "unused", "webhook_secret_encrypted": "unused"})
							conn := db.VcsConnection{ID: parseUUID(connID), WorkspaceID: wsID, Provider: provider}
							ev := vcs.PullRequestEvent{Action: "closed", State: "merged", RepoOwner: "fixture", RepoName: "resolver", Number: 1, Title: "Resolve linked issues", Body: strings.Join(closing, "\n"), HTMLURL: "https://forgejo.test/fixture/resolver/pulls/1", CreatedAt: timestamp, UpdatedAt: timestamp}
							mirror = func() { h.mirrorVCSPullRequest(ctx, conn, ev) }
						}
						mirror()
						if got := catalog.reads[wsID]; got != tc.wantReads {
							t.Errorf("workspace %d catalog reads = %d, want %d", workspace, got, tc.wantReads)
						}
						for i, id := range ids {
							want := tc.want[i]
							if workspace == 1 && tc.statuses[i] == "approved" {
								want = "done"
							}
							var status string
							fixture.QueryRow(t, "SELECT status FROM issue WHERE id = $1", id).Scan(&status)
							if status != want {
								t.Errorf("workspace %d issue %d status = %q, want %q", workspace, i, status, want)
							}
						}
						// A fresh delivery must not reuse even a successful prior
						// resolver. Reset one row onto a custom terminal key and replay.
						if tc.name == "custom" {
							fixture.Exec(t, "UPDATE issue SET status = 'dropped' WHERE id = $1", ids[0])
							mirror()
							if catalog.reads[wsID] != 2 {
								t.Errorf("replayed delivery reads = %d, want 2 total", catalog.reads[wsID])
							}
							var status string
							fixture.QueryRow(t, "SELECT status FROM issue WHERE id = $1", ids[0]).Scan(&status)
							if status != "dropped" {
								t.Errorf("replay changed terminal status to %q", status)
							}
						}
					}
					if catalog.pointReads != 0 {
						t.Errorf("per-key reads = %d, want 0", catalog.pointReads)
					}
				})
			}
		})
	}
}
