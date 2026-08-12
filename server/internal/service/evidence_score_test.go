package service

import "testing"

func evidenceScoresEqual(a, b EvidenceScore) bool {
	if a.ExecutionID != b.ExecutionID ||
		a.AlgorithmVersion != b.AlgorithmVersion ||
		a.InputDigest != b.InputDigest ||
		a.Availability != b.Availability ||
		a.Isolation != b.Isolation ||
		a.Security != b.Security ||
		a.Recovery != b.Recovery ||
		a.Performance != b.Performance ||
		a.Observability != b.Observability ||
		a.Overall != b.Overall ||
		a.Eligible != b.Eligible ||
		a.ComputedAt != b.ComputedAt {
		return false
	}
	if len(a.EvidenceRefs) != len(b.EvidenceRefs) {
		return false
	}
	for i := range a.EvidenceRefs {
		if a.EvidenceRefs[i] != b.EvidenceRefs[i] {
			return false
		}
	}
	return true
}

func TestScoreEvidenceDeterministic(t *testing.T) {
	in := ScoreInput{
		OutputPresent:    true,
		MessageCount:     4,
		UsagePresent:     true,
		ArtifactCount:    2,
		RequiredArtifacts: 2,
		TestsPassed:      1,
		RequiredTests:    1,
		ReviewRecorded:   true,
		EventCount:       8,
	}
	a := ScoreEvidence("h6-v1", "exec-1", in, []string{"ref-1"})
	b := ScoreEvidence("h6-v1", "exec-1", in, []string{"ref-1"})
	if !evidenceScoresEqual(a, b) {
		t.Fatalf("recomputation not byte-identical: %+v vs %+v", a, b)
	}
	if a.InputDigest == "" {
		t.Fatal("input digest empty")
	}
	if !VerifyInputDigest(in, a.InputDigest) {
		t.Fatal("input digest does not verify")
	}
	for _, dim := range []int{a.Availability, a.Isolation, a.Security, a.Recovery, a.Performance, a.Observability, a.Overall} {
		if dim < 0 || dim > 100 {
			t.Fatalf("dimension out of range: %d", dim)
		}
	}
	if !a.Eligible {
		t.Fatal("eligible should be true for complete evidence")
	}
}

func TestScoreEvidenceIneligibleWhenMissing(t *testing.T) {
	in := ScoreInput{
		OutputPresent:  true,
		MessageCount:   1,
		UsagePresent:   false, // missing usage
		ReviewRecorded: true,
	}
	s := ScoreEvidence("h6-v1", "exec-2", in, nil)
	if s.Eligible {
		t.Fatal("eligible should be false when usage missing")
	}
	if s.Security > 70 {
		t.Fatalf("security too high without usage: %d", s.Security)
	}
}

func TestScoreEvidenceDifferentInputsDifferentDigest(t *testing.T) {
	a := ScoreEvidence("h6-v1", "e", ScoreInput{OutputPresent: true}, nil)
	b := ScoreEvidence("h6-v1", "e", ScoreInput{OutputPresent: false}, nil)
	if a.InputDigest == b.InputDigest {
		t.Fatal("different inputs must produce different digests")
	}
}

func TestScoreEvidenceIsolationDegradesOnCrossScope(t *testing.T) {
	in := ScoreInput{FailureCodes: []string{"cross_scope_access"}}
	s := ScoreEvidence("h6-v1", "e", in, nil)
	if s.Isolation != 50 {
		t.Fatalf("isolation = %d, want 50", s.Isolation)
	}
	clean := ScoreEvidence("h6-v1", "e", ScoreInput{}, nil)
	if clean.Isolation != 100 {
		t.Fatalf("clean isolation = %d, want 100", clean.Isolation)
	}
}
