package rounds

import (
	"testing"
	"time"
)

func TestNextRunAtUsesRoundTimezone(t *testing.T) {
	now := time.Date(2026, 7, 10, 5, 30, 0, 0, time.UTC)
	got, err := nextRunAt("0 8 * * *", "Europe/Copenhagen", now)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("next run = %s, want %s", got, want)
	}
}

func TestTaskProgressClassifiesStalledOnlyAfterTimeout(t *testing.T) {
	now := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
	started := now.Add(-31 * time.Minute)
	if got := taskProgress("running", &started, now, 30*time.Minute); got != "stalled" {
		t.Fatalf("taskProgress = %q, want stalled", got)
	}
	recent := now.Add(-29 * time.Minute)
	if got := taskProgress("running", &recent, now, 30*time.Minute); got != "running" {
		t.Fatalf("taskProgress = %q, want running", got)
	}
}

func TestHeldTargetsPreserveAgentAndSquadMentions(t *testing.T) {
	targets := heldTargets("[@A](mention://agent/00000000-0000-0000-0000-000000000001) [@S](mention://squad/00000000-0000-0000-0000-000000000002)")
	if len(targets) != 2 || targets[0].kind != "agent" || targets[1].kind != "squad" {
		t.Fatalf("heldTargets = %#v", targets)
	}
}

func TestNextRunAtRejectsInvalidTimezone(t *testing.T) {
	if _, err := nextRunAt("0 8 * * *", "Mars/Olympus", time.Now()); err == nil {
		t.Fatal("expected invalid timezone error")
	}
}

func TestRunStatus(t *testing.T) {
	tests := []struct {
		total, responded, failed int
		want                     string
	}{
		{3, 1, 0, RunRunning},
		{3, 3, 0, RunReady},
		{3, 2, 1, RunReady},
		{0, 0, 0, RunReady},
	}
	for _, tt := range tests {
		if got := runStatus(tt.total, tt.responded, tt.failed); got != tt.want {
			t.Fatalf("runStatus(%d,%d,%d)=%q, want %q", tt.total, tt.responded, tt.failed, got, tt.want)
		}
	}
}

func TestNormalizeModeAcceptsOnlyLiveOrBatch(t *testing.T) {
	for _, tt := range []struct{ in, want string }{{"live", "live"}, {"batch", "batch"}, {"", "batch"}} {
		got, err := normalizeMode(tt.in)
		if err != nil || got != tt.want {
			t.Fatalf("normalizeMode(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	if _, err := normalizeMode("automatic"); err == nil {
		t.Fatal("expected unsupported mode to fail")
	}
}

func TestShouldHoldCommentOnlyForBatchRounds(t *testing.T) {
	if shouldHoldComment("live") {
		t.Fatal("live rounds must dispatch comments immediately")
	}
	if !shouldHoldComment("batch") {
		t.Fatal("batch rounds must hold comments until Run")
	}
}
