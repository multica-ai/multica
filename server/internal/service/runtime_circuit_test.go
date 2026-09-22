package service

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestClassifyCircuitFailure(t *testing.T) {
	now := time.Date(2026, 8, 28, 11, 41, 49, 0, time.UTC)
	failedAt := now.Add(-2 * time.Minute)

	t.Run("parseable quota holds to reset plus grace", func(t *testing.T) {
		d := classifyCircuitFailure(
			string(taskfailure.ReasonAgentProviderQuotaLimit),
			"You've hit your session limit · resets 12:50pm (UTC)",
			failedAt, now,
		)
		if !d.Open || d.FailureClass != circuitClassQuota || d.ResetSource != circuitResetParseable {
			t.Fatalf("unexpected decision: %+v", d)
		}
		want := time.Date(2026, 8, 28, 12, 50, 0, 0, time.UTC).Add(circuitQuotaGrace)
		if !d.HoldUntil.Equal(want) {
			t.Fatalf("hold_until = %s, want %s", d.HoldUntil, want)
		}
	})

	t.Run("opaque quota holds one interval from failure", func(t *testing.T) {
		d := classifyCircuitFailure(
			string(taskfailure.ReasonAgentProviderQuotaLimit),
			"You've hit your limit", // no reset clause
			failedAt, now,
		)
		if !d.Open || d.ResetSource != circuitResetOpaque {
			t.Fatalf("unexpected decision: %+v", d)
		}
		if want := failedAt.Add(circuitOpaqueQuotaInterval); !d.HoldUntil.Equal(want) {
			t.Fatalf("hold_until = %s, want %s", d.HoldUntil, want)
		}
	})

	t.Run("auth holds the fixed window and never switches", func(t *testing.T) {
		d := classifyCircuitFailure(
			string(taskfailure.ReasonAgentProviderAuthOrAccess),
			"401 unauthorized",
			failedAt, now,
		)
		if !d.Open || d.FailureClass != circuitClassAuth || d.ResetSource != circuitResetAuth {
			t.Fatalf("unexpected decision: %+v", d)
		}
		if want := failedAt.Add(circuitAuthHoldWindow); !d.HoldUntil.Equal(want) {
			t.Fatalf("hold_until = %s, want %s", d.HoldUntil, want)
		}
	})

	t.Run("non provider classes open nothing", func(t *testing.T) {
		for _, reason := range []string{
			string(taskfailure.ReasonAgentProviderCapacityOrRateLimit),
			string(taskfailure.ReasonAgentProviderServerError),
			string(taskfailure.ReasonAgentProviderNetwork),
			string(taskfailure.ReasonAgentContextOverflow),
			string(taskfailure.ReasonAgentProcessFailure),
			string(taskfailure.ReasonTimeout),
			"",
		} {
			if d := classifyCircuitFailure(reason, "429 rate limit", failedAt, now); d.Open {
				t.Errorf("reason %q opened a circuit: %+v", reason, d)
			}
		}
	})

	t.Run("elapsed opaque quota hold opens nothing", func(t *testing.T) {
		old := now.Add(-2 * time.Hour)
		d := classifyCircuitFailure(
			string(taskfailure.ReasonAgentProviderQuotaLimit),
			"You've hit your limit",
			old, now,
		)
		if d.Open {
			t.Fatalf("expired hold should not open: %+v", d)
		}
	})
}

func TestCircuitHeld(t *testing.T) {
	cases := map[string]bool{
		"closed":    false,
		"open":      true,
		"half_open": true,
		"":          false,
	}
	for state, want := range cases {
		if got := circuitHeld(state); got != want {
			t.Errorf("circuitHeld(%q) = %v, want %v", state, got, want)
		}
	}
}

func TestCircuitHeldAt(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	future := now.Add(30 * time.Minute)
	past := now.Add(-30 * time.Minute)

	cases := []struct {
		name       string
		state      string
		resetAt    time.Time
		resetKnown bool
		want       bool
	}{
		{"closed never holds", "closed", future, true, false},
		{"empty state never holds", "", future, true, false},
		{"half_open always holds", "half_open", past, true, true},
		{"open holds until reset window elapses", "open", future, true, true},
		{"open with elapsed window is eligible again", "open", past, true, false},
		{"open at the exact reset instant is eligible", "open", now, true, false},
		{"open without a known reset holds", "open", time.Time{}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := circuitHeldAt(tc.state, tc.resetAt, tc.resetKnown, now); got != tc.want {
				t.Errorf("circuitHeldAt(%q, reset=%s, known=%v) = %v, want %v",
					tc.state, tc.resetAt, tc.resetKnown, got, tc.want)
			}
		})
	}
}

func uuidFrom(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[15] = b
	return u
}

