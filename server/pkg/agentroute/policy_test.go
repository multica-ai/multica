package agentroute

import (
	"errors"
	"reflect"
	"testing"
)

func TestRouteFailsClosedOnCapacityAndCapabilityGaps(t *testing.T) {
	t.Parallel()

	req := testRequest()
	req.Workload.RequiredSkills = []string{"security-review"}
	req.Workload.RequiredTools = []string{"github"}
	req.Workload.RequiredAuthority = []string{"repo:write"}
	req.Workload.Protected = true
	req.Candidates = []Candidate{
		candidate("unknown-capacity", "codex", 9000, 100),
		candidate("below-reserve", "claude", 9000, 250),
		candidate("missing-skill", "gemini", 9000, 100),
	}
	req.Capacities = []Capacity{
		{Provider: "codex", Known: false, RemainingPermille: 800, ReservePermille: 200},
		{Provider: "claude", Known: true, RemainingPermille: 300, ReservePermille: 200},
		{Provider: "gemini", Known: true, RemainingPermille: 800, ReservePermille: 200},
	}
	for i := range req.Candidates {
		req.Candidates[i].SupportedSkills = []string{"security-review"}
		req.Candidates[i].SupportedTools = []string{"github"}
		req.Candidates[i].AuthorityScopes = []string{"repo:write"}
		req.Candidates[i].ProtectedApproved = true
	}
	req.Candidates[1].ExpectedUsePermille = 250
	req.Candidates[2].SupportedSkills = nil

	decision, err := Route(req)
	if err == nil {
		t.Fatal("expected every candidate to be rejected")
	}
	if decision.Primary.Candidate.ID != "" {
		t.Fatalf("failed route must not elect a primary: %+v", decision.Primary)
	}
	assertRejected(t, decision, "unknown-capacity", RejectCapacityUnknown)
	assertRejected(t, decision, "below-reserve", RejectReserveProtected)
	assertRejected(t, decision, "missing-skill", RejectMissingSkill)
}

func TestRouteHardGatesEachAuthorityAndExecutionRequirement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Request, *Candidate)
		reason RejectionReason
	}{
		{
			name: "offline runtime",
			mutate: func(_ *Request, candidate *Candidate) {
				candidate.Online = false
			},
			reason: RejectOffline,
		},
		{
			name: "protected role",
			mutate: func(req *Request, candidate *Candidate) {
				req.Workload.Protected = true
				candidate.ProtectedApproved = false
			},
			reason: RejectProtectedRole,
		},
		{
			name: "required tool",
			mutate: func(req *Request, _ *Candidate) {
				req.Workload.RequiredTools = []string{"github"}
			},
			reason: RejectMissingTool,
		},
		{
			name: "required authority",
			mutate: func(req *Request, _ *Candidate) {
				req.Workload.RequiredAuthority = []string{"deploy:production"}
			},
			reason: RejectMissingAuthority,
		},
		{
			name: "usage forecast",
			mutate: func(_ *Request, candidate *Candidate) {
				candidate.ExpectedUsePermille = 0
			},
			reason: RejectForecastUnknown,
		},
		{
			name: "malformed capacity",
			mutate: func(req *Request, _ *Candidate) {
				req.Capacities[0].RemainingPermille = 1_001
			},
			reason: RejectCapacityInvalid,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := testRequest()
			candidate := candidate("only", "codex", 9000, 100)
			req.Candidates = []Candidate{candidate}
			tc.mutate(&req, &req.Candidates[0])

			decision, err := Route(req)
			if !errors.Is(err, ErrNoEligibleCandidate) {
				t.Fatalf("Route error = %v, want ErrNoEligibleCandidate", err)
			}
			assertRejected(t, decision, "only", tc.reason)
		})
	}
}

func TestRoutePreservesReserveAndBalancesTowardHeadroom(t *testing.T) {
	t.Parallel()

	req := testRequest()
	req.Candidates = []Candidate{
		candidate("codex-tight", "codex", 9000, 150),
		candidate("claude-roomy", "claude", 9000, 150),
	}
	req.Capacities = []Capacity{
		{Provider: "codex", Known: true, RemainingPermille: 400, ReservePermille: 200},
		{Provider: "claude", Known: true, RemainingPermille: 850, ReservePermille: 200},
	}

	decision, err := Route(req)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if got := decision.Primary.Candidate.ID; got != "claude-roomy" {
		t.Fatalf("expected headroom-aware balancing, got %q", got)
	}

	req.Workload.Urgency = UrgencyEmergency
	req.Candidates = []Candidate{candidate("emergency", "codex", 9000, 250)}
	decision, err = Route(req)
	if err != nil {
		t.Fatalf("emergency route should be allowed to consume reserve: %v", err)
	}
	if decision.Primary.Candidate.ID != "emergency" {
		t.Fatalf("unexpected emergency primary: %+v", decision.Primary)
	}
}

