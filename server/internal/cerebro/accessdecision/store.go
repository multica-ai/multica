package accessdecision

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// Store persists the append-only Decision Ledger and reads its grouped drift
// report. It is never consulted by an enforcement path.
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
		LegacyDecision:        string(entry.LegacyDecision),
		LegacyPath:            entry.LegacyPath,
		ShadowDecision:        string(entry.ShadowDecision),
		PolicyDecision:        string(entry.PolicyDecision),
		EvidenceLevel:         string(entry.EvidenceLevel),
		Differs:               entry.Differs,
		Reason:                entry.Reason,
	})
}

// Report returns drift grouped by agent, runtime, and canonical tool. Unknown
// tools fall back to the observed Gateway name in the SQL query.
func (s *Store) Report(ctx context.Context, workspaceID pgtype.UUID) (Report, error) {
	if s == nil || s.q == nil {
		return Report{}, fmt.Errorf("accessdecision: store is not configured")
	}
	rows, err := s.q.ReportCerebroAccessDecisionLedger(ctx, workspaceID)
	if err != nil {
		return Report{}, fmt.Errorf("accessdecision: report: %w", err)
	}
	report := Report{Groups: make([]Group, 0, len(rows))}
	for _, row := range rows {
		group := Group{
			AgentID:        util.UUIDToString(row.AgentID),
			RuntimeID:      util.UUIDToString(row.RuntimeID),
			Tool:           row.Tool,
			PolicyDecision: PolicyDecision(row.PolicyDecision),
			Total:          int(row.Total),
			Diffs:          int(row.Diffs),
		}
		report.Total += group.Total
		report.Diffs += group.Diffs
		report.Groups = append(report.Groups, group)
	}
	return report, nil
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
