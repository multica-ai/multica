package evals

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCanEditEval(t *testing.T) {
	creator := uuid.New()
	eval := Eval{CreatedByID: creator, CreatedByType: "member"}

	cases := []struct {
		name      string
		member    db.Member
		ok        bool
		actorID   uuid.UUID
		actorType string
		want      bool
	}{
		{"workspace owner", db.Member{Role: "owner"}, true, uuid.New(), "member", true},
		{"workspace admin", db.Member{Role: "admin"}, true, uuid.New(), "member", true},
		{"eval creator", db.Member{Role: "member"}, true, creator, "member", true},
		{"same id wrong actor type", db.Member{Role: "member"}, true, creator, "agent", false},
		{"non-admin non-creator", db.Member{Role: "member"}, true, uuid.New(), "member", false},
		{"no member context, not creator", db.Member{}, false, uuid.New(), "member", false},
		{"no member context but creator", db.Member{}, false, creator, "member", true},
	}
	for _, tc := range cases {
		if got := canEditEval(tc.member, tc.ok, tc.actorID, tc.actorType, eval); got != tc.want {
			t.Errorf("%s: canEditEval = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRequestContextUsesValidatedActorResolver(t *testing.T) {
	workspaceID := uuid.New()
	memberID := uuid.New()
	forgedAgentID := uuid.New()
	member := db.Member{WorkspaceID: pgUUID(workspaceID), UserID: pgUUID(memberID), Role: "member"}
	h := &Handler{actorResolver: func(_ *http.Request, userID, _ string) (string, string) {
		return "member", userID
	}}
	ctx := middleware.SetMemberContext(context.Background(), workspaceID.String(), member)
	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	req.Header.Set("X-Agent-ID", forgedAgentID.String())
	rec := httptest.NewRecorder()

	actorID, _, actorType, ok := h.requestContext(rec, req)
	if !ok || actorType != "member" || actorID != memberID {
		t.Fatalf("resolved actor = (%s, %s, %v), want member %s", actorType, actorID, ok, memberID)
	}
}
