package availabilityevidence

import (
	"context"
	"errors"
	"testing"
)

func allowed(name string) Proof {
	return Proof{Subject: Subject{Name: name, Authorized: true}, Outcome: OutcomeAllowed}
}

func denied(name string) Proof {
	return Proof{Subject: Subject{Name: name, Authorized: false}, Outcome: OutcomeDenied}
}

func TestClassifyLevels(t *testing.T) {
	tests := []struct {
		name string
		obs  Observation
		want Level
	}{
		{
			name: "absent from the surface is only a declaration",
			obs:  Observation{Present: false, Proofs: []Proof{allowed("agent"), denied("stranger")}},
			want: LevelDeclared,
		},
		{
			name: "found but never called is discovered",
			obs:  Observation{Present: true},
			want: LevelDiscovered,
		},
		{
			name: "proven access alone is not verified",
			obs:  Observation{Present: true, Proofs: []Proof{allowed("agent")}},
			want: LevelDiscovered,
		},
		{
			name: "proven refusal alone is not verified",
			obs:  Observation{Present: true, Proofs: []Proof{denied("stranger")}},
			want: LevelDiscovered,
		},
		{
			name: "both directions proven is verified",
			obs:  Observation{Present: true, Proofs: []Proof{allowed("agent"), denied("stranger")}},
			want: LevelVerified,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.obs)
			if got.Level != tc.want {
				t.Fatalf("level = %q, want %q (reason: %s)", got.Level, tc.want, got.Reason)
			}
			if got.Reason == "" {
				t.Fatal("every classification must state a reason")
			}
		})
	}
}

// A gate that lets an unauthorized caller through is broken, not verified. If
// this ever passes as verified, the model would certify an open door.
func TestClassifyGateLettingStrangerThroughIsNotVerified(t *testing.T) {
	got := Classify(Observation{
		CapabilityID: "platform:get_agent_capabilities",
		Present:      true,
		Proofs: []Proof{
			allowed("agent"),
			{Subject: Subject{Name: "stranger", Authorized: false}, Outcome: OutcomeAllowed},
		},
	})
	if got.Level == LevelVerified {
		t.Fatal("a gate that allowed an unauthorized subject must never be verified")
	}
	if got.Level != LevelDiscovered {
		t.Fatalf("level = %q, want %q", got.Level, LevelDiscovered)
	}
}

func TestClassifyAuthorizedSubjectDeniedIsContradiction(t *testing.T) {
	got := Classify(Observation{
		Present: true,
		Proofs: []Proof{
			{Subject: Subject{Name: "agent", Authorized: true}, Outcome: OutcomeDenied},
			denied("stranger"),
		},
	})
	if got.Level != LevelDiscovered {
		t.Fatalf("level = %q, want %q", got.Level, LevelDiscovered)
	}
}

func TestOnlyVerifiedIsReality(t *testing.T) {
	if LevelDeclared.IsReality() || LevelDiscovered.IsReality() {
		t.Fatal("only verified may be presented as reality")
	}
	if !LevelVerified.IsReality() {
		t.Fatal("verified must be reality")
	}
}

func TestLedgerRealityOmitsUnproven(t *testing.T) {
	var l Ledger
	l.Record(Observation{CapabilityID: "a", RuntimeType: RuntimeLocal, Present: true, Proofs: []Proof{allowed("agent"), denied("stranger")}})
	l.Record(Observation{CapabilityID: "b", RuntimeType: RuntimeLocal, Present: true, Proofs: []Proof{allowed("agent")}})
	l.Record(Observation{CapabilityID: "c", RuntimeType: RuntimeLocal, Present: false})

	if got := len(l.All()); got != 3 {
		t.Fatalf("All() = %d entries, want 3", got)
	}
	reality := l.Reality()
	if len(reality) != 1 || reality[0].CapabilityID != "a" {
		t.Fatalf("Reality() = %+v, want only the verified capability a", reality)
	}
}

// Re-probing must report current truth, not accumulate stale claims.
func TestLedgerRecordReplacesEarlierEvidence(t *testing.T) {
	var l Ledger
	l.Record(Observation{CapabilityID: "a", RuntimeType: RuntimeLocal, Present: true, Proofs: []Proof{allowed("agent"), denied("stranger")}})
	l.Record(Observation{CapabilityID: "a", RuntimeType: RuntimeLocal, Present: false})

	if got := l.Lookup("a", RuntimeLocal); got.Level != LevelDeclared {
		t.Fatalf("level = %q, want %q — a re-probe must overwrite stale evidence", got.Level, LevelDeclared)
	}
	if got := len(l.All()); got != 1 {
		t.Fatalf("All() = %d entries, want 1", got)
	}
}

// Evidence is per runtime: the same capability may be real on one and absent on
// another, and the ledger must never let one runtime's proof speak for another.
func TestLedgerKeepsRuntimesSeparate(t *testing.T) {
	var l Ledger
	l.Record(Observation{CapabilityID: "a", RuntimeType: RuntimeLocal, Present: true, Proofs: []Proof{allowed("agent"), denied("stranger")}})
	l.Record(Observation{CapabilityID: "a", RuntimeType: RuntimeFirtalGateway, Present: false})

	if got := l.Lookup("a", RuntimeLocal).Level; got != LevelVerified {
		t.Fatalf("local level = %q, want %q", got, LevelVerified)
	}
	if got := l.Lookup("a", RuntimeFirtalGateway).Level; got != LevelDeclared {
		t.Fatalf("gateway level = %q, want %q", got, LevelDeclared)
	}
}

