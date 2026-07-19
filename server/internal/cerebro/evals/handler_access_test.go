package evals

import (
	"testing"

	"github.com/google/uuid"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCanEditEval(t *testing.T) {
	creator := uuid.New()
	eval := Eval{CreatedByID: creator}

	cases := []struct {
		name    string
		member  db.Member
		ok      bool
		actorID uuid.UUID
		want    bool
	}{
		{"workspace owner", db.Member{Role: "owner"}, true, uuid.New(), true},
		{"workspace admin", db.Member{Role: "admin"}, true, uuid.New(), true},
		{"eval creator", db.Member{Role: "member"}, true, creator, true},
		{"non-admin non-creator", db.Member{Role: "member"}, true, uuid.New(), false},
		{"no member context, not creator", db.Member{}, false, uuid.New(), false},
		{"no member context but creator", db.Member{}, false, creator, true},
	}
	for _, tc := range cases {
		if got := canEditEval(tc.member, tc.ok, tc.actorID, eval); got != tc.want {
			t.Errorf("%s: canEditEval = %v, want %v", tc.name, got, tc.want)
		}
	}
}
