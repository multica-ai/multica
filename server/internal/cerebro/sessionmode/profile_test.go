package sessionmode

import (
	"strings"
	"testing"
	"time"
)

func TestProfilesCoverEveryMode(t *testing.T) {
	tests := []struct {
		mode        Mode
		thinking    string
		timeout     time.Duration
		maxTurns    int
		allowsWrite bool
		promptPart  string
	}{
		{Plan, "medium", 30 * time.Minute, 20, false, "must NOT write or edit code"},
		{Build, "high", 120 * time.Minute, 80, true, "implement the requested outcome"},
		{Research, "high", 60 * time.Minute, 40, false, "read-only investigation"},
		{Review, "high", 45 * time.Minute, 30, false, "must NOT edit the reviewed code"},
	}
	for _, tc := range tests {
		profile := ProfileFor(tc.mode)
		if profile.Mode != tc.mode || profile.ThinkingLevel != tc.thinking || profile.Timeout != tc.timeout || profile.MaxTurns != tc.maxTurns || profile.AllowsWrite != tc.allowsWrite {
			t.Fatalf("profile %q = %+v", tc.mode, profile)
		}
		if !strings.Contains(strings.ToLower(profile.Instruction), strings.ToLower(tc.promptPart)) {
			t.Fatalf("profile %q instruction missing %q: %s", tc.mode, tc.promptPart, profile.Instruction)
		}
	}
}

func TestNormalizeAcceptsLegacyDefaultAndRejectsUnknown(t *testing.T) {
	if got, ok := Normalize("default"); !ok || got != Build {
		t.Fatalf("legacy default = %q, %v; want build, true", got, ok)
	}
	if got, ok := Normalize("auto"); !ok || got != Build {
		t.Fatalf("legacy auto = %q, %v; want build, true", got, ok)
	}
	if got, ok := Normalize(" REVIEW "); !ok || got != Review {
		t.Fatalf("review = %q, %v; want review, true", got, ok)
	}
	if _, ok := Normalize("ship"); ok {
		t.Fatal("unknown mode accepted")
	}
}

func TestEffectiveTimeoutUsesShorterPositiveLimit(t *testing.T) {
	if got := EffectiveTimeout(2*time.Hour, 30*time.Minute); got != 30*time.Minute {
		t.Fatalf("got %s, want 30m", got)
	}
	if got := EffectiveTimeout(10*time.Minute, 30*time.Minute); got != 10*time.Minute {
		t.Fatalf("got %s, want 10m", got)
	}
	if got := EffectiveTimeout(0, 30*time.Minute); got != 30*time.Minute {
		t.Fatalf("got %s, want 30m", got)
	}
	if got := EffectiveTimeout(2*time.Hour, 0); got != 2*time.Hour {
		t.Fatalf("got %s, want 2h", got)
	}
}
