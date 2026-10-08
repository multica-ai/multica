package service

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/entitlement"
)

func TestResolveAutopilotNotificationRecipientRejectsFormerMember(t *testing.T) {
	fixture := newAutopilotQuotaFixture(t, entitlement.ActionOff, 1)
	ctx := context.Background()
	autopilot, err := fixture.queries.GetAutopilot(ctx, fixture.autopilotID)
	if err != nil {
		t.Fatalf("load autopilot: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`,
		fixture.workspaceID, fixture.publisherID,
	); err != nil {
		t.Fatalf("remove autopilot creator from workspace: %v", err)
	}

	recipient, ok, err := ResolveAutopilotNotificationRecipient(ctx, fixture.queries, autopilot)
	if err != nil {
		t.Fatalf("resolve former member: %v", err)
	}
	if ok {
		t.Fatalf("former member resolved as recipient %+v", recipient)
	}
}
