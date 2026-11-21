package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestIssueCreatePRAutoCompleteAtomic(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		name := "commit"
		if rollback {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			pool := sharedTestPool(t)
			f := testutil.New(pool, "", "")
			userID := f.User(t, "PR automation creator", uuid.NewString()+"@multica.test")
			workspaceID := f.Workspace(t, "PR automation create", "pr-create-"+uuid.NewString())
			f.Cleanup(t, "DELETE FROM issue WHERE workspace_id = $1", workspaceID)
			f.Cleanup(t, "DELETE FROM issue_pr_automation WHERE workspace_id = $1", workspaceID)
			q := db.New(pool)
			bus := events.New()
			published := 0
			bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
				published++
				id := e.Payload.(map[string]any)["issue_id"].(string)
				disabled, err := q.GetIssuePRAutoCompleteDisabled(context.Background(), util.MustParseUUID(id))
				if err != nil || !disabled {
					t.Errorf("issue became visible before its override: disabled=%v err=%v", disabled, err)
				}
			})
			var txStarter TxStarter = pool
			injected := errors.New("reject create commit")
			if rollback {
				txStarter = &rejectIssueCommitTxStarter{pool: pool, err: injected}
			}
			svc := NewIssueService(q, txStarter, bus, analytics.NoopClient{}, nil)
			_, err := svc.Create(context.Background(), IssueCreateParams{
				WorkspaceID: util.MustParseUUID(workspaceID), Title: "Sequential PR delivery", Status: "todo", Priority: "none",
				CreatorType: "member", CreatorID: util.MustParseUUID(userID), PRAutoCompleteDisabled: true,
			}, IssueCreateOpts{})
			if rollback && !errors.Is(err, injected) || !rollback && err != nil {
				t.Fatalf("Create: %v", err)
			}
			want := 1
			if rollback {
				want = 0
			}
			issues := f.Count(t, "SELECT count(*) FROM issue WHERE workspace_id = $1", workspaceID)
			overrides := f.Count(t, "SELECT count(*) FROM issue_pr_automation WHERE workspace_id = $1", workspaceID)
			if issues != want || overrides != want || published != want {
				t.Fatalf("issues=%d overrides=%d events=%d, want %d each", issues, overrides, published, want)
			}
		})
	}
}
