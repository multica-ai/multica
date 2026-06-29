package execenv

import (
	"context"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflagdispatch"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestServerSnapshotProviderLookupAndCopy(t *testing.T) {
	t.Parallel()

	provider := NewServerSnapshotProvider()
	flags := map[string]string{runtimeBriefSlimFlag: "on"}
	provider.Apply(ServerSnapshot{Version: 7, Flags: flags})
	flags[runtimeBriefSlimFlag] = "off"

	decision, ok := provider.Lookup(context.Background(), runtimeBriefSlimFlag)
	if !ok {
		t.Fatal("Lookup did not find runtime_brief_slim")
	}
	if !decision.Enabled || decision.Variant != "on" || decision.Source != "server_snapshot" {
		t.Fatalf("decision = %+v, want enabled on from server_snapshot", decision)
	}

	snapshot, ok := provider.Snapshot()
	if !ok {
		t.Fatal("Snapshot not found")
	}
	snapshot.Flags[runtimeBriefSlimFlag] = "off"
	decision, _ = provider.Lookup(context.Background(), runtimeBriefSlimFlag)
	if decision.Variant != "on" {
		t.Fatalf("mutating Snapshot copy changed provider decision: %+v", decision)
	}
}

func TestServerSnapshotProviderConcurrentSwapAndLookup(t *testing.T) {
	t.Parallel()

	provider := NewServerSnapshotProvider()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			provider.Apply(ServerSnapshot{
				Version: uint64(i + 1),
				Flags:   map[string]string{runtimeBriefSlimFlag: boolVariant(i%2 == 0)},
			})
		}()
	}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = provider.Lookup(context.Background(), runtimeBriefSlimFlag)
		}()
	}
	wg.Wait()

	if _, ok := provider.Snapshot(); !ok {
		t.Fatal("provider lost snapshot after concurrent swaps")
	}
}

func TestDaemonFeatureFlagProviderPrecedence(t *testing.T) {
	savedFlags := runtimeFlags.Load()
	savedProvider := activeServerSnapshotProvider.Load()
	t.Cleanup(func() {
		runtimeFlags.Store(savedFlags)
		activeServerSnapshotProvider.Store(savedProvider)
	})
	t.Setenv("FF_RUNTIME_BRIEF_SLIM", "false")

	serverSnapshot := NewServerSnapshotProvider()
	serverSnapshot.Apply(ServerSnapshot{
		Version: 1,
		Flags:   map[string]string{runtimeBriefSlimFlag: "on"},
	})
	static := featureflag.NewStaticProvider()
	static.Set(runtimeBriefSlimFlag, featureflag.Rule{Default: true})
	SetServerSnapshotProvider(serverSnapshot)
	SetFeatureFlags(featureflag.NewService(featureflag.NewChainProvider(
		featureflag.NewEnvProvider(featureflag.EnvOverridePrefix),
		serverSnapshot,
		static,
	)))

	if useSlimBrief() {
		t.Fatal("FF_RUNTIME_BRIEF_SLIM=false must override server snapshot and static defaults")
	}
}

func TestHeartbeatSnapshotUpdatesUseSlimBrief(t *testing.T) {
	savedFlags := runtimeFlags.Load()
	savedProvider := activeServerSnapshotProvider.Load()
	t.Cleanup(func() {
		runtimeFlags.Store(savedFlags)
		activeServerSnapshotProvider.Store(savedProvider)
	})

	serverSnapshot := NewServerSnapshotProvider()
	SetServerSnapshotProvider(serverSnapshot)
	SetFeatureFlags(featureflag.NewService(featureflag.NewChainProvider(serverSnapshot)))

	serverProvider := featureflag.NewStaticProvider()
	serverService := featureflag.NewService(serverProvider)
	evaluator := featureflagdispatch.NewEvaluator(serverService)
	rt := db.AgentRuntime{
		ID:          util.MustParseUUID("20000000-0000-0000-0000-000000000001"),
		WorkspaceID: util.MustParseUUID("20000000-0000-0000-0000-000000000002"),
	}

	serverProvider.Set(featureflagdispatch.RuntimeBriefSlimFlag, featureflag.Rule{Default: true})
	ApplyFeatureFlagSnapshot(evaluator.EvaluateForRuntime(context.Background(), rt))
	if !useSlimBrief() {
		t.Fatal("server heartbeat snapshot default=true should enable slim brief")
	}

	serverProvider.Set(featureflagdispatch.RuntimeBriefSlimFlag, featureflag.Rule{Default: false})
	ApplyFeatureFlagSnapshot(evaluator.EvaluateForRuntime(context.Background(), rt))
	if useSlimBrief() {
		t.Fatal("later server heartbeat snapshot default=false should disable slim brief")
	}
}

func TestNilHeartbeatSnapshotFallsBackToStaticProvider(t *testing.T) {
	savedFlags := runtimeFlags.Load()
	savedProvider := activeServerSnapshotProvider.Load()
	t.Cleanup(func() {
		runtimeFlags.Store(savedFlags)
		activeServerSnapshotProvider.Store(savedProvider)
	})

	serverSnapshot := NewServerSnapshotProvider()
	serverSnapshot.Apply(ServerSnapshot{
		Version: 1,
		Flags:   map[string]string{runtimeBriefSlimFlag: "off"},
	})
	static := featureflag.NewStaticProvider()
	static.Set(runtimeBriefSlimFlag, featureflag.Rule{Default: true})
	SetServerSnapshotProvider(serverSnapshot)
	SetFeatureFlags(featureflag.NewService(featureflag.NewChainProvider(serverSnapshot, static)))

	if useSlimBrief() {
		t.Fatal("server snapshot off should suppress static provider before old-server fallback")
	}
	ApplyFeatureFlagSnapshot(nil)
	if !useSlimBrief() {
		t.Fatal("missing heartbeat feature_flags should clear server snapshot and fall back to static provider")
	}
}

func TestApplyFeatureFlagSnapshotNoProviderIsSafe(t *testing.T) {
	savedProvider := activeServerSnapshotProvider.Load()
	activeServerSnapshotProvider.Store(nil)
	t.Cleanup(func() { activeServerSnapshotProvider.Store(savedProvider) })

	ApplyFeatureFlagSnapshot(&protocol.DaemonFeatureFlagSnapshot{
		Version: 1,
		Flags:   map[string]string{runtimeBriefSlimFlag: "on"},
	})
}

func TestApplyFeatureFlagSnapshotNilClearsProvider(t *testing.T) {
	savedProvider := activeServerSnapshotProvider.Load()
	provider := NewServerSnapshotProvider()
	provider.Apply(ServerSnapshot{
		Version: 1,
		Flags:   map[string]string{runtimeBriefSlimFlag: "on"},
	})
	SetServerSnapshotProvider(provider)
	t.Cleanup(func() { activeServerSnapshotProvider.Store(savedProvider) })

	ApplyFeatureFlagSnapshot(nil)
	if _, ok := provider.Lookup(context.Background(), runtimeBriefSlimFlag); ok {
		t.Fatal("nil heartbeat snapshot should clear the server snapshot provider")
	}
}

func boolVariant(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}
