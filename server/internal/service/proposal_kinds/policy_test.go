package proposalkinds

import (
	"strings"
	"testing"
	"time"
)

// Every class no standing approval may decide is listed here. A new forbidden
// class is one line in ForbiddenForStandingApproval and one row here.
func TestEveryForbiddenClassIsRefusedWhateverThePolicy(t *testing.T) {
	forbidden := []string{KeyOrgChange, KeyStandingApproval}
	for _, kind := range forbidden {
		if got := ForbiddenForStandingApproval(kind, false, []byte(`{}`)); got != PolicyRefusalForbidden {
			t.Errorf("kind %q: refusal %q, want %q", kind, got, PolicyRefusalForbidden)
		}
	}
}

func TestUrgentProposalsAreRefusedWhateverTheKind(t *testing.T) {
	for _, kind := range []string{KeyDecision, KeyOpportunity, KeyOrgChange, "goal"} {
		if got := ForbiddenForStandingApproval(kind, true, nil); got != PolicyRefusalUrgent {
			t.Errorf("urgent %q: refusal %q, want %q", kind, got, PolicyRefusalUrgent)
		}
	}
}

// A stage gate is forbidden only when it leaves the launch stage. Other gates
// and budget requests are eligible; their cost and stage are checked per filing.
func TestStageGatesAreForbiddenOnlyAtLaunch(t *testing.T) {
	cases := map[string]string{
		`{"initiative_id":"x","from":"launch","to":"operate"}`: PolicyRefusalForbidden,
		`{"initiative_id":"x","from":"define","to":"plan"}`:    "",
		`{"initiative_id":"x","from":"build","to":"launch"}`:   "",
		`not json`: PolicyRefusalForbidden,
	}
	for payload, want := range cases {
		if got := ForbiddenForStandingApproval(KeyStageGate, false, []byte(payload)); got != want {
			t.Errorf("stage_gate %s: refusal %q, want %q", payload, got, want)
		}
	}
	if got := ForbiddenForStandingApproval(KeyStageGate, false, nil); got != "" {
		t.Errorf("stage_gate kind alone: refusal %q, want none (a policy may be created)", got)
	}
}

func TestOrdinaryKindsAreNotForbidden(t *testing.T) {
	for _, kind := range []string{KeyDecision, KeyOpportunity, "goal", KeyBudget} {
		if got := ForbiddenForStandingApproval(kind, false, nil); got != "" {
			t.Errorf("kind %q: refusal %q, want none", kind, got)
		}
	}
}

func knownKinds(key string) bool {
	switch key {
	case KeyDecision, KeyOpportunity, KeyStageGate, KeyOrgChange, KeyStandingApproval, policyKindBudget:
		return true
	}
	return false
}

func TestValidatePolicySpecRefusesForbiddenAndUnknownKinds(t *testing.T) {
	now := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	for _, kind := range []string{KeyOrgChange, KeyStandingApproval, "made_up"} {
		_, err := ValidatePolicySpec(PolicySpec{Name: "x", Kind: kind}, knownKinds, now)
		if err == nil {
			t.Errorf("kind %q: a policy was accepted", kind)
		}
	}
	// The forbidden kinds are refused even when the registry does not list them
	// as unknown, so a later registration cannot open them.
	if _, err := ValidatePolicySpec(PolicySpec{Name: "x", Kind: KeyOrgChange}, func(string) bool { return true }, now); err == nil {
		t.Fatal("org_change was accepted by a registry that knows every kind")
	}
}

