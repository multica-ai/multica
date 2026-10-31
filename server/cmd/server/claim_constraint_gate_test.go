package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// TestClaimConstraintPolicyGate covers the wiring branches of the router's
// double gate: the MULTICA_CLAIM_CONSTRAINT_POLICY env var must name a policy
// AND the claim_constraint_policy feature flag must be on before the canary
// policy is wired. Every other combination leaves TaskService.ClaimConstraints
// nil, so every claim path behaves exactly as before.
func TestClaimConstraintPolicyGate(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name       string
		env        string
		flagOn     bool
		wantCanary bool
	}{
		{name: "env unset leaves SPI unwired", env: "", flagOn: false, wantCanary: false},
		{name: "flag off leaves SPI unwired", env: "log", flagOn: false, wantCanary: false},
		{name: "env plus flag wires canary", env: "log", flagOn: true, wantCanary: true},
		{name: "unknown policy stays unwired", env: "nope", flagOn: true, wantCanary: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MULTICA_CLAIM_CONSTRAINT_POLICY", tt.env)
			provider := featureflag.NewStaticProvider()
			provider.Set(featureflags.ClaimConstraintPolicy, featureflag.Rule{Default: tt.flagOn})
			_, h := NewRouterWithOptions(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, RouterOptions{
				FeatureFlags: featureflag.NewService(provider),
			})
			if h.TaskService == nil {
				t.Fatal("router constructed without a TaskService")
			}
			_, wired := h.TaskService.ClaimConstraints.(*service.ClaimConstraintCanaryPolicy)
			if wired != tt.wantCanary {
				t.Fatalf("canary wired = %v, want %v", wired, tt.wantCanary)
			}
		})
	}
}
