package proposalkinds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The boss's outcomes at an operate-stage review gate (spec 3.6).
const (
	OutcomeContinue = "continue"
	OutcomeScale    = "scale"
	OutcomeSunset   = "sunset"
)

// ValidOutcome reports whether an outcome names one of the three choices.
func ValidOutcome(s string) bool {
	switch s {
	case OutcomeContinue, OutcomeScale, OutcomeSunset:
		return true
	}
	return false
}

// Gate is the payload of a stage_gate proposal. The system files it when a gate
// stage is ready; only the system may file one (FiledByAgents is false).
// GateDay is set on the operate-stage reviews (30 and 90 days after launch).
type Gate struct {
	InitiativeID string   `json:"initiative_id"`
	From         string   `json:"from"`
	To           string   `json:"to"`
	ArtifactIDs  []string `json:"artifact_ids,omitempty"`
	GateDay      int      `json:"gate_day,omitempty"`
}

type stageGateKind struct {
	initiatives Initiatives
}

func (stageGateKind) Key() string         { return KeyStageGate }
func (stageGateKind) FiledByAgents() bool { return false }

func (stageGateKind) Validate(ctx context.Context, env Env, payload []byte) ([]byte, error) {
	var g Gate
	if err := decodeObject(payload, &g); err != nil {
		return nil, err
	}
	id, err := parseUUID(g.InitiativeID)
	if err != nil {
		return nil, errors.New("stage_gate: initiative_id must be an initiative id")
	}
	if g.From == "" || g.To == "" {
		return nil, errors.New("stage_gate: from and to are required")
	}
	if _, err := env.Q.GetInitiativeInWorkspace(ctx, db.GetInitiativeInWorkspaceParams{ID: id, WorkspaceID: env.WorkspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("stage_gate: initiative not found in this workspace")
		}
		return nil, err
	}
	return json.Marshal(g)
}

func (stageGateKind) Summarize(payload []byte) string {
	var g Gate
	if json.Unmarshal(payload, &g) != nil {
		return ""
	}
	if g.GateDay > 0 {
		return fmt.Sprintf("Review at %d days after launch", g.GateDay)
	}
	return "Gate from " + g.From + " to " + g.To
}

func (k stageGateKind) Apply(ctx context.Context, q *db.Queries, d Decision) (Effect, error) {
	g, err := gateOf(d)
	if err != nil {
		return Effect{}, err
	}
	return Effect{}, k.initiatives.AdvanceFromGate(ctx, q, d, g)
}

func (k stageGateKind) Reject(ctx context.Context, q *db.Queries, d Decision) error {
	g, err := gateOf(d)
	if err != nil {
		return err
	}
	return k.initiatives.ReturnFromGate(ctx, q, d, g)
}

func gateOf(d Decision) (Gate, error) {
	var g Gate
	if err := json.Unmarshal(d.Proposal.Payload, &g); err != nil {
		return Gate{}, fmt.Errorf("stage_gate payload: %w", err)
	}
	return g, nil
}

// parseUUID parses a canonical UUID string. Empty or malformed input is an error.
func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil || !u.Valid {
		return pgtype.UUID{}, errors.New("invalid uuid")
	}
	return u, nil
}
