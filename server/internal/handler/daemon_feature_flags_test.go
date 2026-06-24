package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflagdispatch"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func TestProcessHeartbeatIncludesDaemonFeatureFlagSnapshot(t *testing.T) {
	t.Parallel()

	provider := featureflag.NewStaticProvider()
	provider.Set(featureflagdispatch.RuntimeBriefSlimFlag, featureflag.Rule{Default: true})
	h := &Handler{
		UpdateStore:           NewInMemoryUpdateStore(),
		ModelListStore:        NewInMemoryModelListStore(),
		LocalSkillListStore:   NewInMemoryLocalSkillListStore(),
		LocalSkillImportStore: NewInMemoryLocalSkillImportStore(),
		LivenessStore:         NewNoopLivenessStore(),
		HeartbeatScheduler:    noopHeartbeatScheduler{},
		DaemonFeatureFlags:    featureflagdispatch.NewEvaluator(featureflag.NewService(provider)),
	}
	rt := db.AgentRuntime{
		ID:          util.MustParseUUID("30000000-0000-0000-0000-000000000001"),
		WorkspaceID: util.MustParseUUID("30000000-0000-0000-0000-000000000002"),
	}

	ack, _, err := h.processHeartbeat(context.Background(), rt, false)
	if err != nil {
		t.Fatalf("processHeartbeat: %v", err)
	}
	if ack.FeatureFlags == nil {
		t.Fatal("FeatureFlags snapshot is nil")
	}
	if got := ack.FeatureFlags.Flags[featureflagdispatch.RuntimeBriefSlimFlag]; got != "on" {
		t.Fatalf("runtime_brief_slim = %q, want on", got)
	}
}

type noopHeartbeatScheduler struct{}

func (noopHeartbeatScheduler) Schedule(context.Context, db.AgentRuntime) error {
	return nil
}
