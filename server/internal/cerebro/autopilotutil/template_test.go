package autopilotutil

import (
	"testing"
	"time"
)

func TestInterpolateTitleTemplateSupportsFirtalTokens(t *testing.T) {
	now := time.Date(2026, 5, 10, 7, 30, 0, 0, time.UTC)

	got := InterpolateTitleTemplate("Issue-recovery {{.Date}} / {{date}} / {{.ISOWeek}} / {{isoWeek}}", now)
	want := "Issue-recovery 2026-05-10 / 2026-05-10 / 2026-W19 / 2026-W19"

	if got != want {
		t.Fatalf("InterpolateTitleTemplate() = %q, want %q", got, want)
	}
}
