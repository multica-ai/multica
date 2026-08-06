package companybraincensus

import (
	"slices"
	"testing"
	"time"
)

func TestEvaluateParityMatchesOnlyExactDeterministicEvidence(t *testing.T) {
	report, target := parityFixture()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)

	first := EvaluateParity(report, []TargetPermission{target}, 12, "logical-company-brain", now)
	if len(first) != 1 {
		t.Fatalf("evaluations = %d, want 1", len(first))
	}
	got := first[0]
	if got.Status != ParityMatched || got.BlockerCode != "" {
		t.Fatalf("evaluation = %#v, want exact match", got)
	}
	if got.AgentID != "agent-1" || got.TargetPermissionID != "permission-1" {
		t.Fatalf("evaluation identity = %#v, want agent and permission identity", got)
	}
	if got.CensusVersion != 12 || got.AccessVersion != 7 {
		t.Fatalf("evaluation versions = %#v, want census 12 and access 7", got)
	}
	if got.LegacyToolCount != 2 || got.TargetToolCount != 2 {
		t.Fatalf("tool counts = %d/%d, want 2/2", got.LegacyToolCount, got.TargetToolCount)
	}
	if got.LegacyWriteSource != "commercial" || got.TargetWriteSource != "commercial" {
		t.Fatalf("write destinations = %q/%q, want commercial", got.LegacyWriteSource, got.TargetWriteSource)
	}
	for name, value := range map[string]string{
		"legacy access":          got.LegacyAccessSHA256,
		"target access":          got.TargetAccessSHA256,
		"legacy approval":        got.LegacyApprovalSHA256,
		"target approval":        got.TargetApprovalSHA256,
		"legacy tool calls":      got.LegacyToolCallsSHA256,
		"target tool calls":      got.TargetToolCallsSHA256,
		"complete evidence hash": got.EvidenceSHA256,
	} {
		if len(value) != 64 {
			t.Fatalf("%s hash = %q, want sha256", name, value)
		}
	}
	if got.LegacyAccessSHA256 != got.TargetAccessSHA256 ||
		got.LegacyApprovalSHA256 != got.TargetApprovalSHA256 ||
		got.LegacyToolCallsSHA256 != got.TargetToolCallsSHA256 {
		t.Fatalf("matched evaluation contains unequal hashes: %#v", got)
	}

	report.Actors[0].Sources[0], report.Actors[0].Sources[1] =
		report.Actors[0].Sources[1], report.Actors[0].Sources[0]
	for i := range report.Actors[0].Sources {
		slices.Reverse(report.Actors[0].Sources[i].ToolAccess)
	}
	slices.Reverse(target.AllowedReadSources)
	slices.Reverse(target.ApprovalOutcomes)
	slices.Reverse(target.CanonicalToolCalls)

	reordered := EvaluateParity(report, []TargetPermission{target}, 12, "logical-company-brain", now)
	if len(reordered) != 1 || reordered[0] != got {
		t.Fatalf("input order changed deterministic result:\nfirst: %#v\nagain: %#v", got, reordered)
	}
}

func TestEvaluateParityBlocksEveryMissingOrMismatchedDimension(t *testing.T) {
	report, exact := parityFixture()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		mutate  func(*Report, *TargetPermission)
		blocker ParityBlockerCode
	}{
		{
			name: "access",
			mutate: func(_ *Report, target *TargetPermission) {
				target.AllowedReadSources = []string{"commercial"}
			},
			blocker: BlockerAccessMismatch,
		},
		{
			name: "approval",
			mutate: func(_ *Report, target *TargetPermission) {
				target.ApprovalOutcomes[0].Decision = "deny"
			},
			blocker: BlockerApprovalMismatch,
		},
		{
			name: "tool count",
			mutate: func(_ *Report, target *TargetPermission) {
				target.CanonicalToolCalls = target.CanonicalToolCalls[:1]
			},
			blocker: BlockerToolCountMismatch,
		},
		{
			name: "canonical tool call",
			mutate: func(_ *Report, target *TargetPermission) {
				target.CanonicalToolCalls[0] = "advisor"
			},
			blocker: BlockerToolCallMismatch,
		},
		{
			name: "write destination",
			mutate: func(_ *Report, target *TargetPermission) {
				target.WriteSource = "shared"
			},
			blocker: BlockerWriteDestinationMismatch,
		},
		{
			name: "unverifiable census evidence",
			mutate: func(report *Report, _ *TargetPermission) {
				report.Actors[0].Sources[0].Status = statusUnverifiable
				report.Actors[0].Sources[0].ErrorCode = errorUpstreamUnavailable
				report.Actors[0].Sources[0].Claim = nil
			},
			blocker: BlockerCensusEvidenceMissing,
		},
		{
			name: "missing frozen census timestamp",
			mutate: func(report *Report, _ *TargetPermission) {
				report.GeneratedAt = time.Time{}
			},
			blocker: BlockerCensusEvidenceMissing,
		},
		{
			name: "ambiguous legacy write destination",
			mutate: func(report *Report, _ *TargetPermission) {
				report.Actors[0].Sources[1].Status = statusVerified
				report.Actors[0].Sources[1].ErrorCode = ""
				report.Actors[0].Sources[1].Claim = &Claim{
					WriteSource:        "shared",
					AllowedReadSources: []string{"shared"},
				}
			},
			blocker: BlockerLegacyWriteSourceAmbiguous,
		},
		{
			name: "missing target evidence",
			mutate: func(_ *Report, target *TargetPermission) {
				target.ApprovalOutcomes = nil
			},
			blocker: BlockerTargetEvidenceMissing,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotReport, gotTarget := parityFixture()
			tc.mutate(&gotReport, &gotTarget)
			got := EvaluateParity(
				gotReport,
				[]TargetPermission{gotTarget},
				12,
				"logical-company-brain",
				now,
			)
			if len(got) != 1 || got[0].Status != ParityBlocked || got[0].BlockerCode != tc.blocker {
				t.Fatalf("evaluation = %#v, want blocked by %q", got, tc.blocker)
			}
		})
	}

	t.Run("missing permission", func(t *testing.T) {
		got := EvaluateParity(report, nil, 12, "logical-company-brain", now)
		if len(got) != 1 || got[0].BlockerCode != BlockerTargetPermissionMissing {
			t.Fatalf("evaluation = %#v, want missing-permission blocker", got)
		}
	})

	t.Run("ambiguous permission", func(t *testing.T) {
		duplicate := exact
		duplicate.PermissionID = "permission-2"
		got := EvaluateParity(
			report,
			[]TargetPermission{exact, duplicate},
			12,
			"logical-company-brain",
			now,
		)
		if len(got) != 1 || got[0].BlockerCode != BlockerTargetPermissionAmbiguous {
			t.Fatalf("evaluation = %#v, want ambiguous-permission blocker", got)
		}
	})
}

