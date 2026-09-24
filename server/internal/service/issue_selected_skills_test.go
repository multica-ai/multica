package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestSelectedSkillIDsForMemberIssueCreateIsBoundedAndMemberScoped(t *testing.T) {
	issue := db.Issue{
		CreatorType: "member",
		Description: pgtype.Text{Valid: true, String: "[/one](slash://skill/00000000-0000-0000-0000-000000000001) [/bad](slash://skill/not-a-uuid) [/two](slash://skill/00000000-0000-0000-0000-000000000002)"},
	}
	got := selectedSkillIDsForMemberIssueCreate(issue)
	if len(got) != 2 || !got[0].Valid || !got[1].Valid {
		t.Fatalf("selected IDs = %#v, want two valid UUIDs", got)
	}
	if got[0].String() != "00000000-0000-0000-0000-000000000001" || got[1].String() != "00000000-0000-0000-0000-000000000002" {
		t.Fatalf("selected IDs = %v", got)
	}
	issue.CreatorType = "agent"
	if got := selectedSkillIDsForMemberIssueCreate(issue); got != nil {
		t.Fatalf("agent-authored description produced selections: %v", got)
	}
}
