package proposalkinds

import (
	"context"
	"errors"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type noInitiatives struct{}

func (noInitiatives) OpenFromOpportunity(context.Context, *db.Queries, Decision, Opportunity) error {
	return nil
}
func (noInitiatives) AdvanceFromGate(context.Context, *db.Queries, Decision, Gate) error { return nil }
func (noInitiatives) ReturnFromGate(context.Context, *db.Queries, Decision, Gate) error  { return nil }

func TestRegistry_UnknownKindIsRefused(t *testing.T) {
	r := NewRegistry(noInitiatives{})
	if _, err := r.Lookup("teleport"); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("an unknown kind must be refused with ErrUnknownKind, got %v", err)
	}
}

func TestRegistry_EmptyKeyIsADecision(t *testing.T) {
	k, err := NewRegistry(nil).Lookup("")
	if err != nil || k.Key() != KeyDecision {
		t.Fatalf("the empty key should be the decision kind, got %v, %v", k, err)
	}
}

func TestRegistry_StageGatesAreNotFiledByTheAPI(t *testing.T) {
	r := NewRegistry(noInitiatives{})
	for key, want := range map[string]bool{KeyDecision: true, KeyOpportunity: true, KeyStageGate: false} {
		k, err := r.Lookup(key)
		if err != nil {
			t.Fatalf("lookup %s: %v", key, err)
		}
		if k.FiledByAgents() != want {
			t.Errorf("%s FiledByAgents = %v, want %v", key, k.FiledByAgents(), want)
		}
	}
}

func TestDecisionKind_RefusesAPayload(t *testing.T) {
	k, _ := NewRegistry(nil).Lookup(KeyDecision)
	if out, err := k.Validate(context.Background(), Env{}, []byte(`{}`)); err != nil || string(out) != "{}" {
		t.Fatalf("an empty payload is a decision: %q, %v", out, err)
	}
	if _, err := k.Validate(context.Background(), Env{}, []byte(`{"handoff_squad_id":"x"}`)); err == nil {
		t.Fatalf("a decision takes no payload fields; the handoff is a column, not a payload field")
	}
}

func TestOpportunity_NeedsAThesisAndRejectsUnknownFields(t *testing.T) {
	k, _ := NewRegistry(noInitiatives{}).Lookup(KeyOpportunity)
	ctx := context.Background()
	if _, err := k.Validate(ctx, Env{}, []byte(`{"market":"SMB"}`)); err == nil {
		t.Fatalf("an opportunity without a thesis is refused")
	}
	if _, err := k.Validate(ctx, Env{}, []byte(`{"thesis":"x","colour":"red"}`)); err == nil {
		t.Fatalf("an unknown field is refused rather than dropped")
	}
	if _, err := k.Validate(ctx, Env{}, []byte(`[1,2]`)); err == nil {
		t.Fatalf("a payload that is not an object is refused")
	}
}

func TestStageGate_NeedsAnInitiativeIdAndStages(t *testing.T) {
	k, _ := NewRegistry(noInitiatives{}).Lookup(KeyStageGate)
	ctx := context.Background()
	if _, err := k.Validate(ctx, Env{}, []byte(`{"initiative_id":"not-a-uuid","from":"validate","to":"define"}`)); err == nil {
		t.Fatalf("a malformed initiative id is refused before any lookup")
	}
	if _, err := k.Validate(ctx, Env{}, []byte(`{"initiative_id":"11111111-1111-1111-1111-111111111111","from":"validate"}`)); err == nil {
		t.Fatalf("a gate without its next stage is refused")
	}
}

func TestOutcomes(t *testing.T) {
	for _, ok := range []string{OutcomeContinue, OutcomeScale, OutcomeSunset} {
		if !ValidOutcome(ok) {
			t.Errorf("%s should be a valid outcome", ok)
		}
	}
	if ValidOutcome("delete") {
		t.Errorf("delete is not an outcome")
	}
}