func TestValidatePolicySpecDefaultsAndBounds(t *testing.T) {
	now := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	spec, err := ValidatePolicySpec(PolicySpec{Name: "  Routine  ", Kind: KeyDecision}, knownKinds, now)
	if err != nil {
		t.Fatalf("a minimal policy: %v", err)
	}
	if spec.Name != "Routine" {
		t.Errorf("name = %q, want trimmed", spec.Name)
	}
	if spec.MaxPerWeek == nil || *spec.MaxPerWeek != PolicyDefaultMaxPerWeek {
		t.Errorf("max_per_week = %v, want the default %d", spec.MaxPerWeek, PolicyDefaultMaxPerWeek)
	}

	bad := []PolicySpec{
		{Name: "", Kind: KeyDecision},
		{Name: strings.Repeat("n", PolicyNameMax+1), Kind: KeyDecision},
		{Name: "x", Kind: KeyDecision, MaxPerWeek: ptr32(0)},
		{Name: "x", Kind: KeyDecision, MaxPerWeek: ptr32(51)},
		{Name: "x", Kind: KeyDecision, Match: PolicyMatch{SquadIDs: []string{"not-a-uuid"}}},
		{Name: "x", Kind: KeyDecision, Match: PolicyMatch{FloorIDs: []string{"nope"}}},
		{Name: "x", Kind: KeyDecision, Match: PolicyMatch{MaxCostMicros: ptr64(-1)}},
		{Name: "x", Kind: KeyDecision, Match: PolicyMatch{Tags: []string{"  "}}},
		{Name: "x", Kind: KeyDecision, Match: PolicyMatch{PayloadEquals: map[string]any{" ": 1}}},
		{Name: "x", Kind: KeyDecision, ValidUntil: "2026-11-02"},
		{Name: "x", Kind: KeyDecision, ValidUntil: "11/03/2026"},
	}
	for i, s := range bad {
		if _, err := ValidatePolicySpec(s, knownKinds, now); err == nil {
			t.Errorf("bad spec %d was accepted: %+v", i, s)
		}
	}
	good := PolicySpec{Name: "Today", Kind: KeyDecision, ValidUntil: "2026-11-03", MaxPerWeek: ptr32(50)}
	if _, err := ValidatePolicySpec(good, knownKinds, now); err != nil {
		t.Errorf("a policy ending today: %v", err)
	}
}

func TestStandingApprovalPayloadRoundTrips(t *testing.T) {
	spec := PolicySpec{
		Name: "Research notes", Kind: KeyDecision, ValidUntil: "2026-12-31",
		Match: PolicyMatch{SquadIDs: []string{"00000000-0000-4000-8000-000000000001"}, MaxCostMicros: ptr64(5000)},
	}
	raw, err := EncodePolicySpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodePolicySpec(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.Name != spec.Name || back.Kind != spec.Kind || back.ValidUntil != spec.ValidUntil ||
		len(back.Match.SquadIDs) != 1 || *back.Match.MaxCostMicros != 5000 {
		t.Fatalf("round trip lost fields: %+v", back)
	}
	if _, err := DecodePolicySpec([]byte(`{"name":"x","kind":"decision","colour":"red"}`)); err == nil {
		t.Fatal("an unknown field was accepted; a misspelt rule would be dropped silently")
	}
}

func ptr32(v int32) *int32 { return &v }
func ptr64(v int64) *int64 { return &v }

// The tags a filing carries come from the payload's tags array: lowercased,
// trimmed, de-duplicated, with non-strings and empty entries dropped.
func TestProposalTagsReadThePayload(t *testing.T) {
	got := ProposalTags([]byte(`{"tags":[" Docs ","docs",42,"","q4-launch"]}`))
	want := []string{"docs", "q4-launch"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ProposalTags = %v, want %v", got, want)
	}
	if none := ProposalTags([]byte(`{"name":"x"}`)); none == nil || len(none) != 0 {
		t.Fatalf("a payload with no tags = %#v, want an empty non-nil list", none)
	}
}

// A policy's match tags are checked strictly: an empty or over-long tag is refused.
func TestPolicyMatchTagsAreValidated(t *testing.T) {
	now := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	spec := PolicySpec{Name: "x", Kind: KeyDecision, Match: PolicyMatch{Tags: []string{" Docs ", "docs"}}}
	out, err := ValidatePolicySpec(spec, knownKinds, now)
	if err != nil || len(out.Match.Tags) != 1 || out.Match.Tags[0] != "docs" {
		t.Fatalf("tags %v: %v, want [docs]", out.Match.Tags, err)
	}
	spec.Match.Tags = []string{"  "}
	if _, err := ValidatePolicySpec(spec, knownKinds, now); err == nil {
		t.Fatal("an empty tag was accepted")
	}
}
