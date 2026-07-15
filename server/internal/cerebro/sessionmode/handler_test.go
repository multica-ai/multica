package sessionmode

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCanManageMemberRequiresOwnerOrAdmin(t *testing.T) {
	for _, role := range []string{"owner", "admin", "OWNER"} {
		if !canManageMember(db.Member{Role: role}) {
			t.Fatalf("role %q should manage modes", role)
		}
	}
	for _, role := range []string{"member", "guest", ""} {
		if canManageMember(db.Member{Role: role}) {
			t.Fatalf("role %q unexpectedly manages modes", role)
		}
	}
}

func TestParseManagedModeRejectsLegacyAndUnknownValues(t *testing.T) {
	if got, err := parseManagedMode("plan"); err != nil || got != Plan {
		t.Fatalf("plan = %q, %v", got, err)
	}
	for _, value := range []string{"auto", "default", "security", ""} {
		if _, err := parseManagedMode(value); err == nil {
			t.Fatalf("mode %q accepted", value)
		}
	}
}

func TestActorAndWorkspaceRejectInvalidContext(t *testing.T) {
	member := db.Member{UserID: pgtype.UUID{Valid: false}}
	if _, _, err := actorAndWorkspace(member, "not-a-uuid"); err == nil {
		t.Fatal("invalid context accepted")
	}
}
