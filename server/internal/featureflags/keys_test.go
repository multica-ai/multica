package featureflags

import (
	"context"
	"os"
	"testing"

	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// project_plans is published off by default. The route gate and the frontend
// publication resolve their default at two separate call sites, so this walks
// all three environment states and holds the two together — a route must
// never serve a surface /api/config reports as off, or vice versa.
func TestProjectPlansEnvironmentStatesKeepBackendAndFrontendAligned(t *testing.T) {
	t.Setenv(featureflag.EnvFlagFile, "")
	falseValue := "false"
	trueValue := "true"
	tests := []struct {
		name     string
		envValue *string
		want     bool
	}{
		{name: "unset defaults off", want: false},
		{name: "false stays off", envValue: &falseValue, want: false},
		{name: "true opts in", envValue: &trueValue, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setOptionalEnv(t, "FF_PROJECT_PLANS", test.envValue)
			flags, err := featureflag.NewServiceFromEnv()
			if err != nil {
				t.Fatalf("build feature flag service: %v", err)
			}

			backendEnabled := ProjectPlansEnabled(context.Background(), flags)
			if backendEnabled != test.want {
				t.Fatalf("backend project_plans = %t, want %t", backendEnabled, test.want)
			}
			frontendEnabled, published := EvaluateFrontendPublicFlags(context.Background(), flags)[ProjectPlans]
			if !published {
				t.Fatal("project_plans must be published for the frontend")
			}
			if frontendEnabled != backendEnabled {
				t.Fatalf("frontend project_plans = %t, backend = %t", frontendEnabled, backendEnabled)
			}
		})
	}
}

// Every published flag fails closed when no provider supplies a decision:
// project_plans is explicitly listed in flagDefaults as off, a key absent
// from the map keeps the old fail-closed behaviour, and the non-plan gates
// stay disabled by default.
func TestPublishedFlagsDefaultOff(t *testing.T) {
	published := EvaluateFrontendPublicFlags(context.Background(), nil)
	for _, key := range frontendPublicFlags {
		if published[key] {
			t.Fatalf("published default for %q = true, want false", key)
		}
		if got := defaultFor(key); got {
			t.Fatalf("defaultFor(%q) = true, want false", key)
		}
	}
	if BillingWorkspaceSubscriptionsEnabled(context.Background(), nil) {
		t.Fatal("billing_workspace_subscriptions must stay disabled by default")
	}
	if ComposioMCPAppsEnabled(context.Background(), nil) {
		t.Fatal("composio_mcp_apps must stay disabled by default")
	}
	if PluginsV1Enabled(context.Background(), nil) {
		t.Fatal("plugins_v1 must stay disabled by default")
	}
	if len(flagDefaults) != 1 {
		t.Fatalf("flagDefaults must list project_plans only, got %v", flagDefaults)
	}
	if defaultFor("some_flag_nobody_registered") {
		t.Fatal("a key absent from flagDefaults must default to off")
	}
}

func setOptionalEnv(t *testing.T, key string, value *string) {
	t.Helper()
	previous, present := os.LookupEnv(key)
	t.Cleanup(func() {
		var err error
		if present {
			err = os.Setenv(key, previous)
		} else {
			err = os.Unsetenv(key)
		}
		if err != nil {
			t.Errorf("restore %s: %v", key, err)
		}
	})

	var err error
	if value == nil {
		err = os.Unsetenv(key)
	} else {
		err = os.Setenv(key, *value)
	}
	if err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}
