package accessdecision

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// Store persists the append-only decision ledger. It is never consulted by an
// enforcement path.
type Store struct {
	q *cerebrodb.Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{q: cerebrodb.New(pool)}
}

func (s *Store) Append(ctx context.Context, entry Entry) error {
	if s == nil || s.q == nil {
		return fmt.Errorf("accessdecision: store is not configured")
	}
	workspaceID, err := requiredUUID("workspace_id", entry.WorkspaceID)
	if err != nil {
		return err
	}
	agentID, err := requiredUUID("agent_id", entry.AgentID)
	if err != nil {
		return err
	}
	runtimeID, err := requiredUUID("runtime_id", entry.RuntimeID)
	if err != nil {
		return err
	}
	return s.q.AppendCerebroAccessDecisionLedger(ctx, cerebrodb.AppendCerebroAccessDecisionLedgerParams{
		WorkspaceID:           workspaceID,
		AgentID:               agentID,
		RuntimeID:             runtimeID,
		OnBehalfOfUserID:      optionalUUID(entry.OnBehalfOfUserID),
		TaskID:                optionalUUID(entry.TaskID),
		IssueID:               optionalUUID(entry.IssueID),
		ObservedToolName:      entry.ObservedToolName,
		CanonicalCapabilityID: optionalText(entry.CanonicalCapabilityID),
		// The existing database shape predates the Gateway cutover and still has
		// comparison columns. Persist the one canonical outcome into both sides
		// so storage cannot revive a caller-controlled legacy access path.
		LegacyDecision: string(entry.Decision),
		LegacyPath:     "policy_decision_service",
		ShadowDecision: string(entry.Decision),
		PolicyDecision: string(entry.PolicyDecision),
		EvidenceLevel:  string(entry.EvidenceLevel),
		Differs:        false,
		Reason:         entry.Reason,
	})
}

func requiredUUID(field, value string) (pgtype.UUID, error) {
	id, err := util.ParseUUID(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("accessdecision: invalid %s: %w", field, err)
	}
	return id, nil
}

func optionalUUID(value string) pgtype.UUID {
	if value == "" {
		return pgtype.UUID{}
	}
	id, err := util.ParseUUID(value)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}

func optionalText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}
