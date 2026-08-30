package toolapprovalsweeper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/service/toolaction"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type SQLStore struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewSQLStore(pool *pgxpool.Pool, queries *db.Queries) *SQLStore {
	return &SQLStore{pool: pool, queries: queries}
}

func (s *SQLStore) ExpireDue(ctx context.Context, asOf time.Time, batchSize int32) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT workspace_id, id
		FROM agent_tool_approval_request
		WHERE status IN ('pending', 'approved') AND expires_at <= $1
		ORDER BY expires_at ASC, id ASC
		LIMIT $2
	`, asOf, batchSize)
	if err != nil {
		return 0, err
	}
	var candidates [][2]pgtype.UUID
	for rows.Next() {
		var workspaceID, approvalID pgtype.UUID
		if err := rows.Scan(&workspaceID, &approvalID); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, [2]pgtype.UUID{workspaceID, approvalID})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	expiredCount := 0
	for _, candidate := range candidates {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return expiredCount, err
		}
		queries := s.queries.WithTx(tx)
		current, err := queries.LockAgentToolApprovalRequest(ctx, db.LockAgentToolApprovalRequestParams{WorkspaceID: candidate[0], ID: candidate[1]})
		if errors.Is(err, pgx.ErrNoRows) {
			_ = tx.Rollback(ctx)
			continue
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return expiredCount, err
		}
		expired, err := queries.ExpireAgentToolApprovalRequest(ctx, db.ExpireAgentToolApprovalRequestParams{
			ExpiredAt: pgtype.Timestamptz{Time: asOf, Valid: true}, WorkspaceID: current.WorkspaceID, ID: current.ID,
			AgentID: current.AgentID, TaskID: current.TaskID, InvocationID: current.InvocationID,
			TransportKind: current.TransportKind, ServerKey: current.ServerKey, ToolName: current.ToolName,
			SchemaDigest: current.SchemaDigest, PolicyRevision: current.PolicyRevision,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			_ = tx.Rollback(ctx)
			continue
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return expiredCount, err
		}
		argumentBytes := expired.ArgumentBytes
		_, err = toolaction.NewSQLService(queries).RecordIn(ctx, queries, toolaction.Event{
			WorkspaceID: util.UUIDToString(expired.WorkspaceID), AgentID: util.UUIDToString(expired.AgentID),
			TaskID: util.UUIDToString(expired.TaskID), IssueID: util.UUIDToString(expired.IssueID),
			InvocationID: util.UUIDToString(expired.InvocationID), ApprovalRequestID: util.UUIDToString(expired.ID),
			TransportKind: expired.TransportKind, ServerKey: expired.ServerKey, ToolName: expired.ToolName,
			SchemaDigest: expired.SchemaDigest, CoverageKind: expired.TransportKind, EventType: "approval_expired",
			ArgumentBytes: &argumentBytes, OutcomeCode: "expired", CreatedAt: asOf,
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			return expiredCount, fmt.Errorf("audit expired approval: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return expiredCount, err
		}
		expiredCount++
	}
	return expiredCount, nil
}

func (s *SQLStore) DeleteRetained(ctx context.Context, cutoff time.Time, batchSize int32) (RetentionResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT workspace_id
		FROM (
			SELECT workspace_id FROM agent_tool_approval_request
			WHERE status IN ('consumed', 'denied', 'expired', 'cancelled')
			  AND COALESCE(consumed_at, decided_at) < $1
			UNION
			SELECT workspace_id FROM agent_tool_action_event WHERE created_at < $1
		) AS due
	`, cutoff)
	if err != nil {
		return RetentionResult{}, err
	}
	var workspaces []pgtype.UUID
	for rows.Next() {
		var workspaceID pgtype.UUID
		if err := rows.Scan(&workspaceID); err != nil {
			rows.Close()
			return RetentionResult{}, err
		}
		workspaces = append(workspaces, workspaceID)
	}
	rows.Close()
	result := RetentionResult{}
	for _, workspaceID := range workspaces {
		if result.ApprovalsDeleted < int(batchSize) {
			ids, err := s.queries.DeleteTerminalAgentToolApprovalRequestsByRetention(ctx, db.DeleteTerminalAgentToolApprovalRequestsByRetentionParams{
				WorkspaceID: workspaceID, RetentionCutoff: pgtype.Timestamptz{Time: cutoff, Valid: true},
				BatchSize: batchSize - int32(result.ApprovalsDeleted),
			})
			if err != nil {
				return result, err
			}
			result.ApprovalsDeleted += len(ids)
		}
		if result.ActionEventsDeleted < int(batchSize) {
			ids, err := s.queries.DeleteAgentToolActionEventsByRetention(ctx, db.DeleteAgentToolActionEventsByRetentionParams{
				WorkspaceID: workspaceID, RetentionCutoff: pgtype.Timestamptz{Time: cutoff, Valid: true},
				BatchSize: batchSize - int32(result.ActionEventsDeleted),
			})
			if err != nil {
				return result, err
			}
			result.ActionEventsDeleted += len(ids)
		}
	}
	return result, nil
}
