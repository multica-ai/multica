package mentiongate

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// FIR-2409: the mention gate now layers the configurable trigger_other_agent
// capability on top of the hardcoded baseline. These tests prove the two
// headline behaviors — an explicit Deny removes the owner/admin master key, and
// an explicit Allow grants a member the baseline would block — while a
// workspace with no rule keeps today's behavior (covered by gate_test.go).

func policyStore() *toolpolicy.Store {
	return toolpolicy.NewStoreFromQueries(cerebrodb.New(mentionGatePool))
}

func TestCanTriggerMention_PolicyDenyRemovesAdminMasterKey(t *testing.T) {
	if mentionGatePool == nil {
		t.Skip("db unreachable")
	}
	svc, _, agentID, ownerID := setupMentionGateFixture(t)
	adminID := createMentionGateMemberWithRole(t, "ws-admin", "owner")
	store := policyStore()
	ctx := context.Background()
	ws := uuidString(mentionGateWorkspaceID)

	// Baseline: an owner/admin holds the master key for someone else's agent.
	req := httptest.NewRequest("POST", "/api/issues/comment", nil)
	req.Header.Set("X-User-ID", uuidString(adminID))
	allowed, err := svc.CanTriggerMention(ctx, req, ws, agentID, ownerID)
	if err != nil {
		t.Fatalf("baseline CanTriggerMention: %v", err)
	}
	if !allowed {
		t.Fatal("baseline: owner/admin should hold the master key")
	}

	// An agent-layer Deny on trigger_other_agent removes the master key.
	if _, err := store.Set(ctx, toolpolicy.SetParams{
		WorkspaceID: mentionGateWorkspaceID,
		ToolKey:     triggerOtherAgentKey,
		Layer:       toolpolicy.LayerAgent,
		SubjectID:   agentID,
		Setting:     toolpolicy.SettingDeny,
	}); err != nil {
		t.Fatalf("set agent Deny: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Clear(context.Background(), mentionGateWorkspaceID, triggerOtherAgentKey, toolpolicy.LayerAgent, agentID, "")
	})

	req2 := httptest.NewRequest("POST", "/api/issues/comment", nil)
	req2.Header.Set("X-User-ID", uuidString(adminID))
	allowed, err = svc.CanTriggerMention(ctx, req2, ws, agentID, ownerID)
	if err != nil {
		t.Fatalf("post-Deny CanTriggerMention: %v", err)
	}
	if allowed {
		t.Fatal("agent-layer Deny should remove the owner/admin master key")
	}
}

func TestCanTriggerMention_PolicyAllowGrantsBlockedMember(t *testing.T) {
	if mentionGatePool == nil {
		t.Skip("db unreachable")
	}
	svc, commenterID, agentID, ownerID := setupMentionGateFixture(t)
	store := policyStore()
	ctx := context.Background()
	ws := uuidString(mentionGateWorkspaceID)

	// Baseline: a plain member with no group access is blocked.
	req := httptest.NewRequest("POST", "/api/issues/comment", nil)
	req.Header.Set("X-User-ID", uuidString(commenterID))
	allowed, err := svc.CanTriggerMention(ctx, req, ws, agentID, ownerID)
	if err != nil {
		t.Fatalf("baseline CanTriggerMention: %v", err)
	}
	if allowed {
		t.Fatal("baseline: member without group access should be blocked")
	}

	// A member-layer Allow on trigger_other_agent grants the member.
	if _, err := store.Set(ctx, toolpolicy.SetParams{
		WorkspaceID: mentionGateWorkspaceID,
		ToolKey:     triggerOtherAgentKey,
		Layer:       toolpolicy.LayerUser,
		SubjectID:   commenterID,
		Setting:     toolpolicy.SettingAllow,
	}); err != nil {
		t.Fatalf("set user Allow: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Clear(context.Background(), mentionGateWorkspaceID, triggerOtherAgentKey, toolpolicy.LayerUser, commenterID, "")
	})

	req2 := httptest.NewRequest("POST", "/api/issues/comment", nil)
	req2.Header.Set("X-User-ID", uuidString(commenterID))
	allowed, err = svc.CanTriggerMention(ctx, req2, ws, agentID, ownerID)
	if err != nil {
		t.Fatalf("post-Allow CanTriggerMention: %v", err)
	}
	if !allowed {
		t.Fatal("member-layer Allow should grant a baseline-blocked member")
	}
}

func createMentionGateMemberWithRole(t *testing.T, name, role string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	email := name + "@cerebro-mentiongate-tests.local"
	var userID string
	if err := mentionGatePool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, name, email).Scan(&userID); err != nil {
		t.Fatalf("create user %q: %v", name, err)
	}
	if _, err := mentionGatePool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, $3)
	`, mentionGateWorkspaceID, userID, role); err != nil {
		t.Fatalf("create member %q: %v", name, err)
	}
	t.Cleanup(func() {
		mentionGatePool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return parseUUID(userID)
}
