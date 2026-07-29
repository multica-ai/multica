package daemon

// FIR-4013 — a task with no Session Mode must carry no Mode profile.
//
// Before this, effectiveSessionModeProfile fell through to ProfileFor(""),
// which ProfileFor maps to the Build defaults. Every run that never had a Mode
// recorded — issue assignment, wakeup, autopilot, promoted sub-issue, a mention
// in a thread nobody labelled — silently inherited Build's 80-turn cap and
// 120-minute timeout. Production evidence: 54 of the 100 most recent Claude
// spawns carried `--max-turns 80` while the Session modes feature was off, and
// the workspace's own published Build profile (199 turns) reached only the 8
// runs that did have a Mode.

import (
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/sessionmode"
)

func TestEffectiveSessionModeProfileEmptyWithoutMode(t *testing.T) {
	for _, name := range []string{"", "   ", "not-a-mode"} {
		profile := effectiveSessionModeProfile(Task{SessionMode: name})
		if profile.MaxTurns != 0 {
			t.Errorf("SessionMode %q: MaxTurns = %d, want 0 (no turn cap)", name, profile.MaxTurns)
		}
		if profile.Timeout != 0 {
			t.Errorf("SessionMode %q: Timeout = %s, want 0 (runtime decides)", name, profile.Timeout)
		}
		if profile.Mode != "" || profile.Instruction != "" {
			t.Errorf("SessionMode %q: profile = %+v, want zero value", name, profile)
		}
	}
}

// A pinned snapshot must not leak in through the no-Mode path either: without a
// Mode there is nothing to apply it to.
func TestEffectiveSessionModeProfileIgnoresConfigWithoutMode(t *testing.T) {
	config := sessionmode.Config{
		Mode: sessionmode.Build, Version: "2", Instruction: "Published Build.",
		ThinkingLevel: "high", TimeoutMinutes: 120, MaxTurns: 199, ApprovalPolicy: "inherit",
	}
	if profile := effectiveSessionModeProfile(Task{SessionModeConfig: &config}); profile.MaxTurns != 0 {
		t.Fatalf("MaxTurns = %d, want 0 — a snapshot without a Mode must not apply", profile.MaxTurns)
	}
}

// Regression guard: a run that DID pick a Mode keeps its profile, including the
// workspace's published turn cap.
func TestEffectiveSessionModeProfileKeepsSelectedMode(t *testing.T) {
	if profile := effectiveSessionModeProfile(Task{SessionMode: "build"}); profile.MaxTurns != 80 {
		t.Errorf("build MaxTurns = %d, want 80 (built-in default)", profile.MaxTurns)
	}
	config := sessionmode.Config{
		Mode: sessionmode.Build, Version: "2", Instruction: "Published Build.",
		ThinkingLevel: "high", TimeoutMinutes: 120, MaxTurns: 199, ApprovalPolicy: "inherit",
	}
	profile := effectiveSessionModeProfile(Task{SessionMode: "build", SessionModeConfig: &config})
	if profile.MaxTurns != 199 || profile.Timeout != 120*time.Minute {
		t.Errorf("published profile = %+v, want MaxTurns 199 and 120m", profile)
	}
}

// PlanMode is the old-server compatibility path: it still resolves to a Mode,
// so it must keep its cap rather than falling into the no-profile branch.
func TestEffectiveSessionModeProfilePlanModeStillCaps(t *testing.T) {
	if profile := effectiveSessionModeProfile(Task{PlanMode: true}); profile.MaxTurns != 20 {
		t.Fatalf("plan MaxTurns = %d, want 20", profile.MaxTurns)
	}
}
