package agentoffice

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	parsed, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("ParseUUID(%q): %v", s, err)
	}
	return parsed
}

func hasUUID(got []pgtype.UUID, want pgtype.UUID) bool {
	for _, g := range got {
		if util.UUIDToString(g) == util.UUIDToString(want) {
			return true
		}
	}
	return false
}

const (
	uOwner    = "11111111-1111-1111-1111-111111111111"
	uApprover = "22222222-2222-2222-2222-222222222222"
	uProposer = "33333333-3333-3333-3333-333333333333"
)

func TestChangeRequestRecipients(t *testing.T) {
	owner := mustUUID(t, uOwner)
	approver := mustUUID(t, uApprover)
	proposer := mustUUID(t, uProposer)

	t.Run("owner and approvers minus proposer", func(t *testing.T) {
		agent := cerebrodb.Agent{ContextOwnerID: owner, ContextApproverIds: []pgtype.UUID{approver}}
		got := changeRequestRecipients(agent, proposer)
		if len(got) != 2 {
			t.Fatalf("want 2 recipients, got %d", len(got))
		}
		if !hasUUID(got, owner) || !hasUUID(got, approver) {
			t.Fatalf("missing expected recipient: %v", got)
		}
	})

	t.Run("proposer excluded even when owner", func(t *testing.T) {
		agent := cerebrodb.Agent{ContextOwnerID: proposer, ContextApproverIds: []pgtype.UUID{approver}}
		got := changeRequestRecipients(agent, proposer)
		if len(got) != 1 || !hasUUID(got, approver) {
			t.Fatalf("want approver only, got %v", got)
		}
	})

	t.Run("duplicates collapse", func(t *testing.T) {
		agent := cerebrodb.Agent{ContextOwnerID: owner, ContextApproverIds: []pgtype.UUID{owner, approver, approver}}
		got := changeRequestRecipients(agent, proposer)
		if len(got) != 2 {
			t.Fatalf("want 2 deduped recipients, got %d", len(got))
		}
	})

	t.Run("no owner, no approvers yields none", func(t *testing.T) {
		if got := changeRequestRecipients(cerebrodb.Agent{}, proposer); len(got) != 0 {
			t.Fatalf("want 0 recipients, got %d", len(got))
		}
	})
}

func TestChangeRequestBody(t *testing.T) {
	if got := changeRequestBody("Mia", "1.0.0", "1.1.0", ""); got != "1.0.0 → 1.1.0 on Mia" {
		t.Errorf("unexpected body without description: %q", got)
	}
	if got := changeRequestBody("Mia", "1.0.0", "1.1.0", "tighten rules"); got != "1.0.0 → 1.1.0 on Mia: tighten rules" {
		t.Errorf("unexpected body with description: %q", got)
	}
}
