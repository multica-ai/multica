package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// uid returns a deterministic pgtype.UUID for tests. The tag is the only
// thing that varies between calls; we never compare these to real database
// UUIDs, only to each other.
func uid(tag byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	for i := range u.Bytes {
		u.Bytes[i] = tag
	}
	return u
}

func TestDecideFlip(t *testing.T) {
	agentA := uid(0xA1)
	agentB := uid(0xB2)
	memberM := uid(0xC3)

	mkIssue := func(status string, assigneeType string, assigneeID pgtype.UUID) db.Issue {
		i := db.Issue{Status: status}
		if assigneeType != "" {
			i.AssigneeType = pgtype.Text{String: assigneeType, Valid: true}
			i.AssigneeID = assigneeID
		}
		return i
	}

	mkComment := func(authorType string, authorID pgtype.UUID, commentType string) db.Comment {
		return db.Comment{
			AuthorType: authorType,
			AuthorID:   authorID,
			Type:       commentType,
		}
	}

	tests := []struct {
		name    string
		comment db.Comment
		issue   db.Issue
		want    *FlipTransition
	}{
		// ── Rule A: member commenting on waiting ──────────────────────────
		{
			name:    "rule_a/member comment on waiting agent issue → flip",
			comment: mkComment("member", memberM, "comment"),
			issue:   mkIssue("waiting", "agent", agentA),
			want:    &FlipTransition{FromStatus: "waiting", ToStatus: "in_progress"},
		},

		// ── Rule B: agent assignee comment on in_progress ─────────────────
		{
			name:    "rule_b/agent assignee comment on in_progress → flip",
			comment: mkComment("agent", agentA, "comment"),
			issue:   mkIssue("in_progress", "agent", agentA),
			want:    &FlipTransition{FromStatus: "in_progress", ToStatus: "waiting"},
		},
		{
			name:    "rule_b/non-assignee agent comment on in_progress → no flip",
			comment: mkComment("agent", agentB, "comment"),
			issue:   mkIssue("in_progress", "agent", agentA),
			want:    nil,
		},

		// ── comment.type filter ───────────────────────────────────────────
		{
			name:    "type=progress_update from agent assignee → no flip (opt-out)",
			comment: mkComment("agent", agentA, "progress_update"),
			issue:   mkIssue("in_progress", "agent", agentA),
			want:    nil,
		},
		{
			name:    "type=progress_update from member on waiting → no flip",
			comment: mkComment("member", memberM, "progress_update"),
			issue:   mkIssue("waiting", "agent", agentA),
			want:    nil,
		},
		{
			name:    "type=status_change → no flip (server-generated)",
			comment: mkComment("agent", agentA, "status_change"),
			issue:   mkIssue("in_progress", "agent", agentA),
			want:    nil,
		},
		{
			name:    "type=system → no flip (server-generated)",
			comment: mkComment("agent", agentA, "system"),
			issue:   mkIssue("in_progress", "agent", agentA),
			want:    nil,
		},

		// ── assignee_type filter ──────────────────────────────────────────
		{
			name:    "assignee_type=member → no flip (Rule A)",
			comment: mkComment("member", memberM, "comment"),
			issue:   mkIssue("waiting", "member", memberM),
			want:    nil,
		},
		{
			name:    "assignee_type=member with agent comment → no flip (Rule B)",
			comment: mkComment("agent", agentA, "comment"),
			issue:   mkIssue("in_progress", "member", memberM),
			want:    nil,
		},
		{
			name:    "assignee_type=NULL (unassigned) → no flip",
			comment: mkComment("member", memberM, "comment"),
			issue:   db.Issue{Status: "waiting"}, // AssigneeType.Valid == false
			want:    nil,
		},

		// ── status whitelist ──────────────────────────────────────────────
		{
			name:    "agent comment on todo → no flip",
			comment: mkComment("agent", agentA, "comment"),
			issue:   mkIssue("todo", "agent", agentA),
			want:    nil,
		},
		{
			name:    "agent comment on in_review → no flip",
			comment: mkComment("agent", agentA, "comment"),
			issue:   mkIssue("in_review", "agent", agentA),
			want:    nil,
		},
		{
			name:    "agent comment on planned → no flip",
			comment: mkComment("agent", agentA, "comment"),
			issue:   mkIssue("planned", "agent", agentA),
			want:    nil,
		},
		{
			name:    "agent comment on developing → no flip",
			comment: mkComment("agent", agentA, "comment"),
			issue:   mkIssue("developing", "agent", agentA),
			want:    nil,
		},
		{
			name:    "agent comment on deployed → no flip",
			comment: mkComment("agent", agentA, "comment"),
			issue:   mkIssue("deployed", "agent", agentA),
			want:    nil,
		},
		{
			name:    "agent comment on blocked → no flip",
			comment: mkComment("agent", agentA, "comment"),
			issue:   mkIssue("blocked", "agent", agentA),
			want:    nil,
		},
		{
			name:    "agent comment on done → no flip",
			comment: mkComment("agent", agentA, "comment"),
			issue:   mkIssue("done", "agent", agentA),
			want:    nil,
		},
		{
			name:    "agent comment on cancelled → no flip",
			comment: mkComment("agent", agentA, "comment"),
			issue:   mkIssue("cancelled", "agent", agentA),
			want:    nil,
		},
		{
			name:    "agent comment on backlog → no flip",
			comment: mkComment("agent", agentA, "comment"),
			issue:   mkIssue("backlog", "agent", agentA),
			want:    nil,
		},
		{
			name:    "member comment on in_progress → no flip (Rule A wants waiting)",
			comment: mkComment("member", memberM, "comment"),
			issue:   mkIssue("in_progress", "agent", agentA),
			want:    nil,
		},
		{
			name:    "agent comment on waiting → no flip (Rule B wants in_progress)",
			comment: mkComment("agent", agentA, "comment"),
			issue:   mkIssue("waiting", "agent", agentA),
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideFlip(tt.comment, tt.issue)
			if (got == nil) != (tt.want == nil) {
				t.Fatalf("DecideFlip nil mismatch: got=%v want=%v", got, tt.want)
			}
			if got == nil {
				return
			}
			if got.FromStatus != tt.want.FromStatus || got.ToStatus != tt.want.ToStatus {
				t.Fatalf("DecideFlip transition mismatch:\n got  %+v\n want %+v", got, tt.want)
			}
		})
	}
}
