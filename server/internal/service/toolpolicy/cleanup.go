package toolpolicy

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/toolaction"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type AgentCleanupQueries interface {
	toolaction.EventQueries
	CancelAgentToolApprovalRequestsForAgentCleanup(context.Context, db.CancelAgentToolApprovalRequestsForAgentCleanupParams) ([]db.AgentToolApprovalRequest, error)
	DeleteAgentToolPolicyRulesForAgent(context.Context, db.DeleteAgentToolPolicyRulesForAgentParams) (int64, error)
	DeleteAgentToolPolicyForAgent(context.Context, db.DeleteAgentToolPolicyForAgentParams) (int64, error)
}

type WorkspaceCleanupQueries interface {
	DeleteAgentToolActionEventsForWorkspace(context.Context, pgtype.UUID) (int64, error)
	DeleteAgentToolApprovalRequestsForWorkspace(context.Context, pgtype.UUID) (int64, error)
	DeleteAgentToolPolicyRulesForWorkspaceCleanup(context.Context, pgtype.UUID) (int64, error)
	DeleteAgentToolPolicyRevisionsForWorkspaceCleanup(context.Context, pgtype.UUID) (int64, error)
	DeleteAgentToolPoliciesForWorkspaceCleanup(context.Context, pgtype.UUID) (int64, error)
}

func CleanupAgent(ctx context.Context, queries AgentCleanupQueries, recorder toolaction.InTransactionRecorder, workspaceID, agentID pgtype.UUID, at time.Time) error {
	cancelled, err := queries.CancelAgentToolApprovalRequestsForAgentCleanup(ctx, db.CancelAgentToolApprovalRequestsForAgentCleanupParams{
		CancelledAt: pgtype.Timestamptz{Time: at.UTC(), Valid: true},
		WorkspaceID: workspaceID,
		AgentID:     agentID,
	})
	if err != nil {
		return fmt.Errorf("cancel agent approvals: %w", err)
	}
	for _, approval := range cancelled {
		if _, err := recorder.RecordIn(ctx, queries, cancellationEvent(approval, at.UTC())); err != nil {
			return fmt.Errorf("audit agent approval cancellation: %w", err)
		}
	}
	if _, err := queries.DeleteAgentToolPolicyRulesForAgent(ctx, db.DeleteAgentToolPolicyRulesForAgentParams{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
	}); err != nil {
		return fmt.Errorf("delete agent tool policy rules: %w", err)
	}
	if _, err := queries.DeleteAgentToolPolicyForAgent(ctx, db.DeleteAgentToolPolicyForAgentParams{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
	}); err != nil {
		return fmt.Errorf("delete agent tool policy: %w", err)
	}
	return nil
}

func CleanupWorkspace(ctx context.Context, queries WorkspaceCleanupQueries, workspaceID pgtype.UUID) error {
	steps := []struct {
		name string
		run  func() error
	}{
		{name: "action events", run: func() error { _, err := queries.DeleteAgentToolActionEventsForWorkspace(ctx, workspaceID); return err }},
		{name: "approvals", run: func() error {
			_, err := queries.DeleteAgentToolApprovalRequestsForWorkspace(ctx, workspaceID)
			return err
		}},
		{name: "policy rules", run: func() error {
			_, err := queries.DeleteAgentToolPolicyRulesForWorkspaceCleanup(ctx, workspaceID)
			return err
		}},
		{name: "policy revisions", run: func() error {
			_, err := queries.DeleteAgentToolPolicyRevisionsForWorkspaceCleanup(ctx, workspaceID)
			return err
		}},
		{name: "policies", run: func() error {
			_, err := queries.DeleteAgentToolPoliciesForWorkspaceCleanup(ctx, workspaceID)
			return err
		}},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
			return fmt.Errorf("delete workspace tool %s: %w", step.name, err)
		}
	}
	return nil
}
