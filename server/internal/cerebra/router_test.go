package cerebra

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestRouter_FilterBeforeSelect_And_Fallback(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	classifier := HeuristicClassifier{}
	policy := &Policy{}
	session := NewSessionStore(time.Hour)
	unavail := NewUnavailabilityStore(time.Hour)

	router := NewRouter(classifier, policy, session, unavail, logger, nil)

	runtimes := []RuntimeEntry{
		{
			RuntimeID: "rt-1",
			TierMap: TierMap{
				TierSimple:   "claude-haiku-3-5",
				TierStandard: "claude-sonnet-4-5",
				TierHeavy:    "claude-opus-4-5",
			},
		},
	}

	// 1. Simple task routes to simple model
	res := router.Route(ctx, "Hello world", TaskMeta{}, runtimes, "default-model")
	if res.Model != "claude-haiku-3-5" || res.Tier != TierSimple {
		t.Fatalf("expected haiku/simple, got model=%q, tier=%q", res.Model, res.Tier)
	}

	// 2. Mark simple model unavailable -> should escalate to standard
	unavail.MarkUnavailable(ctx, "rt-1", "claude-haiku-3-5", time.Hour)

	res2 := router.Route(ctx, "Hello world", TaskMeta{}, runtimes, "default-model")
	if res2.Model != "claude-sonnet-4-5" || res2.Tier != TierStandard {
		t.Fatalf("expected escalation to sonnet/standard, got model=%q, tier=%q", res2.Model, res2.Tier)
	}

	// 3. Mark standard also unavailable -> simple should escalate to heavy
	unavail.MarkUnavailable(ctx, "rt-1", "claude-sonnet-4-5", time.Hour)

	res3 := router.Route(ctx, "Hello world", TaskMeta{}, runtimes, "default-model")
	if res3.Model != "claude-opus-4-5" || res3.Tier != TierHeavy {
		t.Fatalf("expected escalation to opus/heavy, got model=%q, tier=%q", res3.Model, res3.Tier)
	}

	// 4. Mark all unavailable -> hard fallback to default-model
	unavail.MarkUnavailable(ctx, "rt-1", "claude-opus-4-5", time.Hour)

	res4 := router.Route(ctx, "Hello world", TaskMeta{}, runtimes, "default-model")
	if res4.Model != "default-model" || !res4.FallbackUsed {
		t.Fatalf("expected hard fallback to default-model, got model=%q, fallback=%v", res4.Model, res4.FallbackUsed)
	}
}

func TestRouter_StandardAndHeavyFallbacks(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	classifier := HeuristicClassifier{}
	policy := &Policy{}

	// Test Heavy task falling back to Standard when Heavy is unavailable
	t.Run("heavy task falls back to standard", func(t *testing.T) {
		session := NewSessionStore(time.Hour)
		unavail := NewUnavailabilityStore(time.Hour)
		router := NewRouter(classifier, policy, session, unavail, logger, nil)

		runtimes := []RuntimeEntry{
			{
				RuntimeID: "rt-1",
				TierMap: TierMap{
					TierSimple:   "claude-haiku-3-5",
					TierStandard: "claude-sonnet-4-5",
					TierHeavy:    "claude-opus-4-5",
				},
			},
		}

		// Mark heavy unavailable
		unavail.MarkUnavailable(ctx, "rt-1", "claude-opus-4-5", time.Hour)

		res := router.Route(ctx, "architect and design distributed cache", TaskMeta{}, runtimes, "default-model")
		if res.Model != "claude-sonnet-4-5" || res.Tier != TierStandard {
			t.Fatalf("expected heavy to fallback to standard (sonnet), got %q, tier=%q", res.Model, res.Tier)
		}
	})

	// Test Standard task escalating to Heavy when Standard is unavailable
	t.Run("standard task escalates to heavy", func(t *testing.T) {
		session := NewSessionStore(time.Hour)
		unavail := NewUnavailabilityStore(time.Hour)
		router := NewRouter(classifier, policy, session, unavail, logger, nil)

		runtimes := []RuntimeEntry{
			{
				RuntimeID: "rt-1",
				TierMap: TierMap{
					TierSimple:   "claude-haiku-3-5",
					TierStandard: "claude-sonnet-4-5",
					TierHeavy:    "claude-opus-4-5",
				},
			},
		}

		// Mark standard unavailable
		unavail.MarkUnavailable(ctx, "rt-1", "claude-sonnet-4-5", time.Hour)

		res := router.Route(ctx, "debug this issue", TaskMeta{}, runtimes, "default-model")
		if res.Model != "claude-opus-4-5" || res.Tier != TierHeavy {
			t.Fatalf("expected standard to escalate to heavy (opus), got %q, tier=%q", res.Model, res.Tier)
		}
	})

	// Test Standard task with MCP tool floor does NOT fallback to Simple
	t.Run("standard tool task does not fallback to simple", func(t *testing.T) {
		session := NewSessionStore(time.Hour)
		unavail := NewUnavailabilityStore(time.Hour)
		router := NewRouter(classifier, policy, session, unavail, logger, nil)

		runtimes := []RuntimeEntry{
			{
				RuntimeID: "rt-1",
				TierMap: TierMap{
					TierSimple:   "claude-haiku-3-5",
					TierStandard: "claude-sonnet-4-5",
					TierHeavy:    "claude-opus-4-5",
				},
			},
		}

		// Mark standard and heavy unavailable
		unavail.MarkUnavailable(ctx, "rt-1", "claude-sonnet-4-5", time.Hour)
		unavail.MarkUnavailable(ctx, "rt-1", "claude-opus-4-5", time.Hour)

		meta := TaskMeta{WillUseMCPTools: true}
		res := router.Route(ctx, "debug this issue", meta, runtimes, "default-model")
		if res.Model != "default-model" || !res.FallbackUsed {
			t.Fatalf("expected hard fallback to default-model because simple is forbidden for tools, got %q", res.Model)
		}
	})
}

func TestRouter_SessionAffinity(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	classifier := HeuristicClassifier{}
	policy := &Policy{}
	session := NewSessionStore(time.Hour)
	unavail := NewUnavailabilityStore(time.Hour)

	router := NewRouter(classifier, policy, session, unavail, logger, nil)

	runtimes := []RuntimeEntry{
		{
			RuntimeID: "rt-1",
			TierMap: TierMap{
				TierStandard: "claude-sonnet-4-5",
			},
		},
	}

	meta := TaskMeta{IssueID: "issue-123"}
	res1 := router.Route(ctx, "debug this issue", meta, runtimes, "default-model")
	if res1.Model != "claude-sonnet-4-5" {
		t.Fatalf("expected sonnet, got %q", res1.Model)
	}

	// Next turn with same tier should hit session pin
	res2 := router.Route(ctx, "fix the failing test", meta, runtimes, "default-model")
	if res2.Model != "claude-sonnet-4-5" {
		t.Fatalf("expected pinned sonnet, got %q", res2.Model)
	}
}