func TestEvaluateParityReturnsOneResultForEveryEligibleAgent(t *testing.T) {
	report, target := parityFixture()
	second := report.Actors[0]
	second.AgentID = "agent-2"
	second.Name = "Beta"
	report.Actors = append(report.Actors, second)

	got := EvaluateParity(
		report,
		[]TargetPermission{target},
		12,
		"logical-company-brain",
		time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC),
	)
	if len(got) != 2 {
		t.Fatalf("evaluations = %#v, want one result per census actor", got)
	}
	if got[0].AgentID != "agent-1" || got[0].Status != ParityMatched {
		t.Fatalf("first evaluation = %#v, want matched agent-1", got[0])
	}
	if got[1].AgentID != "agent-2" ||
		got[1].BlockerCode != BlockerTargetPermissionMissing {
		t.Fatalf("second evaluation = %#v, want blocked agent-2", got[1])
	}
}

func TestEvaluateParityBlocksDuplicateCensusActorsOnce(t *testing.T) {
	report, target := parityFixture()
	report.Actors = append(report.Actors, report.Actors[0])

	got := EvaluateParity(
		report,
		[]TargetPermission{target},
		12,
		"logical-company-brain",
		time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC),
	)
	if len(got) != 1 ||
		got[0].Status != ParityBlocked ||
		got[0].BlockerCode != BlockerCensusEvidenceAmbiguous {
		t.Fatalf("evaluation = %#v, want one ambiguity blocker", got)
	}
}

func parityFixture() (Report, TargetPermission) {
	report := Report{
		GeneratedAt: time.Date(2026, 7, 29, 18, 0, 0, 0, time.UTC),
		Actors: []actor{{
			AgentID: "agent-1",
			Name:    "Alpha",
			Status:  "online",
			Sources: []connectionClaim{
				{
					ConnectionID:   "legacy-commercial",
					ConnectionName: "company-brain-commercial",
					Claim: &Claim{
						WriteSource:        "commercial",
						AllowedReadSources: []string{"shared", "commercial"},
					},
					ToolAccess: []toolAccess{
						{Tool: "search", Decision: "allow"},
						{Tool: "add_fact", Decision: "ask"},
					},
					Status: statusVerified,
				},
				{
					ConnectionID:   "legacy-shared",
					ConnectionName: "company-brain-shared",
					ToolAccess: []toolAccess{
						{Tool: "search", Decision: "deny"},
						{Tool: "add_fact", Decision: "deny"},
					},
					Status:    statusUnverifiable,
					ErrorCode: errorAccessDenied,
				},
			},
		}},
	}
	target := TargetPermission{
		PermissionID:             "permission-1",
		CompanyBrainConnectionID: "logical-company-brain",
		AgentID:                  "agent-1",
		AccessVersion:            7,
		AllowedReadSources:       []string{"commercial", "shared"},
		WriteSource:              "commercial",
		ApprovalOutcomes: []ScopedToolDecision{
			{Source: "commercial", Tool: "add_fact", Decision: "ask"},
			{Source: "commercial", Tool: "search", Decision: "allow"},
			{Source: "shared", Tool: "add_fact", Decision: "deny"},
			{Source: "shared", Tool: "search", Decision: "deny"},
		},
		CanonicalToolCalls: []string{"add_fact", "search"},
	}
	return report, target
}