func TestSelectFallbackRuntime(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	t.Run("empty pool", func(t *testing.T) {
		if got := selectFallbackRuntime(nil); got.Outcome != fallbackEmptyPool {
			t.Fatalf("outcome = %v, want empty pool", got.Outcome)
		}
	})

	t.Run("picks first ready unheld in priority order", func(t *testing.T) {
		got := selectFallbackRuntime([]runtimeCandidate{
			{RuntimeID: uuidFrom(1), Priority: 0, Available: true, Held: true, HoldUntil: now.Add(time.Hour)},
			{RuntimeID: uuidFrom(2), Priority: 1, Available: true, Held: false},
			{RuntimeID: uuidFrom(3), Priority: 2, Available: true, Held: false},
		})
		if got.Outcome != fallbackSelected {
			t.Fatalf("outcome = %v, want selected", got.Outcome)
		}
		if got.Chosen.RuntimeID != uuidFrom(2) {
			t.Fatalf("chose %x, want the priority-1 runtime", got.Chosen.RuntimeID.Bytes)
		}
	})

	t.Run("all held reports earliest known reset", func(t *testing.T) {
		got := selectFallbackRuntime([]runtimeCandidate{
			{RuntimeID: uuidFrom(1), Priority: 0, Available: true, Held: true, HoldUntil: now.Add(3 * time.Hour)},
			{RuntimeID: uuidFrom(2), Priority: 1, Available: true, Held: true, HoldUntil: now.Add(1 * time.Hour)},
		})
		if got.Outcome != fallbackAllHeld {
			t.Fatalf("outcome = %v, want all held", got.Outcome)
		}
		if got.HeldCount != 2 {
			t.Fatalf("held count = %d, want 2", got.HeldCount)
		}
		if !got.EarliestKnown || !got.EarliestReset.Equal(now.Add(time.Hour)) {
			t.Fatalf("earliest = %v known=%v, want %s", got.EarliestReset, got.EarliestKnown, now.Add(time.Hour))
		}
	})

	t.Run("all held with unknown deadline does not fabricate one", func(t *testing.T) {
		got := selectFallbackRuntime([]runtimeCandidate{
			{RuntimeID: uuidFrom(1), Priority: 0, Available: true, Held: true}, // zero HoldUntil
		})
		if got.Outcome != fallbackAllHeld {
			t.Fatalf("outcome = %v, want all held", got.Outcome)
		}
		if got.EarliestKnown {
			t.Fatalf("earliest should be unknown, got %s", got.EarliestReset)
		}
	})

	t.Run("held preferred candidate but a lower-priority one is ready", func(t *testing.T) {
		got := selectFallbackRuntime([]runtimeCandidate{
			{RuntimeID: uuidFrom(1), Priority: 0, Available: false, Held: false}, // offline
			{RuntimeID: uuidFrom(2), Priority: 1, Available: true, Held: true, HoldUntil: now.Add(time.Hour)},
			{RuntimeID: uuidFrom(3), Priority: 2, Available: true, Held: false},
		})
		if got.Outcome != fallbackSelected || got.Chosen.RuntimeID != uuidFrom(3) {
			t.Fatalf("unexpected: %+v", got)
		}
	})

	t.Run("none available and none held is a readiness skip", func(t *testing.T) {
		got := selectFallbackRuntime([]runtimeCandidate{
			{RuntimeID: uuidFrom(1), Priority: 0, Available: false, Held: false},
			{RuntimeID: uuidFrom(2), Priority: 1, Available: false, Held: false},
		})
		if got.Outcome != fallbackNoneAvailable {
			t.Fatalf("outcome = %v, want none available", got.Outcome)
		}
	})

	// F3: an auth/access hold on the PRIMARY never auto-switches to a fallback,
	// a quota hold does, and an auth hold on a non-primary binding is just a
	// skipped candidate (only the primary's credential must never be masked).
	t.Run("auth hold on primary skips with no fallback", func(t *testing.T) {
		got := selectFallbackRuntime([]runtimeCandidate{
			{RuntimeID: uuidFrom(1), Priority: 0, Available: true, Held: true, HeldClass: circuitClassAuth, HoldUntil: now.Add(4 * time.Hour)},
			{RuntimeID: uuidFrom(2), Priority: 1, Available: true, Held: false}, // healthy, but must NOT be chosen
		})
		if got.Outcome != fallbackAuthHeld {
			t.Fatalf("outcome = %v, want auth held (no fallback)", got.Outcome)
		}
		if !got.EarliestKnown || !got.EarliestReset.Equal(now.Add(4*time.Hour)) {
			t.Fatalf("earliest = %v known=%v, want %s", got.EarliestReset, got.EarliestKnown, now.Add(4*time.Hour))
		}
	})

	t.Run("quota hold on primary falls through to a ready binding", func(t *testing.T) {
		got := selectFallbackRuntime([]runtimeCandidate{
			{RuntimeID: uuidFrom(1), Priority: 0, Available: true, Held: true, HeldClass: circuitClassQuota, HoldUntil: now.Add(time.Hour)},
			{RuntimeID: uuidFrom(2), Priority: 1, Available: true, Held: false},
		})
		if got.Outcome != fallbackSelected || got.Chosen.RuntimeID != uuidFrom(2) {
			t.Fatalf("unexpected: %+v, want selected priority-1", got)
		}
	})

	t.Run("auth hold on a non-primary binding does not block a lower ready one", func(t *testing.T) {
		got := selectFallbackRuntime([]runtimeCandidate{
			{RuntimeID: uuidFrom(1), Priority: 0, Available: true, Held: true, HeldClass: circuitClassQuota, HoldUntil: now.Add(time.Hour)},
			{RuntimeID: uuidFrom(2), Priority: 1, Available: true, Held: true, HeldClass: circuitClassAuth, HoldUntil: now.Add(4 * time.Hour)},
			{RuntimeID: uuidFrom(3), Priority: 2, Available: true, Held: false},
		})
		if got.Outcome != fallbackSelected || got.Chosen.RuntimeID != uuidFrom(3) {
			t.Fatalf("unexpected: %+v, want selected priority-2", got)
		}
	})

	t.Run("auth hold on primary with unknown deadline does not fabricate one", func(t *testing.T) {
		got := selectFallbackRuntime([]runtimeCandidate{
			{RuntimeID: uuidFrom(1), Priority: 0, Available: true, Held: true, HeldClass: circuitClassAuth}, // zero HoldUntil
			{RuntimeID: uuidFrom(2), Priority: 1, Available: true, Held: false},
		})
		if got.Outcome != fallbackAuthHeld {
			t.Fatalf("outcome = %v, want auth held", got.Outcome)
		}
		if got.EarliestKnown {
			t.Fatalf("earliest should be unknown, got %s", got.EarliestReset)
		}
	})
}
