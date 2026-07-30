package featureflags

import (
	"context"
	"testing"
)

func TestResourceLabelsReleaseFlagDefaultsToOff(t *testing.T) {
	ctx := context.Background()
	if ResourceLabelsEnabled(ctx, nil) {
		t.Fatal("resource labels release flag must default to off")
	}
}

// MUL-5345: the desktop client is fail-closed on this flag, so it stays off
// unless the key arrives as an explicit true. A key that is never published
// can therefore never be turned on — which is exactly what happened when the
// desktop side shipped without this entry: hang stack capture was inert in
// production and no amount of flag configuration could have enabled it.
func TestDesktopHangStackCaptureIsPublishedToClients(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if _, published := flags[DesktopHangStackCapture]; !published {
		t.Fatal("desktop hang stack capture flag must be published, or the client can never enable it")
	}
}

func TestDesktopHangStackCaptureDefaultsToOff(t *testing.T) {
	ctx := context.Background()
	if DesktopHangStackCaptureEnabled(ctx, nil) {
		t.Fatal("hang stack capture must default to off")
	}
	flags := EvaluateFrontendPublicFlags(ctx, nil)
	if flags[DesktopHangStackCapture] {
		t.Fatal("published default must be off until it is deliberately enabled")
	}
}

func TestAgentBuilderCompatDecisionStaysEnabled(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if !flags[agentBuilderCompat] {
		t.Fatal("agent builder must stay enabled for installed clients")
	}
}

func TestAgentSkillTogglesCompatDecisionStaysEnabled(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if !flags[agentSkillTogglesCompat] {
		t.Fatal("agent skill toggles must stay enabled for installed v0.4.0 clients")
	}
}
