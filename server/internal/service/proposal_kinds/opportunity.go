package proposalkinds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DefaultOpportunityStage is where an approved opportunity starts when the
// filing does not name a stage.
const DefaultOpportunityStage = "validate"

// Opportunity limits, bounded for the boss who reads them.
const (
	opportunityMaxThesis   = 4000
	opportunityMaxMarket   = 200
	opportunityMaxEvidence = 20
)

// Opportunity is the payload of an opportunity proposal (spec 3.2). Approval
// creates the initiative at TargetStage.
type Opportunity struct {
	Title          string   `json:"title,omitempty"`
	Thesis         string   `json:"thesis"`
	Market         string   `json:"market,omitempty"`
	EvidenceDocIDs []string `json:"evidence_doc_ids,omitempty"`
	TargetStage    string   `json:"target_stage,omitempty"`
}

type opportunityKind struct {
	initiatives Initiatives
}

func (opportunityKind) Key() string         { return KeyOpportunity }
func (opportunityKind) FiledByAgents() bool { return true }

func (opportunityKind) Validate(ctx context.Context, env Env, payload []byte) ([]byte, error) {
	var o Opportunity
	if err := decodeObject(payload, &o); err != nil {
		return nil, err
	}
	o.Thesis = strings.TrimSpace(o.Thesis)
	o.Market = strings.TrimSpace(o.Market)
	o.Title = strings.TrimSpace(o.Title)
	if o.Thesis == "" {
		return nil, errors.New("opportunity: thesis is required: say why this is worth doing")
	}
	if len(o.Thesis) > opportunityMaxThesis {
		return nil, fmt.Errorf("opportunity: thesis must be at most %d characters", opportunityMaxThesis)
	}
	if len(o.Market) > opportunityMaxMarket {
		return nil, fmt.Errorf("opportunity: market must be at most %d characters", opportunityMaxMarket)
	}
	if len(o.EvidenceDocIDs) > opportunityMaxEvidence {
		return nil, fmt.Errorf("opportunity: at most %d evidence_doc_ids", opportunityMaxEvidence)
	}
	if len(o.EvidenceDocIDs) == 0 {
		return nil, errors.New("opportunity: evidence_doc_ids is required: cite the documents the thesis rests on")
	}
	if err := requireOpportunityFloor(ctx, env); err != nil {
		return nil, err
	}
	if o.TargetStage == "" {
		o.TargetStage = DefaultOpportunityStage
	}
	keys, err := defaultPipelineStageKeys(ctx, env)
	if err != nil {
		return nil, err
	}
	if !contains(keys, o.TargetStage) {
		return nil, fmt.Errorf("opportunity: target_stage %q is not a stage of the default pipeline", o.TargetStage)
	}
	return json.Marshal(o)
}

func (opportunityKind) Summarize(payload []byte) string {
	var o Opportunity
	if json.Unmarshal(payload, &o) != nil {
		return ""
	}
	if o.Market != "" {
		return "Opportunity in " + o.Market + ", starting at " + o.TargetStage
	}
	return "Opportunity, starting at " + o.TargetStage
}

func (k opportunityKind) Apply(ctx context.Context, q *db.Queries, d Decision) (Effect, error) {
	var o Opportunity
	if err := json.Unmarshal(d.Proposal.Payload, &o); err != nil {
		return Effect{}, fmt.Errorf("opportunity payload: %w", err)
	}
	if o.Title == "" {
		o.Title = d.Issue.Title
	}
	return Effect{}, k.initiatives.OpenFromOpportunity(ctx, q, d, o)
}

func (opportunityKind) Reject(context.Context, *db.Queries, Decision) error { return nil }

// opportunityFloors are the floors whose squads may file an opportunity (spec 3.2).
var opportunityFloors = []string{"Research", "Product"}

// requireOpportunityFloor refuses a filing by a squad that does not sit on the
// Research or Product floor. A floor is matched by name, as the starter company
// names them.
func requireOpportunityFloor(ctx context.Context, env Env) error {
	name, err := env.Q.GetSquadFloorName(ctx, db.GetSquadFloorNameParams{SquadID: env.SquadID, WorkspaceID: env.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("opportunity: the filing squad is not in this workspace")
	}
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	for _, f := range opportunityFloors {
		if strings.EqualFold(name, f) {
			return nil
		}
	}
	if name == "" {
		return errors.New("opportunity: only squads on the Research or Product floor may file one; this squad sits on no floor")
	}
	return fmt.Errorf("opportunity: only squads on the Research or Product floor may file one; this squad sits on %s", name)
}

// defaultPipelineStageKeys reads the stage keys of the workspace's default
// pipeline. A workspace without one refuses an opportunity at filing: there is
// nowhere for the initiative to start.
func defaultPipelineStageKeys(ctx context.Context, env Env) ([]string, error) {
	row, err := env.Q.GetDefaultPipeline(ctx, env.WorkspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("opportunity: this workspace has no default pipeline; install the starter company's pipeline first")
		}
		return nil, err
	}
	var stages []struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(row.Stages, &stages); err != nil {
		return nil, fmt.Errorf("default pipeline stages: %w", err)
	}
	keys := make([]string, 0, len(stages))
	for _, s := range stages {
		keys = append(keys, s.Key)
	}
	return keys, nil
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
