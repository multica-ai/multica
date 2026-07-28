package featureflags

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agentroute"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func TestResourceLabelsReleaseFlagDefaultsToOff(t *testing.T) {
	ctx := context.Background()
	if ResourceLabelsEnabled(ctx, nil) {
		t.Fatal("resource labels release flag must default to off")
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

func TestAdaptiveAgentRoutingModeIsTwoGateShadowFirst(t *testing.T) {
	ctx := context.Background()
	if got := AdaptiveAgentRoutingMode(ctx, nil); got != agentroute.ModeOff {
		t.Fatalf("nil flags mode = %q, want off", got)
	}

	static := featureflag.NewStaticProvider()
	static.Set(AdaptiveAgentRoutingActive, featureflag.Rule{Default: true})
	flags := featureflag.NewService(static)
	if got := AdaptiveAgentRoutingMode(ctx, flags); got != agentroute.ModeOff {
		t.Fatalf("active-only mode = %q, want off", got)
	}

	static.Set(AdaptiveAgentRouting, featureflag.Rule{Default: true})
	static.Set(AdaptiveAgentRoutingActive, featureflag.Rule{Default: false})
	if got := AdaptiveAgentRoutingMode(ctx, flags); got != agentroute.ModeShadow {
		t.Fatalf("base-only mode = %q, want shadow", got)
	}

	static.Set(AdaptiveAgentRoutingActive, featureflag.Rule{Default: true})
	if got := AdaptiveAgentRoutingMode(ctx, flags); got != agentroute.ModeActive {
		t.Fatalf("two-gate mode = %q, want active", got)
	}
}
