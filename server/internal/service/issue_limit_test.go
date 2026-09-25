package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type issueLimitProviderFunc func(context.Context, uuid.UUID, entitlement.GateName) entitlement.Decision

func (f issueLimitProviderFunc) Gate(ctx context.Context, workspaceID uuid.UUID, gate entitlement.GateName) entitlement.Decision {
	return f(ctx, workspaceID, gate)
}

func TestResolveIssueCountPolicyUsesOnlyCloudInstruction(t *testing.T) {
	workspace := uuid.New()
	workspaceID := pgtype.UUID{Bytes: workspace, Valid: true}
	limit := 17

	valid := ResolveIssueCountPolicy(context.Background(), issueLimitProviderFunc(
		func(_ context.Context, gotWorkspace uuid.UUID, gate entitlement.GateName) entitlement.Decision {
			if gotWorkspace != workspace || gate != entitlement.GateIssueCount {
				t.Fatalf("gate request = %s %s", gotWorkspace, gate)
			}
			return entitlement.Decision{
				Gate:           entitlement.Gate{Action: entitlement.ActionEnforce, Limit: &limit},
				PolicyRevision: 9,
				Reason:         entitlement.ReasonRefreshed,
			}
		}), workspaceID)
	if valid.Action != entitlement.ActionEnforce || valid.Limit != int64(limit) || valid.PolicyRevision != 9 {
		t.Fatalf("valid policy = %+v", valid)
	}

	malformed := ResolveIssueCountPolicy(context.Background(), issueLimitProviderFunc(
		func(context.Context, uuid.UUID, entitlement.GateName) entitlement.Decision {
			return entitlement.Decision{
				Gate:   entitlement.Gate{Action: entitlement.ActionEnforce},
				Reason: entitlement.ReasonCacheFresh,
			}
		}), workspaceID)
	if malformed.Action != entitlement.ActionOff {
		t.Fatalf("malformed policy = %+v", malformed)
	}

	observe := ResolveIssueCountPolicy(context.Background(), issueLimitProviderFunc(
		func(context.Context, uuid.UUID, entitlement.GateName) entitlement.Decision {
			return entitlement.Decision{
				Gate: entitlement.Gate{Action: entitlement.ActionObserve, Limit: &limit},
			}
		}), workspaceID)
	if observe.Action != entitlement.ActionOff || observe.Limit != 0 {
		t.Fatalf("observe policy = %+v, want fail-open off", observe)
	}

	disabled := ResolveIssueCountPolicy(context.Background(), nil, workspaceID)
	if disabled.Action != entitlement.ActionOff {
		t.Fatalf("disabled policy = %+v", disabled)
	}
}

func TestIssueCountLimitSerializesConcurrentCreatesAndDeleteFreesCapacity(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	queries := db.New(pool)
	workspaceIDString, userIDString, _, _ := seedAttributionFixture(t, pool)
	workspaceID := util.MustParseUUID(workspaceIDString)
	userID := util.MustParseUUID(userIDString)

	var initialCount int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM issue WHERE workspace_id = $1`, workspaceID).Scan(&initialCount); err != nil {
		t.Fatalf("count initial issues: %v", err)
	}
	limit := int(initialCount + 1)
	stub := entitlementtest.New()
	stub.Set(uuid.UUID(workspaceID.Bytes), entitlement.GateIssueCount, entitlement.Decision{
		Gate:           entitlement.Gate{Action: entitlement.ActionEnforce, Limit: &limit},
		PolicyRevision: 13,
	})
	svc := NewIssueService(queries, pool, nil, nil, nil)
	svc.Entitlements = stub

	create := func(title string) (db.Issue, error) {
		result, err := svc.Create(ctx, IssueCreateParams{
			WorkspaceID:    workspaceID,
			Title:          title,
			Status:         "todo",
			Priority:       "none",
			CreatorType:    "member",
			CreatorID:      userID,
			AllowDuplicate: true,
		}, IssueCreateOpts{})
		return result.Issue, err
	}

	start := make(chan struct{})
	type createResult struct {
		issue db.Issue
		err   error
	}
	results := make(chan createResult, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			issue, err := create(fmt.Sprintf("issue-limit-race-%d", index))
			results <- createResult{issue: issue, err: err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	var created db.Issue
	createdCount, blockedCount := 0, 0
	for result := range results {
		switch {
		case result.err == nil:
			created = result.issue
			createdCount++
		default:
			var limitErr *IssueLimitReachedError
			if !errors.As(result.err, &limitErr) {
				t.Fatalf("concurrent create: %v", result.err)
			}
			if limitErr.Limit != int64(limit) || limitErr.PolicyRevision != 13 {
				t.Fatalf("limit error = %v", result.err)
			}
			blockedCount++
		}
	}
	if createdCount != 1 || blockedCount != 1 {
		t.Fatalf("created = %d, blocked = %d; want 1 and 1", createdCount, blockedCount)
	}

	var countAtLimit int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM issue WHERE workspace_id = $1`, workspaceID).Scan(&countAtLimit); err != nil {
		t.Fatalf("count issues at limit: %v", err)
	}
	if countAtLimit != int64(limit) {
		t.Fatalf("issue count = %d, want %d", countAtLimit, limit)
	}
	if err := queries.DeleteIssue(ctx, db.DeleteIssueParams{ID: created.ID, WorkspaceID: workspaceID}); err != nil {
		t.Fatalf("delete admitted issue: %v", err)
	}
	if _, err := create("issue-limit-after-delete"); err != nil {
		t.Fatalf("create after delete: %v", err)
	}
}
