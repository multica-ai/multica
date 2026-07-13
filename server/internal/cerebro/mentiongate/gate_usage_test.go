package mentiongate

// FIR-3091 punkt 8 fase 3: the mention gate is a live enforcement point, so
// every applied trigger_other_agent decision must leave one usage-log row —
// including baseline-decided calls (decided_by ""), so the Usage tab shows the
// permission being used even before any explicit rule exists.

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestCanTriggerMention_RecordsUsage(t *testing.T) {
	if mentionGatePool == nil {
		t.Skip("db unreachable")
	}
	svc, commenterID, agentID, ownerID := setupMentionGateFixture(t)
	ctx := context.Background()
	ws := uuidString(mentionGateWorkspaceID)

	clearUsage := func() {
		if _, err := mentionGatePool.Exec(ctx,
			`DELETE FROM cerebro_tool_policy_usage WHERE workspace_id = $1`, mentionGateWorkspaceID); err != nil {
			t.Fatalf("clear usage rows: %v", err)
		}
	}
	clearUsage()
	t.Cleanup(clearUsage)

	// A plain member with no group access is blocked by the baseline. The
	// enforced decision must land in the usage log with the baseline marker
	// (decided_by "").
	req := httptest.NewRequest("POST", "/api/issues/comment", nil)
	req.Header.Set("X-User-ID", uuidString(commenterID))
	allowed, err := svc.CanTriggerMention(ctx, req, ws, agentID, ownerID)
	if err != nil {
		t.Fatalf("CanTriggerMention: %v", err)
	}
	if allowed {
		t.Fatal("baseline: member without group access should be blocked")
	}

	var (
		count                                             int
		point, subjectType, subjectID, resource, decision string
		decidedBy                                         string
	)
	if err := mentionGatePool.QueryRow(ctx, `
		SELECT COUNT(*) FROM cerebro_tool_policy_usage
		WHERE workspace_id = $1 AND tool_key = $2
	`, mentionGateWorkspaceID, triggerOtherAgentKey).Scan(&count); err != nil {
		t.Fatalf("count usage rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("usage rows = %d, want 1", count)
	}
	if err := mentionGatePool.QueryRow(ctx, `
		SELECT enforcement_point, subject_type, subject_id::text, resource, decision, decided_by
		FROM cerebro_tool_policy_usage
		WHERE workspace_id = $1 AND tool_key = $2
	`, mentionGateWorkspaceID, triggerOtherAgentKey).Scan(&point, &subjectType, &subjectID, &resource, &decision, &decidedBy); err != nil {
		t.Fatalf("read usage row: %v", err)
	}
	if point != "mention_gate" || subjectType != "member" || subjectID != uuidString(commenterID) {
		t.Fatalf("subject = %s/%s/%s, want mention_gate/member/%s", point, subjectType, subjectID, uuidString(commenterID))
	}
	if resource != uuidString(agentID) {
		t.Fatalf("resource = %q, want the target agent id %q", resource, uuidString(agentID))
	}
	if decision != "deny" || decidedBy != "" {
		t.Fatalf("decision = %q decided_by = %q, want deny decided by the baseline (\"\")", decision, decidedBy)
	}
}
