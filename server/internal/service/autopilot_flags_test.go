package service

import (
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// autopilotTaskDrivenFlags builds a feature-flag Service that forces the
// AutopilotTaskDrivenRuns gate on or off, for testing both rollout phases
// (MUL-4809 §4.1 P0-3).
func autopilotTaskDrivenFlags(enabled bool) *featureflag.Service {
	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.AutopilotTaskDrivenRuns, featureflag.Rule{Default: enabled})
	return featureflag.NewService(provider)
}
