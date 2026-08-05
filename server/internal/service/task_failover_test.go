package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func failoverRuntime(id, workspace, owner, daemon, provider, status string) db.AgentRuntime {
	return db.AgentRuntime{
		ID:          util.MustParseUUID(id),
		WorkspaceID: util.MustParseUUID(workspace),
		OwnerID:     util.MustParseUUID(owner),
		DaemonID:    pgtype.Text{String: daemon, Valid: true},
		Provider:    provider,
		Status:      status,
	}
}

func TestConfiguredFailoverRuntimeIDs(t *testing.T) {
	const first = "10000000-0000-0000-0000-000000000001"
	const second = "10000000-0000-0000-0000-000000000002"

	got := configuredFailoverRuntimeIDs([]byte(`{"failover":{"runtime_ids":["` + first + `","bad","` + first + `","` + second + `"]}}`))
	if len(got) != 2 {
		t.Fatalf("got %d ids, want 2", len(got))
	}
	if got[0] != util.MustParseUUID(first) || got[1] != util.MustParseUUID(second) {
		t.Fatalf("ids = %v, want valid unique ids in configured order", got)
	}
	if got := configuredFailoverRuntimeIDs([]byte(`{"failover":`)); got != nil {
		t.Fatalf("malformed config returned %v, want nil", got)
	}
}

func TestSelectConfiguredFailoverRuntime(t *testing.T) {
	const (
		workspace = "20000000-0000-0000-0000-000000000001"
		owner     = "30000000-0000-0000-0000-000000000001"
		sourceID  = "40000000-0000-0000-0000-000000000001"
		offlineID = "40000000-0000-0000-0000-000000000002"
		wrongID   = "40000000-0000-0000-0000-000000000003"
		targetID  = "40000000-0000-0000-0000-000000000004"
	)

	source := failoverRuntime(sourceID, workspace, owner, "daemon-a", "claude", "online")
	offline := failoverRuntime(offlineID, workspace, owner, "daemon-a", "claude", "offline")
	wrongDaemon := failoverRuntime(wrongID, workspace, owner, "daemon-b", "claude", "online")
	target := failoverRuntime(targetID, workspace, owner, "daemon-a", "claude", "online")

	configured := []pgtype.UUID{offline.ID, wrongDaemon.ID, target.ID}
	got, ok := selectConfiguredFailoverRuntime(source.ID, configured, []db.AgentRuntime{target, wrongDaemon, source, offline})
	if !ok || got != target.ID {
		t.Fatalf("selected (%v, %v), want online compatible target %s", got, ok, targetID)
	}

	wrongProvider := target
	wrongProvider.Provider = "codex"
	if RuntimeResumeCompatible(source, wrongProvider) {
		t.Fatal("different providers must not be resume-compatible")
	}
	wrongOwner := target
	wrongOwner.OwnerID = util.MustParseUUID("30000000-0000-0000-0000-000000000002")
	if RuntimeResumeCompatible(source, wrongOwner) {
		t.Fatal("different owners must not be resume-compatible")
	}
}

func TestProviderLimitReasonsRequireFailover(t *testing.T) {
	if !isFailoverOnlyRetryReason("agent_error.provider_quota_limit") {
		t.Fatal("quota limit must use alternate-runtime retry")
	}
	if !isFailoverOnlyRetryReason("agent_error.provider_capacity_or_rate_limit") {
		t.Fatal("capacity limit must use alternate-runtime retry")
	}
	if isFailoverOnlyRetryReason("agent_error.provider_network") {
		t.Fatal("network errors keep the existing same-runtime retry path")
	}
}
