package runtimeaccess

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRuntimeAccessCanUseRuntime(t *testing.T) {
	workspace := runtimeAccessUUID(1)
	otherWorkspace := runtimeAccessUUID(2)
	owner := runtimeAccessUUID(3)
	caller := runtimeAccessUUID(4)

	tests := []struct {
		name    string
		member  db.Member
		runtime db.AgentRuntime
		want    bool
	}{
		{
			name:    "workspace owner",
			member:  db.Member{WorkspaceID: workspace, UserID: caller, Role: "owner"},
			runtime: db.AgentRuntime{WorkspaceID: workspace, OwnerID: owner, Visibility: "private"},
			want:    true,
		},
		{
			name:    "workspace admin",
			member:  db.Member{WorkspaceID: workspace, UserID: caller, Role: "admin"},
			runtime: db.AgentRuntime{WorkspaceID: workspace, OwnerID: owner, Visibility: "private"},
			want:    true,
		},
		{
			name:    "private runtime owner",
			member:  db.Member{WorkspaceID: workspace, UserID: owner, Role: "member"},
			runtime: db.AgentRuntime{WorkspaceID: workspace, OwnerID: owner, Visibility: "private"},
			want:    true,
		},
		{
			name:    "public runtime member",
			member:  db.Member{WorkspaceID: workspace, UserID: caller, Role: "member"},
			runtime: db.AgentRuntime{WorkspaceID: workspace, OwnerID: owner, Visibility: "public"},
			want:    true,
		},
		{
			name:    "private runtime nonowner",
			member:  db.Member{WorkspaceID: workspace, UserID: caller, Role: "member"},
			runtime: db.AgentRuntime{WorkspaceID: workspace, OwnerID: owner, Visibility: "private"},
			want:    false,
		},
		{
			name:    "workspace mismatch fails closed",
			member:  db.Member{WorkspaceID: otherWorkspace, UserID: owner, Role: "owner"},
			runtime: db.AgentRuntime{WorkspaceID: workspace, OwnerID: owner, Visibility: "public"},
			want:    false,
		},
		{
			name:    "invalid member user fails closed",
			member:  db.Member{WorkspaceID: workspace, Role: "owner"},
			runtime: db.AgentRuntime{WorkspaceID: workspace, OwnerID: owner, Visibility: "public"},
			want:    false,
		},
		{
			name:    "invalid workspace scope fails closed",
			member:  db.Member{UserID: caller, Role: "owner"},
			runtime: db.AgentRuntime{OwnerID: owner, Visibility: "public"},
			want:    false,
		},
		{
			name:    "unknown visibility fails closed",
			member:  db.Member{WorkspaceID: workspace, UserID: caller, Role: "member"},
			runtime: db.AgentRuntime{WorkspaceID: workspace, OwnerID: owner, Visibility: "future"},
			want:    false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CanUse(test.member, test.runtime); got != test.want {
				t.Fatalf("CanUse(role=%q, visibility=%q) = %v, want %v", test.member.Role, test.runtime.Visibility, got, test.want)
			}
		})
	}
}

func runtimeAccessUUID(last byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = last
	return pgtype.UUID{Bytes: bytes, Valid: true}
}
