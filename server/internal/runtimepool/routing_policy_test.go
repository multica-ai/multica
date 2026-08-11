package runtimepool

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRuntimeMatchesTriggerPolicy(t *testing.T) {
	userID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	otherID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	tests := []struct {
		name           string
		mode           string
		owner, trigger pgtype.UUID
		want           bool
	}{
		{name: "own local", mode: "local", owner: userID, trigger: userID, want: true},
		{name: "other local", mode: "local", owner: otherID, trigger: userID},
		{name: "anonymous local", mode: "local", owner: userID},
		{name: "cloud with user", mode: "cloud", owner: otherID, trigger: userID, want: true},
		{name: "cloud anonymous", mode: "cloud", owner: otherID, want: true},
		{name: "unknown mode", mode: "custom", owner: userID, trigger: userID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RuntimeMatchesTriggerPolicy(db.AgentRuntime{RuntimeMode: tc.mode, OwnerID: tc.owner}, tc.trigger); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
