package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// EvidenceDimension is one of the six frozen scoring dimensions.
type EvidenceDimension string

const (
	DimensionAvailability EvidenceDimension = "availability"
	DimensionIsolation    EvidenceDimension = "isolation"
	DimensionSecurity     EvidenceDimension = "security"
	DimensionRecovery     EvidenceDimension = "recovery"
	DimensionPerformance  EvidenceDimension = "performance"
	DimensionObservability EvidenceDimension = "observability"
)

// allDimensions is the frozen six-dimension set (v1.3 A5.2).
var allDimensions = []EvidenceDimension{
	DimensionAvailability,
	DimensionIsolation,
	DimensionSecurity,
	DimensionRecovery,
	DimensionPerformance,
	DimensionObservability,
}

// EvidenceScore is the computed six-dimension score for one execution. Every
// dimension and overall is 0..100. The scoring function is pure: identical
// input snapshots produce byte-identical output (reproducibility contract).
type EvidenceScore struct {
	ExecutionID      string
	AlgorithmVersion string
	InputDigest      string
	Availability     int
	Isolation        int
	Security         int
	Recovery         int
	Performance      int
	Observability    int
	Overall          int
	Eligible         bool
	ComputedAt       string
	EvidenceRefs     []string
}

// ScoreInput is the canonical ordered input snapshot for scoring.
type ScoreInput struct {
	// OutputPresent marks a non-empty runtime output.
	OutputPresent bool
	// MessageCount is the number of persisted messages.
	MessageCount int
	// UsagePresent marks a persisted usage record with provider and model.
	UsagePresent bool
	// ArtifactCount is the number of required artifacts with ref+SHA-256.
	ArtifactCount int
	// RequiredArtifacts is the number of required artifacts declared.
	RequiredArtifacts int
	// TestsPassed is the number of passing required tests.
	TestsPassed int
	// RequiredTests is the number of required tests declared.
	RequiredTests int
	// ReviewRecorded marks a completed independent review.
	ReviewRecorded bool
	// EventCount is the total persisted evidence event count.
	EventCount int
	// FailureCodes is the sorted set of failure codes observed.
	FailureCodes []string
}

// inputDigest computes the deterministic SHA-256 of the canonical JSON
// serialization of the input snapshot. Field order is fixed by struct order,
// so the same input always produces the same digest.
func inputDigest(in ScoreInput) string {
	b, _ := json.Marshal(in)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ScoreEvidence computes the six-dimension score for an input snapshot.
// It is a pure function: same input -> same output, byte for byte.
func ScoreEvidence(algorithmVersion, executionID string, in ScoreInput, refs []string) EvidenceScore {
	s := EvidenceScore{
		ExecutionID:      executionID,
		AlgorithmVersion: algorithmVersion,
		InputDigest:      inputDigest(in),
		EvidenceRefs:     refs,
	}

	// availability: evidence present + gateable output.
	avail := 0
	if in.OutputPresent {
		avail += 40
	}
	if in.MessageCount >= 1 {
		avail += 30
	}
	if in.EventCount >= 3 {
		avail += 30
	}
	s.Availability = clamp100(avail)

	// isolation: no cross-scope leakage signal => 100, otherwise reduced.
	s.Isolation = 100
	for _, code := range in.FailureCodes {
		switch code {
		case "cross_scope_access", "workspace_fallback_used", "projectless_mismatch":
			s.Isolation = 50
		}
	}

	// security: secret handling / usage evidence.
	sec := 40
	if in.UsagePresent {
		sec += 30
	}
	if in.ReviewRecorded {
		sec += 30
	}
	s.Security = clamp100(sec)

	// recovery: artifacts + tests present => recoverable path.
	rec := 0
	if in.RequiredArtifacts > 0 && in.ArtifactCount == in.RequiredArtifacts {
		rec += 50
	}
	if in.RequiredTests > 0 && in.TestsPassed == in.RequiredTests {
		rec += 50
	}
	if in.RequiredArtifacts == 0 && in.RequiredTests == 0 {
		rec = 100
	}
	s.Recovery = clamp100(rec)

	// performance: proportional to completion of evidence categories.
	perf := 0
	if in.OutputPresent {
		perf += 25
	}
	if in.MessageCount >= 1 {
		perf += 25
	}
	if in.UsagePresent {
		perf += 25
	}
	if in.ReviewRecorded {
		perf += 25
	}
	s.Performance = clamp100(perf)

	// observability: event volume + review linkage.
	obs := 0
	switch {
	case in.EventCount >= 10:
		obs = 100
	case in.EventCount >= 5:
		obs = 80
	case in.EventCount >= 3:
		obs = 60
	case in.EventCount >= 1:
		obs = 40
	default:
		obs = 0
	}
	if in.ReviewRecorded {
		obs = clamp100(obs + 10)
	}
	s.Observability = obs

	// eligible: all five runtime categories plus review recorded.
	s.Eligible = in.OutputPresent &&
		in.MessageCount >= 1 &&
		in.UsagePresent &&
		(in.RequiredArtifacts == 0 || in.ArtifactCount == in.RequiredArtifacts) &&
		(in.RequiredTests == 0 || in.TestsPassed == in.RequiredTests) &&
		in.ReviewRecorded

	// overall = mean of six dimensions, rounded down to keep determinism.
	sum := s.Availability + s.Isolation + s.Security + s.Recovery + s.Performance + s.Observability
	s.Overall = sum / 6

	return s
}

func clamp100(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// VerifyInputDigest reports whether the persisted digest matches the
// canonical digest of the given input snapshot.
func VerifyInputDigest(in ScoreInput, digest string) bool {
	return inputDigest(in) == digest
}
