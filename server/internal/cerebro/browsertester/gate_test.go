package browsertester

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func testUUID(value byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = value
	return pgtype.UUID{Bytes: bytes, Valid: true}
}

func TestAgentGroupIDsRequiresExactAgentAllowlist(t *testing.T) {
	groupID := testUUID(1)
	allowedAgentID := testUUID(2)
	otherAgentID := testUUID(3)
	groups := []cerebrodb.CerebroGroup{{ID: groupID, Name: "browser-testers"}}

	listAgents := func(context.Context, pgtype.UUID) ([]cerebrodb.CerebroGroupAgentAccess, error) {
		return []cerebrodb.CerebroGroupAgentAccess{{GroupID: groupID, AgentID: allowedAgentID}}, nil
	}

	allowedGroups, err := AgentGroupIDs(context.Background(), groups, otherAgentID, listAgents)
	if err != nil {
		t.Fatalf("AgentGroupIDs returned error: %v", err)
	}
	if len(allowedGroups) != 0 {
		t.Fatalf("owner membership alone granted agent access: %v", allowedGroups)
	}
}

func TestAgentGroupIDsAllowsListedAgent(t *testing.T) {
	groupID := testUUID(1)
	agentID := testUUID(2)
	groups := []cerebrodb.CerebroGroup{{ID: groupID, Name: "Browser-Testers"}}

	listAgents := func(context.Context, pgtype.UUID) ([]cerebrodb.CerebroGroupAgentAccess, error) {
		return []cerebrodb.CerebroGroupAgentAccess{{GroupID: groupID, AgentID: agentID}}, nil
	}

	allowedGroups, err := AgentGroupIDs(context.Background(), groups, agentID, listAgents)
	if err != nil {
		t.Fatalf("AgentGroupIDs returned error: %v", err)
	}
	if _, ok := allowedGroups[groupID]; !ok {
		t.Fatalf("listed agent was not granted: %v", allowedGroups)
	}
}

func TestAgentGroupIDsFailsClosedOnLookupError(t *testing.T) {
	groupID := testUUID(1)
	agentID := testUUID(2)
	groups := []cerebrodb.CerebroGroup{{ID: groupID, Name: "browser-testers"}}
	wantErr := errors.New("lookup failed")

	listAgents := func(context.Context, pgtype.UUID) ([]cerebrodb.CerebroGroupAgentAccess, error) {
		return nil, wantErr
	}

	allowedGroups, err := AgentGroupIDs(context.Background(), groups, agentID, listAgents)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if len(allowedGroups) != 0 {
		t.Fatalf("lookup error must fail closed, got %v", allowedGroups)
	}
}
