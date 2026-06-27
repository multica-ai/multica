package featureflagdispatch

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func TestEvaluateForRuntimeWritesDaemonBoundSnapshot(t *testing.T) {
	t.Parallel()

	provider := featureflag.NewStaticProvider()
	provider.Set(RuntimeBriefSlimFlag, featureflag.Rule{Default: true})
	evaluator := NewEvaluator(featureflag.NewService(provider))

	snapshot := evaluator.EvaluateForRuntime(context.Background(), testRuntime("00000000-0000-0000-0000-000000000001", "daemon-a"))
	if snapshot == nil {
		t.Fatal("snapshot is nil")
	}
	if snapshot.Version != defaultSnapshotVersion {
		t.Fatalf("snapshot version = %d, want %d", snapshot.Version, defaultSnapshotVersion)
	}
	if got := snapshot.Flags[RuntimeBriefSlimFlag]; got != "on" {
		t.Fatalf("%s = %q, want on", RuntimeBriefSlimFlag, got)
	}
	if len(snapshot.Flags) != len(DaemonBoundFlags) {
		t.Fatalf("snapshot flags = %#v, want exactly daemon-bound registry", snapshot.Flags)
	}
}

func TestEvaluateForRuntimeIncludesDaemonContext(t *testing.T) {
	t.Parallel()

	provider := featureflag.NewStaticProvider()
	provider.Set(RuntimeBriefSlimFlag, featureflag.Rule{
		Default: false,
		Allow:   []string{"daemon-allowed"},
		AllowBy: "daemon_id",
	})
	evaluator := NewEvaluator(featureflag.NewService(provider))

	snapshot := evaluator.EvaluateForRuntime(context.Background(), testRuntime("00000000-0000-0000-0000-000000000002", "daemon-allowed"))
	if got := snapshot.Flags[RuntimeBriefSlimFlag]; got != "on" {
		t.Fatalf("%s = %q, want on for daemon_id allow", RuntimeBriefSlimFlag, got)
	}
}

func TestEvaluateForRuntimeIsDaemonProcessScoped(t *testing.T) {
	t.Parallel()

	provider := featureflag.NewStaticProvider()
	provider.Set(RuntimeBriefSlimFlag, featureflag.Rule{
		Default: false,
		Allow:   []string{"daemon-a"},
		AllowBy: "daemon_id",
	})
	evaluator := NewEvaluator(featureflag.NewService(provider))

	snapshotA := evaluator.EvaluateForRuntime(context.Background(), testRuntime("00000000-0000-0000-0000-0000000000aa", "daemon-a"))
	snapshotB := evaluator.EvaluateForRuntime(context.Background(), testRuntime("00000000-0000-0000-0000-0000000000bb", "daemon-a"))
	if got := snapshotA.Flags[RuntimeBriefSlimFlag]; got != "on" {
		t.Fatalf("workspace A %s = %q, want on", RuntimeBriefSlimFlag, got)
	}
	if got := snapshotB.Flags[RuntimeBriefSlimFlag]; got != "on" {
		t.Fatalf("workspace B %s = %q, want on", RuntimeBriefSlimFlag, got)
	}
}

func TestEvaluateForRuntimeDoesNotUseWorkspaceContext(t *testing.T) {
	t.Parallel()

	provider := featureflag.NewStaticProvider()
	provider.Set(RuntimeBriefSlimFlag, featureflag.Rule{
		Default: false,
		Allow:   []string{"00000000-0000-0000-0000-0000000000aa"},
		AllowBy: "workspace_id",
	})
	evaluator := NewEvaluator(featureflag.NewService(provider))

	snapshot := evaluator.EvaluateForRuntime(context.Background(), testRuntime("00000000-0000-0000-0000-0000000000aa", "daemon-a"))
	if got := snapshot.Flags[RuntimeBriefSlimFlag]; got != "off" {
		t.Fatalf("%s = %q, want off because daemon snapshots are process-scoped, not workspace-scoped", RuntimeBriefSlimFlag, got)
	}
}

func testRuntime(workspaceID, daemonID string) db.AgentRuntime {
	return db.AgentRuntime{
		ID:          util.MustParseUUID("10000000-0000-0000-0000-000000000001"),
		WorkspaceID: util.MustParseUUID(workspaceID),
		DaemonID:    pgtype.Text{String: daemonID, Valid: daemonID != ""},
	}
}
