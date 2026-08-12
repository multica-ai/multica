package service

import (
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// hookFlags builds a flag service for tests. Both Event Hooks switches are
// evaluated PER WORKSPACE, so a test that wants the engine to run must say so
// explicitly — a nil service falls through to the compiled default (off), which is
// what production ships with.
func hookFlags(hooks, execution bool) *featureflag.Service {
	p := featureflag.NewStaticProvider()
	p.Set(featureflags.EventHooks, featureflag.Rule{Default: hooks})
	p.Set(featureflags.EventHookExecution, featureflag.Rule{Default: execution})
	return featureflag.NewService(p)
}

// hookFlagsAllowingWorkspaces enables both switches ONLY for the given workspace
// ids, mirroring a real `allow_by: workspace_id` canary rollout.
func hookFlagsAllowingWorkspaces(workspaceIDs ...string) *featureflag.Service {
	p := featureflag.NewStaticProvider()
	rule := featureflag.Rule{Default: false, Allow: workspaceIDs, AllowBy: "workspace_id"}
	p.Set(featureflags.EventHooks, rule)
	p.Set(featureflags.EventHookExecution, rule)
	return featureflag.NewService(p)
}
