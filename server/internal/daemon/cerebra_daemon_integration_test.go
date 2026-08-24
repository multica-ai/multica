package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebra"
)

func TestDetectMCPUsage(t *testing.T) {
	tests := []struct {
		name          string
		overlay       []byte
		connectedApps []string
		want          bool
	}{
		{
			name:          "empty overlay and empty apps",
			overlay:       nil,
			connectedApps: nil,
			want:          false,
		},
		{
			name:          "short overlay bytes {}",
			overlay:       []byte("{}"),
			connectedApps: nil,
			want:          false,
		},
		{
			name:          "valid overlay json",
			overlay:       []byte(`{"servers":{"github":{}}}`),
			connectedApps: nil,
			want:          true,
		},
		{
			name:          "connected app present",
			overlay:       nil,
			connectedApps: []string{"github", "slack"},
			want:          true,
		},
		{
			name:          "connected apps with only whitespace",
			overlay:       nil,
			connectedApps: []string{"   ", ""},
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectMCPUsage(tt.overlay, tt.connectedApps)
			if got != tt.want {
				t.Errorf("detectMCPUsage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRouteBeforeDispatch(t *testing.T) {
	ctx := context.Background()

	t.Run("nil router returns default model", func(t *testing.T) {
		got := routeBeforeDispatch(ctx, nil, "prompt", cerebra.TaskMeta{}, nil, "claude-3-5-sonnet")
		if got != "claude-3-5-sonnet" {
			t.Errorf("routeBeforeDispatch() = %v, want %v", got, "claude-3-5-sonnet")
		}
	})

	t.Run("active router routes based on prompt", func(t *testing.T) {
		classifier := cerebra.HeuristicClassifier{}
		policy := &cerebra.Policy{}
		session := cerebra.NewSessionStore(time.Hour)
		unavail := cerebra.NewUnavailabilityStore(time.Hour)
		router := cerebra.NewRouter(classifier, policy, session, unavail, nil, nil)

		runtimes := []cerebra.RuntimeEntry{
			{
				RuntimeID: "rt-1",
				TierMap: cerebra.TierMap{
					cerebra.TierSimple:   "claude-3-5-haiku",
					cerebra.TierStandard: "claude-3-5-sonnet",
					cerebra.TierHeavy:    "claude-3-opus",
				},
			},
		}

		// Simple prompt
		gotSimple := routeBeforeDispatch(ctx, router, "hello world", cerebra.TaskMeta{}, runtimes, "default-model")
		if gotSimple != "claude-3-5-haiku" {
			t.Errorf("got %v, want claude-3-5-haiku", gotSimple)
		}

		// Heavy prompt
		gotHeavy := routeBeforeDispatch(ctx, router, "architect and design system", cerebra.TaskMeta{}, runtimes, "default-model")
		if gotHeavy != "claude-3-opus" {
			t.Errorf("got %v, want claude-3-opus", gotHeavy)
		}
	})
}
