package service

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// /note must remain quiet even when an explicit subscription bypasses the
// handler's direct-trigger resolver. Exercise persisted capture and dispatch.
func TestIssueWakeupNoteComment(t *testing.T) {
	for _, authorType := range []string{"member", "agent"} {
		t.Run(authorType, func(t *testing.T) {
			f, s, issue, agent := wakeFixture(t)
			authorID := f.UserID
			if authorType == "agent" {
				authorID = agent
			}
			w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, FilterActorType: authorType, FilterActorID: authorID, Instruction: "React to ordinary comments"})
			disabled := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}, Instruction: "Disabled rule"})
			if _, err := s.Disable(context.Background(), issue, disabled.ID, parseTestUUID(t, f.UserID)); err != nil {
				t.Fatal(err)
			}
			other := f.User(t, "other note author", "other-note-author@multica.test")
			f.Member(t, f.WorkspaceID, other, "member")
			filtered := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}, FilterActorType: "member", FilterActorID: other, Instruction: "Wait for another author"})
			updated := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", EventTypes: []string{"issue.updated"}, Instruction: "Read changed issue"})
			for _, content := range []string{"/note", "/note acceptance recorded", " \t\r\n/NOTE quiet", "/NoTe\u00a0quiet", "/note\u2003quiet"} {
				id := f.Insert(t, "comment", testutil.Cols{"issue_id": issue, "workspace_id": f.WorkspaceID, "author_type": authorType, "author_id": authorID, "content": content, "type": "comment"})
				if got := f.Count(t, "SELECT count(*) FROM comment WHERE id=$1 AND content=$2", id, content); got != 1 {
					t.Fatalf("%q was not preserved", content)
				}
				if got := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID); got != 0 {
					t.Fatalf("%q captured %d receipts", content, got)
				}
				wakeDispatch(t, s, w)
				if got := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID)); got != 0 {
					t.Fatalf("%q queued %d runs", content, got)
				}
			}
			current, err := f.q.GetIssueWakeup(context.Background(), db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: parseTestUUID(t, f.WorkspaceID)})
			if err != nil || !current.Enabled {
				t.Fatalf("quiet notes consumed the once rule: %+v %v", current, err)
			}
			f.Insert(t, "comment", testutil.Cols{"issue_id": issue, "workspace_id": f.WorkspaceID, "author_type": authorType, "author_id": authorID, "content": "/notes ordinary", "type": "comment"})
			if got := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", w.ID); got != 1 {
				t.Fatalf("ordinary comment captured %d receipts, want 1", got)
			}
			for _, untouched := range []db.IssueWakeup{disabled, filtered} {
				if got := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", untouched.ID); got != 0 {
					t.Fatalf("disabled/filtered rule captured %d receipts", got)
				}
			}
			f.Exec(t, "UPDATE issue SET title='changed after note' WHERE id=$1", issue)
			if got := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1", updated.ID); got != 1 {
				t.Fatalf("unrelated issue update captured %d receipts, want 1", got)
			}
			wakeDispatch(t, s, w)
			if got := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID)); got != 1 {
				t.Fatalf("ordinary comment queued %d runs, want 1", got)
			}
		})
	}
}
