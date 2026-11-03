package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Tenant lifecycle (spec 3.3, plan 10.14). Deleting a workspace does not remove
// its WeKnora tenant at once. The delete transaction queues the tenant, and a
// job removes it only after the hold has passed, so a mistaken deletion can still
// be undone by an operator during the hold.

// KnowledgeTenantHold is how long a deleted workspace's tenant is kept.
const KnowledgeTenantHold = 7 * 24 * time.Hour

const (
	knowledgeDeprovisionBatch = 20
	knowledgeRetryBase        = 5 * time.Minute
	knowledgeRetryCap         = 6 * time.Hour
)

// ErrKnowledgeDeprovisionNotHeld means there is no held purge to cancel: it
// already ran, or it never existed.
var ErrKnowledgeDeprovisionNotHeld = errors.New("no knowledge tenant is held for this workspace")

// QueueKnowledgeTeardown is called inside the workspace delete transaction. It
// queues the workspace's WeKnora tenant for removal after the hold, and deletes
// the workspace's knowledge-access rows and owner logins, which have no foreign
// keys. The knowledge bases and links are removed by the same delete. The queue
// row is written only if the delete commits.
func QueueKnowledgeTeardown(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, now time.Time) error {
	if _, err := q.QueueKnowledgeTenantDeprovision(ctx, db.QueueKnowledgeTenantDeprovisionParams{
		WorkspaceID: workspaceID, HoldUntil: pgtype.Timestamptz{Time: now.Add(KnowledgeTenantHold), Valid: true},
	}); err != nil {
		return fmt.Errorf("queue knowledge tenant removal: %w", err)
	}
	if err := q.DeleteWorkspaceKnowledgeACL(ctx, workspaceID); err != nil {
		return fmt.Errorf("delete knowledge access: %w", err)
	}
	if err := q.DeleteWorkspaceKnowledgeOwnerAccounts(ctx, workspaceID); err != nil {
		return fmt.Errorf("delete owner knowledge logins: %w", err)
	}
	if err := q.DeleteWorkspaceKnowledgeRepoFiles(ctx, workspaceID); err != nil {
		return fmt.Errorf("delete knowledge repo files: %w", err)
	}
	if err := q.DeleteWorkspaceKnowledgeRepoSync(ctx, workspaceID); err != nil {
		return fmt.Errorf("delete knowledge repo sync: %w", err)
	}
	return nil
}

// CancelKnowledgeDeprovision cancels a purge that has not run yet, so the tenant
// is kept. It is the operator's recovery during the hold.
func (s *KnowledgeService) CancelKnowledgeDeprovision(ctx context.Context, workspaceID pgtype.UUID) error {
	n, err := s.Queries.CancelKnowledgeDeprovision(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("cancel knowledge tenant removal: %w", err)
	}
	if n == 0 {
		return ErrKnowledgeDeprovisionNotHeld
	}
	return nil
}

// DeprovisionDue removes the WeKnora tenants whose hold has passed. A tenant
// that cannot be removed now is retried with backoff. It returns how many
// tenants were removed. Without a configured sidecar there is nothing to remove.
func (s *KnowledgeService) DeprovisionDue(ctx context.Context) (int, error) {
	if s == nil || s.Client == nil {
		return 0, nil
	}
	rows, err := s.Queries.ClaimDueKnowledgeDeprovisions(ctx, knowledgeDeprovisionBatch)
	if err != nil {
		return 0, fmt.Errorf("claim knowledge tenant removals: %w", err)
	}
	removed := 0
	for _, row := range rows {
		err := s.Client.DeleteTenant(ctx, row.WeknoraTenantID)
		if err == nil || errors.Is(err, knowledge.ErrNotFound) {
			// Already gone counts as removed: the job is for the end state.
			if _, err := s.Queries.MarkKnowledgeDeprovisionDone(ctx, row.WorkspaceID); err != nil {
				return removed, fmt.Errorf("record knowledge tenant removal: %w", err)
			}
			removed++
			continue
		}
		slog.Warn("knowledge: tenant removal failed; will retry",
			"workspace_id", util.UUIDToString(row.WorkspaceID), "attempts", row.Attempts, "error", err)
		if _, err := s.Queries.RetryKnowledgeDeprovision(ctx, db.RetryKnowledgeDeprovisionParams{
			WorkspaceID:   row.WorkspaceID,
			NextAttemptAt: pgtype.Timestamptz{Time: time.Now().Add(knowledgeRetryDelay(row.Attempts)), Valid: true},
			LastError:     knowledgeFailureText(err),
		}); err != nil {
			return removed, fmt.Errorf("schedule knowledge tenant retry: %w", err)
		}
	}
	return removed, nil
}

// knowledgeRetryDelay doubles from knowledgeRetryBase per attempt, up to the cap.
func knowledgeRetryDelay(attempts int32) time.Duration {
	d := knowledgeRetryBase
	for i := int32(1); i < attempts && d < knowledgeRetryCap; i++ {
		d *= 2
	}
	if d > knowledgeRetryCap {
		return knowledgeRetryCap
	}
	return d
}
