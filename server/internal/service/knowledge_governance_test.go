package service

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAssessKnowledgeGovernanceMarksNegativeFeedback(t *testing.T) {
	row := db.ListKnowledgeGovernanceCandidatesRow{
		UpdatedAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
		HelpfulCount:    1,
		NotHelpfulCount: 2,
		OutdatedCount:   2,
		InjectionCount:  8,
		UsageCount:      0,
	}

	got := assessKnowledgeGovernance(row, "")
	if got.staleScore < 70 {
		t.Fatalf("staleScore = %.2f, want >= 70", got.staleScore)
	}
	if got.effectivenessScore > 60 {
		t.Fatalf("effectivenessScore = %.2f, want <= 60", got.effectivenessScore)
	}
	if got.reviewReason == "" {
		t.Fatal("reviewReason is empty")
	}
	if got.updateSuggestion == "" {
		t.Fatal("updateSuggestion is empty")
	}
}

func TestDetectKnowledgeConflictsRequiresDifferentRecommendations(t *testing.T) {
	rows := []db.ListKnowledgeGovernanceCandidatesRow{
		{
			ID:                  pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
			Type:                "lesson",
			LifecycleStatus:     "published",
			ProblemPattern:      "Mobile push notifications disappear on Xiaomi devices after app idle",
			RecommendedPractice: "Check MIUI background restrictions first.",
		},
		{
			ID:                  pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
			Type:                "lesson",
			LifecycleStatus:     "published",
			ProblemPattern:      "Mobile push notifications disappear on Xiaomi devices after app idle",
			RecommendedPractice: "Use only vendor push overwrite IDs.",
		},
	}

	got := detectKnowledgeConflicts(rows)
	if len(got) != 2 {
		t.Fatalf("detectKnowledgeConflicts returned %d rows, want 2", len(got))
	}
}

func TestEligibleForTaskClaimKnowledgeSkipsConflictAndStaleReview(t *testing.T) {
	base := db.KnowledgeItem{
		ConfidenceStatus: "high",
		LifecycleStatus:  "published",
	}
	if !eligibleForTaskClaimKnowledge(base, "high") {
		t.Fatal("base item should be eligible")
	}
	conflict := base
	conflict.ConflictGroup = pgtype.Text{String: "conflict:test", Valid: true}
	if eligibleForTaskClaimKnowledge(conflict, "high") {
		t.Fatal("conflict item should not be eligible")
	}
	stale := base
	stale.ReviewNeededAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	stale.StaleScore = numericFromFloat(90)
	if eligibleForTaskClaimKnowledge(stale, "high") {
		t.Fatal("stale review-needed item should not be eligible")
	}
}
