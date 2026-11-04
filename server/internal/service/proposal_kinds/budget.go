package proposalkinds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// KeyBudget is the budget kind's key, as stored in proposal.kind.
const KeyBudget = "budget"

// The budget kind: a squad asks the boss to raise a budget (spec
// 4.2). Approval raises the budget's amount and releases the holds once, so the
// runs it held are decided again; rejection changes nothing.

// budgetMaxReason bounds the reason and the expected value, read in the brief.
const budgetMaxReason = 1000

// BudgetRequest is the payload of a budget proposal.
type BudgetRequest struct {
	// BudgetID is the budget the request raises.
	BudgetID string `json:"budget_id"`
	// AmountMicros is the new amount of the budget, in millionths of a dollar.
	AmountMicros int64 `json:"amount_micros"`
	// Reason says why the squad needs more.
	Reason string `json:"reason"`
	// ExpectedValue says what the spend is expected to return.
	ExpectedValue string `json:"expected_value"`
}

type budgetKind struct{}

func (budgetKind) Key() string         { return KeyBudget }
func (budgetKind) FiledByAgents() bool { return true }

func (budgetKind) Validate(ctx context.Context, env Env, payload []byte) ([]byte, error) {
	var b BudgetRequest
	if err := decodeObject(payload, &b); err != nil {
		return nil, err
	}
	b.Reason = strings.TrimSpace(b.Reason)
	b.ExpectedValue = strings.TrimSpace(b.ExpectedValue)
	id, err := util.ParseUUID(b.BudgetID)
	if err != nil {
		return nil, errors.New("budget: budget_id must name a budget of this workspace")
	}
	current, err := env.Q.GetBudgetForWorkspace(ctx, db.GetBudgetForWorkspaceParams{ID: id, WorkspaceID: env.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("budget: budget_id must name a budget of this workspace")
	}
	if err != nil {
		return nil, fmt.Errorf("budget: load budget: %w", err)
	}
	if b.AmountMicros <= current.AmountMicros {
		return nil, fmt.Errorf("budget: amount_micros must be more than the budget's current amount (%d)", current.AmountMicros)
	}
	if b.Reason == "" {
		return nil, errors.New("budget: reason is required: say what the money is for")
	}
	if len(b.Reason) > budgetMaxReason || len(b.ExpectedValue) > budgetMaxReason {
		return nil, fmt.Errorf("budget: reason and expected_value must be at most %d characters", budgetMaxReason)
	}
	return json.Marshal(b)
}

func (budgetKind) Summarize(payload []byte) string {
	var b BudgetRequest
	if json.Unmarshal(payload, &b) != nil {
		return ""
	}
	return fmt.Sprintf("Raise a budget to $%.2f: %s", float64(b.AmountMicros)/1_000_000, b.Reason)
}

// Apply raises the budget and releases its holds once, and records the
// budget_set governance entry, in the approval transaction.
func (budgetKind) Apply(ctx context.Context, q *db.Queries, d Decision) (Effect, error) {
	var b BudgetRequest
	if err := decodeObject(d.Proposal.Payload, &b); err != nil {
		return Effect{}, err
	}
	id, err := util.ParseUUID(b.BudgetID)
	if err != nil {
		return Effect{}, fmt.Errorf("budget: budget_id: %w", err)
	}
	current, err := q.GetBudgetForWorkspace(ctx, db.GetBudgetForWorkspaceParams{ID: id, WorkspaceID: d.WorkspaceID})
	if err != nil {
		return Effect{}, fmt.Errorf("budget: load budget: %w", err)
	}
	if b.AmountMicros <= current.AmountMicros {
		// The budget was raised since filing: the request is already met.
		return Effect{}, nil
	}
	raised, err := q.UpdateBudget(ctx, db.UpdateBudgetParams{
		ID: id, WorkspaceID: d.WorkspaceID, AmountMicros: b.AmountMicros, SoftPercent: current.SoftPercent,
		Mode: current.Mode, Classes: current.Classes, Status: current.Status,
	})
	if err != nil {
		return Effect{}, fmt.Errorf("budget: raise budget: %w", err)
	}
	if _, err := q.ReleaseBudgetHoldsForBudget(ctx, db.ReleaseBudgetHoldsForBudgetParams{
		BudgetID: id, ReleaseReason: pgtype.Text{String: "budget_changed", Valid: true},
	}); err != nil {
		return Effect{}, fmt.Errorf("budget: release holds: %w", err)
	}
	details, err := json.Marshal(map[string]any{
		"from_micros": current.AmountMicros, "to_micros": raised.AmountMicros,
		"proposal_id": util.UUIDToString(d.Proposal.ID), "source": "proposal",
	})
	if err != nil {
		return Effect{}, err
	}
	if _, err := q.InsertGovernanceEvent(ctx, db.InsertGovernanceEventParams{
		ID: dbid.NewV7(), WorkspaceID: d.WorkspaceID, Kind: "budget_set",
		ActorType: "member", ActorID: d.DeciderID, SubjectType: "budget", SubjectID: id,
		Summary: fmt.Sprintf("The boss approved a budget raise to $%.2f per period.", float64(raised.AmountMicros)/1_000_000),
		Details: details,
	}); err != nil {
		return Effect{}, fmt.Errorf("budget: record governance: %w", err)
	}
	return Effect{}, nil
}

func (budgetKind) Reject(ctx context.Context, q *db.Queries, d Decision) error { return nil }
