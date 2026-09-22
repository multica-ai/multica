package service

import (
	"strings"
	"testing"
)

// TestFallbackRouteMarker pins the specific-route contract (F6: name the route,
// not a generic "via fallback runtime"). The marker must name the target
// provider and distinguish a plain failover from a half-open probe, and an
// ordinary dispatch onto the default runtime must produce no marker.
func TestFallbackRouteMarker(t *testing.T) {
	t.Run("no failover, no probe -> empty marker", func(t *testing.T) {
		if got := fallbackRouteMarker(false, runtimeCandidate{Provider: "claude"}); got != "" {
			t.Fatalf("got %q, want empty marker", got)
		}
	})

	t.Run("failover names the target provider", func(t *testing.T) {
		got := fallbackRouteMarker(true, runtimeCandidate{Provider: "codex"})
		if got != " · failover→codex" {
			t.Fatalf("got %q, want a provider-specific failover marker", got)
		}
	})

	t.Run("probe window is labelled as a half-open probe", func(t *testing.T) {
		got := fallbackRouteMarker(true, runtimeCandidate{Provider: "claude", ProbeWindow: true})
		if got != " · half-open probe→claude" {
			t.Fatalf("got %q, want a half-open probe marker", got)
		}
	})
}

// TestRunOnlyTriggerSummary pins the visible-failover contract (SE-37711 /
// SE-37664, parent §5: never silent). A run_only dispatch routed off the default
// runtime must carry its specific route marker in the task snapshot, the marker
// must survive title truncation, and a run that did not move must look exactly
// like before.
func TestRunOnlyTriggerSummary(t *testing.T) {
	const marker = " · failover→claude"
	markerRunes := len([]rune(marker))

	t.Run("no marker leaves the snapshot untouched", func(t *testing.T) {
		if got := runOnlyTriggerSummary("Nightly triage", ""); got != "Nightly triage" {
			t.Fatalf("got %q, want the plain title", got)
		}
	})

	t.Run("no marker still truncates an over-long title", func(t *testing.T) {
		long := strings.Repeat("x", triggerSummaryMaxLen+50)
		got := runOnlyTriggerSummary(long, "")
		if r := []rune(got); len(r) != triggerSummaryMaxLen+1 || !strings.HasSuffix(got, "…") {
			t.Fatalf("got %d runes (suffix …=%v), want %d + ellipsis", len(r), strings.HasSuffix(got, "…"), triggerSummaryMaxLen)
		}
	})

	t.Run("marker is appended to the title", func(t *testing.T) {
		got := runOnlyTriggerSummary("Nightly triage", marker)
		if got != "Nightly triage"+marker {
			t.Fatalf("got %q, want title + marker", got)
		}
	})

	t.Run("marker survives on an empty title", func(t *testing.T) {
		if got := runOnlyTriggerSummary("", marker); got != marker {
			t.Fatalf("got %q, want the bare marker", got)
		}
	})

	t.Run("the whole marker stays within the length budget", func(t *testing.T) {
		long := strings.Repeat("у", triggerSummaryMaxLen+50) // multibyte to prove rune budgeting
		got := runOnlyTriggerSummary(long, marker)
		if r := []rune(got); len(r) > triggerSummaryMaxLen {
			t.Fatalf("summary is %d runes, want <= %d", len(r), triggerSummaryMaxLen)
		}
		if !strings.HasSuffix(got, marker) {
			t.Fatalf("marker was truncated away: %q", got)
		}
		// The title portion is truncated with an ellipsis and the full marker
		// is preserved: budget = max - markerRunes, title fills the budget.
		wantTitleRunes := triggerSummaryMaxLen - markerRunes
		title := strings.TrimSuffix(got, marker)
		if r := []rune(title); len(r) != wantTitleRunes || !strings.HasSuffix(title, "…") {
			t.Fatalf("title portion = %d runes (ellipsis=%v), want %d + ellipsis", len(r), strings.HasSuffix(title, "…"), wantTitleRunes)
		}
	})
}
