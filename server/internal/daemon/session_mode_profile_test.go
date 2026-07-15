package daemon

// CEREBRO-PATCH(session-mode-config-runtime-test): FIR-3111 covers versioned Mode snapshot resolution.

import (
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/sessionmode"
)

func TestEffectiveSessionModeProfileUsesClaimSnapshot(t *testing.T) {
	config := sessionmode.Config{
		Mode: sessionmode.Plan, Version: "9", Instruction: "Configured Plan.", Model: "gpt-5.4",
		ThinkingLevel: "high", TimeoutMinutes: 18, MaxTurns: 11, ApprovalPolicy: "inherit",
	}
	profile := effectiveSessionModeProfile(Task{SessionMode: "plan", SessionModeConfig: &config})
	if profile.Version != "9" || profile.Instruction != "Configured Plan." || profile.Timeout != 18*time.Minute || profile.MaxTurns != 11 {
		t.Fatalf("profile = %+v", profile)
	}
}

func TestEffectiveSessionModeProfileFallsBackForInvalidSnapshot(t *testing.T) {
	config := sessionmode.Config{Mode: sessionmode.Plan, Instruction: "", TimeoutMinutes: 0}
	profile := effectiveSessionModeProfile(Task{SessionMode: "plan", SessionModeConfig: &config})
	if profile.Version != "1" || profile.Timeout != 30*time.Minute {
		t.Fatalf("fallback profile = %+v", profile)
	}
}
