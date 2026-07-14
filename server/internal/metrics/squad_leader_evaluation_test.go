package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNormalizeSquadLeaderEvaluationOutcome(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"action", "action"},
		{"no_action", "no_action"},
		{"failed", "failed"},
		{"", "other"},
		{"ACTION", "action"}, // normalizer lower-cases before matching the allow-list
		{"delegated", "other"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := NormalizeSquadLeaderEvaluationOutcome(c.in); got != c.want {
				t.Fatalf("NormalizeSquadLeaderEvaluationOutcome(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestRecordSquadLeaderEvaluation(t *testing.T) {
	m := NewBusinessMetrics()

	m.RecordSquadLeaderEvaluation("action")
	m.RecordSquadLeaderEvaluation("action")
	m.RecordSquadLeaderEvaluation("no_action")
	m.RecordSquadLeaderEvaluation("failed")
	m.RecordSquadLeaderEvaluation("totally-bogus")

	cv := m.events.squadLeaderEvaluation
	if got := testutil.ToFloat64(cv.WithLabelValues("action")); got != 2 {
		t.Errorf("counter for outcome=action = %v, want 2", got)
	}
	if got := testutil.ToFloat64(cv.WithLabelValues("no_action")); got != 1 {
		t.Errorf("counter for outcome=no_action = %v, want 1", got)
	}
	if got := testutil.ToFloat64(cv.WithLabelValues("failed")); got != 1 {
		t.Errorf("counter for outcome=failed = %v, want 1", got)
	}
	if got := testutil.ToFloat64(cv.WithLabelValues("other")); got != 1 {
		t.Errorf("counter for outcome=other (bogus collapsed) = %v, want 1", got)
	}
}

func TestRecordSquadLeaderEvaluationNilSafe(t *testing.T) {
	// Mirrors the nil-safety contract of the other Record* helpers — calling
	// on a nil receiver must not panic so that callers can skip wiring metrics
	// in tests / minimal CLI builds without guarding every call site.
	var m *BusinessMetrics
	m.RecordSquadLeaderEvaluation("action")

	m2 := &BusinessMetrics{}
	m2.RecordSquadLeaderEvaluation("action")
}