func TestRouteTreatsRemainingBelowReserveAsProtectedNotMalformed(t *testing.T) {
	t.Parallel()

	req := testRequest()
	req.Candidates = []Candidate{candidate("ordinary", "codex", 9000, 1)}
	req.Capacities = []Capacity{
		{Provider: "codex", Known: true, RemainingPermille: 100, ReservePermille: 200},
	}

	decision, err := Route(req)
	if !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("Route error = %v, want ErrNoEligibleCandidate", err)
	}
	assertRejected(t, decision, "ordinary", RejectReserveProtected)

	req.Workload.Urgency = UrgencyEmergency
	decision, err = Route(req)
	if err != nil {
		t.Fatalf("emergency work may use remaining reserve: %v", err)
	}
	if decision.Primary.Candidate.ID != "ordinary" {
		t.Fatalf("unexpected emergency primary: %+v", decision.Primary)
	}
}

func TestRouteUsesOnlyPromotedEvidenceBackedAffinity(t *testing.T) {
	t.Parallel()

	req := testRequest()
	req.Workload.RequiredSkills = []string{"architecture"}
	a := candidate("a", "codex", 8000, 100)
	b := candidate("b", "claude", 8000, 100)
	a.SupportedSkills = []string{"architecture"}
	b.SupportedSkills = []string{"architecture"}
	req.Candidates = []Candidate{a, b}
	req.Affinities = []SkillAffinity{
		{Skill: "architecture", Provider: "codex", Status: AffinityExperimental, ScoreBP: 9000, EvidenceRevision: "eval-1"},
		{Skill: "architecture", Provider: "claude", Status: AffinityPromoted, ScoreBP: 600, EvidenceRevision: ""},
	}

	first, err := Route(req)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if first.Primary.Candidate.ID != "a" {
		t.Fatalf("unpromoted affinity must not affect deterministic tie break: %+v", first.Primary)
	}

	req.Affinities[1].EvidenceRevision = "holdout-2026-07"
	second, err := Route(req)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if second.Primary.Candidate.ID != "b" {
		t.Fatalf("promoted evidence-backed affinity should change ranking: %+v", second.Primary)
	}
}

func TestRouteTopologyAndWriteOwnership(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(*Request)
		topology Topology
		roles    []AssignmentRole
	}{
		{
			name: "serial dependencies",
			mutate: func(req *Request) {
				req.Workload.HasDependencies = true
			},
			topology: TopologySerial,
			roles:    []AssignmentRole{RoleLead},
		},
		{
			name: "bounded parallel exploration",
			mutate: func(req *Request) {
				req.Workload.IndependentBranches = 3
				req.Workload.AmbiguityBP = 7000
				req.Workload.MaxParallel = 3
			},
			topology: TopologyBoundedParallel,
			roles:    []AssignmentRole{RoleLead, RoleExplorer, RoleExplorer},
		},
		{
			name: "cross provider review",
			mutate: func(req *Request) {
				req.Workload.Risk = RiskHigh
				req.Workload.AmbiguityBP = 7000
				req.Workload.AllowCrossProviderReview = true
			},
			topology: TopologyCrossProviderReview,
			roles:    []AssignmentRole{RoleLead, RoleCritic},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := testRequest()
			req.Candidates = []Candidate{
				candidate("codex-a", "codex", 9100, 100),
				candidate("claude-a", "claude", 9000, 100),
				candidate("codex-b", "codex", 8900, 100),
			}
			tc.mutate(&req)
			decision, err := Route(req)
			if err != nil {
				t.Fatalf("Route: %v", err)
			}
			if decision.Topology != tc.topology {
				t.Fatalf("topology = %q, want %q", decision.Topology, tc.topology)
			}
			var roles []AssignmentRole
			writers := 0
			for _, assignment := range decision.Assignments {
				roles = append(roles, assignment.Role)
				if assignment.WriteCapable {
					writers++
				}
			}
			if !reflect.DeepEqual(roles, tc.roles) {
				t.Fatalf("roles = %v, want %v", roles, tc.roles)
			}
			if writers != 1 || !decision.Assignments[0].WriteCapable {
				t.Fatalf("expected exactly one write-capable lead: %+v", decision.Assignments)
			}
			if tc.topology == TopologyCrossProviderReview &&
				decision.Assignments[0].Candidate.Provider == decision.Assignments[1].Candidate.Provider {
				t.Fatalf("critic must use a distinct provider: %+v", decision.Assignments)
			}
		})
	}
}