// A capability nobody probed must read as unproven, never as absent.
func TestLookupUnprobedIsDeclaredWithReason(t *testing.T) {
	var l Ledger
	got := l.Lookup("never-probed", RuntimeClaudeCode)
	if got.Level != LevelDeclared {
		t.Fatalf("level = %q, want %q", got.Level, LevelDeclared)
	}
	if got.Reason == "" {
		t.Fatal("an unprobed capability must say why it is unproven")
	}
}

func TestRuntimeTypesCoversThreeFamilies(t *testing.T) {
	got := RuntimeTypes()
	if len(got) != 3 {
		t.Fatalf("RuntimeTypes() = %v, want the three runtime families", got)
	}
	seen := map[RuntimeType]bool{}
	for _, rt := range got {
		if seen[rt] {
			t.Fatalf("duplicate runtime type %q", rt)
		}
		seen[rt] = true
	}
	for _, want := range []RuntimeType{RuntimeFirtalGateway, RuntimeClaudeCode, RuntimeLocal} {
		if !seen[want] {
			t.Fatalf("RuntimeTypes() is missing %q", want)
		}
	}
}

// ── probe ────────────────────────────────────────────────────────────────────

type fakeGate struct {
	allowAuthorized   bool
	allowUnauthorized bool
	err               error
}

func (g fakeGate) Decide(_ context.Context, _ string, s Subject) (Outcome, error) {
	if g.err != nil {
		return "", g.err
	}
	if s.Authorized {
		if g.allowAuthorized {
			return OutcomeAllowed, nil
		}
		return OutcomeDenied, nil
	}
	if g.allowUnauthorized {
		return OutcomeAllowed, nil
	}
	return OutcomeDenied, nil
}

type erroringSurface struct{}

func (erroringSurface) RuntimeType() RuntimeType { return RuntimeLocal }
func (erroringSurface) Present(context.Context, string) (bool, error) {
	return false, errors.New("runtime unreachable")
}

func TestProbeVerifiesWorkingGate(t *testing.T) {
	var l Ledger
	p := Prober{
		Surface: NewStaticSurface(RuntimeFirtalGateway, []string{"platform:get_agent_capabilities"}),
		Gate:    fakeGate{allowAuthorized: true},
	}
	if err := p.Probe(context.Background(), &l, []string{"platform:get_agent_capabilities"},
		Subject{Name: "agent", Authorized: true}, Subject{Name: "stranger"}); err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	got := l.Lookup("platform:get_agent_capabilities", RuntimeFirtalGateway)
	if got.Level != LevelVerified {
		t.Fatalf("level = %q, want %q (reason: %s)", got.Level, LevelVerified, got.Reason)
	}
	if len(got.Proofs) != 2 {
		t.Fatalf("got %d proofs, want both an access and a refusal proof", len(got.Proofs))
	}
}

func TestProbeWithoutGateNeverVerifies(t *testing.T) {
	var l Ledger
	p := Prober{Surface: NewStaticSurface(RuntimeLocal, []string{"x"})}
	if err := p.Probe(context.Background(), &l, []string{"x"}, Subject{Name: "agent", Authorized: true}, Subject{Name: "stranger"}); err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if got := l.Lookup("x", RuntimeLocal); got.Level != LevelDiscovered {
		t.Fatalf("level = %q, want %q — no gate means no proof", got.Level, LevelDiscovered)
	}
}

func TestProbeMissingCapabilityIsDeclared(t *testing.T) {
	var l Ledger
	p := Prober{
		Surface: NewStaticSurface(RuntimeFirtalGateway, []string{"other"}),
		Gate:    fakeGate{allowAuthorized: true},
	}
	if err := p.Probe(context.Background(), &l, []string{"missing"}, Subject{Name: "agent", Authorized: true}, Subject{Name: "stranger"}); err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if got := l.Lookup("missing", RuntimeFirtalGateway); got.Level != LevelDeclared {
		t.Fatalf("level = %q, want %q", got.Level, LevelDeclared)
	}
}

// A surface that cannot be read must not look like a clean absence.
func TestProbeSurfaceErrorIsReportedNotSwallowed(t *testing.T) {
	var l Ledger
	p := Prober{Surface: erroringSurface{}, Gate: fakeGate{allowAuthorized: true}}
	if err := p.Probe(context.Background(), &l, []string{"x"}, Subject{Name: "agent", Authorized: true}, Subject{Name: "stranger"}); err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	got := l.Lookup("x", RuntimeLocal)
	if got.Level != LevelDeclared {
		t.Fatalf("level = %q, want %q", got.Level, LevelDeclared)
	}
	if got.Reason == "" || got.Level.IsReality() {
		t.Fatalf("a probe failure must be reported as unproven with a reason, got %+v", got)
	}
}

func TestProbeRequiresSurface(t *testing.T) {
	var l Ledger
	err := (Prober{}).Probe(context.Background(), &l, []string{"x"}, Subject{}, Subject{})
	if err == nil {
		t.Fatal("a probe without a surface must fail loudly")
	}
}
