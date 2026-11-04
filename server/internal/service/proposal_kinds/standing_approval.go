package proposalkinds

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// standingApprovalKind is the proposal an agent files to ask for a standing
// approval (spec 4.4). Its payload is the policy. The boss approves it like any
// proposal, and approval creates the policy with the decider as its creator. No
// agent creates a policy directly, and a standing_approval proposal can never be
// auto-approved (ForbiddenForStandingApproval).
type standingApprovalKind struct {
	registry *Registry
}

func (standingApprovalKind) Key() string         { return KeyStandingApproval }
func (standingApprovalKind) FiledByAgents() bool { return true }

func (k standingApprovalKind) known(key string) bool {
	return k.registry != nil && k.registry.Has(key)
}

func (k standingApprovalKind) Validate(_ context.Context, _ Env, payload []byte) ([]byte, error) {
	spec, err := DecodePolicySpec(payload)
	if err != nil {
		return nil, err
	}
	spec, err = ValidatePolicySpec(spec, k.known, time.Now())
	if err != nil {
		return nil, err
	}
	return EncodePolicySpec(spec)
}

func (standingApprovalKind) Summarize(payload []byte) string {
	spec, err := DecodePolicySpec(payload)
	if err != nil {
		return ""
	}
	return "Standing approval for " + spec.Kind + ": " + spec.Name
}

// Apply creates the policy the approval asks for. The payload was validated at
// filing; it is checked again, because the registered kinds may have changed
// between the filing and the decision.
func (k standingApprovalKind) Apply(ctx context.Context, q *db.Queries, d Decision) (Effect, error) {
	spec, err := DecodePolicySpec(d.Proposal.Payload)
	if err != nil {
		return Effect{}, err
	}
	spec, err = ValidatePolicySpec(spec, k.known, time.Now())
	if err != nil {
		return Effect{}, err
	}
	match, err := json.Marshal(spec.Match)
	if err != nil {
		return Effect{}, err
	}
	var validUntil pgtype.Date
	if spec.ValidUntil != "" {
		day, err := time.Parse("2006-01-02", spec.ValidUntil)
		if err != nil {
			return Effect{}, err
		}
		validUntil = pgtype.Date{Time: day, Valid: true}
	}
	policy, err := q.CreateApprovalPolicy(ctx, db.CreateApprovalPolicyParams{
		ID:          dbid.NewV7(),
		WorkspaceID: d.WorkspaceID,
		Name:        spec.Name,
		Kind:        spec.Kind,
		Match:       match,
		MaxPerWeek:  *spec.MaxPerWeek,
		ValidUntil:  validUntil,
		CreatedBy:   d.DeciderID,
	})
	if err != nil {
		return Effect{}, err
	}
	return Effect{Policy: &policy}, nil
}

func (standingApprovalKind) Reject(context.Context, *db.Queries, Decision) error { return nil }