func TestBoundedParallelUsesAggregateProviderBudget(t *testing.T) {
	t.Parallel()

	req := testRequest()
	req.Workload.IndependentBranches = 3
	req.Workload.AmbiguityBP = 7000
	req.Workload.MaxParallel = 3
	req.Candidates = []Candidate{
		candidate("codex-a", "codex", 10000, 250),
		candidate("codex-b", "codex", 9900, 250),
		candidate("claude-a", "claude", 8900, 250),
	}
	req.Capacities = []Capacity{
		{Provider: "codex", Known: true, RemainingPermille: 600, ReservePermille: 200},
		{Provider: "claude", Known: true, RemainingPermille: 800, ReservePermille: 200},
	}

	decision, err := Route(req)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if decision.Topology != TopologyBoundedParallel {
		t.Fatalf("topology = %q, want bounded parallel", decision.Topology)
	}
	if got := candidateIDsFromAssignments(decision.Assignments); !reflect.DeepEqual(got, []string{"codex-a", "claude-a"}) {
		t.Fatalf("aggregate reserve guard selected oversubscribed branch: %v", got)
	}
}

func TestRouteFallbacksAreCrossProviderAndDeterministic(t *testing.T) {
	t.Parallel()

	req := testRequest()
	req.Candidates = []Candidate{
		candidate("codex-b", "codex", 9000, 100),
		candidate("claude-b", "claude", 9000, 100),
		candidate("claude-a", "claude", 9000, 100),
		candidate("codex-a", "codex", 9000, 100),
	}

	first, err := Route(req)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	req.Candidates[0], req.Candidates[3] = req.Candidates[3], req.Candidates[0]
	req.Candidates[1], req.Candidates[2] = req.Candidates[2], req.Candidates[1]
	second, err := Route(req)
	if err != nil {
		t.Fatalf("Route after reorder: %v", err)
	}
	if first.Primary.Candidate.ID != second.Primary.Candidate.ID {
		t.Fatalf("input order changed primary: %q != %q", first.Primary.Candidate.ID, second.Primary.Candidate.ID)
	}
	if !reflect.DeepEqual(candidateIDs(first.Fallbacks), candidateIDs(second.Fallbacks)) {
		t.Fatalf("input order changed fallbacks: %v != %v", candidateIDs(first.Fallbacks), candidateIDs(second.Fallbacks))
	}
	for _, fallback := range first.Fallbacks {
		if fallback.Candidate.Provider == first.Primary.Candidate.Provider {
			t.Fatalf("same-provider fallback is forbidden: %+v", fallback)
		}
	}
}

func testRequest() Request {
	return Request{
		Workload: Workload{ID: "work-1", Risk: RiskMedium, Urgency: UrgencyNormal, MaxParallel: 3},
		Capacities: []Capacity{
			{Provider: "codex", Known: true, RemainingPermille: 800, ReservePermille: 200},
			{Provider: "claude", Known: true, RemainingPermille: 800, ReservePermille: 200},
		},
	}
}

func candidate(id, provider string, qualityBP, expectedUsePermille int) Candidate {
	return Candidate{
		ID:                  id,
		Provider:            provider,
		Model:               provider + "-model",
		Thinking:            "high",
		Online:              true,
		QualityBP:           qualityBP,
		LatencyPenaltyBP:    100,
		ExpectedUsePermille: expectedUsePermille,
		ProtectedApproved:   true,
	}
}

func assertRejected(t *testing.T, decision Decision, candidateID string, reason RejectionReason) {
	t.Helper()
	for _, rejection := range decision.Rejections {
		if rejection.CandidateID == candidateID && rejection.Reason == reason {
			return
		}
	}
	t.Fatalf("missing rejection %q/%q in %+v", candidateID, reason, decision.Rejections)
}

func candidateIDs(candidates []RankedCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.Candidate.ID)
	}
	return out
}

func candidateIDsFromAssignments(assignments []Assignment) []string {
	out := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		out = append(out, assignment.Candidate.ID)
	}
	return out
}
