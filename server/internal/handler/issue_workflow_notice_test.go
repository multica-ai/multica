package handler

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestWorkflowHandoffEnqueueNoticeRequiresValidTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, tc := range []struct {
		name    string
		task    db.AgentTaskQueue
		err     error
		notices int
		phrase  string
	}{
		{name: "queued", task: db.AgentTaskQueue{ID: dbid.NewV7()}},
		{name: "zero task", notices: 1, phrase: "Enqueue failed"},
		{name: "zero uuid", task: db.AgentTaskQueue{ID: pgtype.UUID{Valid: true}}, notices: 1, phrase: "Enqueue failed"},
		{name: "error", err: fmt.Errorf("private-runtime SQL credentials"), notices: 1, phrase: "Enqueue failed"},
		{name: "task with error", task: db.AgentTaskQueue{ID: dbid.NewV7()}, err: fmt.Errorf("private-runtime SQL credentials"), notices: 1, phrase: "Enqueue failed"},
		{name: "wrapped duplicate", err: fmt.Errorf("wrapped: %w", service.ErrDuplicatePendingTask), notices: 1, phrase: "Another pending run occupied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := dbfx.Issue(t, "diagnose only this handoff")
			dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id=$1`, id)
			issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(id))
			if err != nil {
				t.Fatal(err)
			}
			issue.Status = "in_review"
			issue.Revision = 42
			issue.UpdatedAt = pgtype.Timestamptz{Time: time.Date(2026, 1, 2, 3, 4, 5, 123000000, time.UTC), Valid: true}
			h := *testHandler
			h.Bus = events.New()
			var notices []CommentResponse
			h.Bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
				if e.ActorType != "system" || e.ActorID != "" {
					t.Fatal("notice actor")
				}
				notices = append(notices, e.Payload.(map[string]any)["comment"].(CommentResponse))
			})
			h.noteWorkflowHandoffEnqueueFailure(context.Background(), issue, tc.task, tc.err)
			if len(notices) != tc.notices {
				t.Fatalf("notices=%d want %d", len(notices), tc.notices)
			}
			for _, n := range notices {
				if n.Type != "system" || n.ParentID != nil || !strings.Contains(n.Content, tc.phrase) || !strings.Contains(n.Content, "2026-01-02T03:04:05.123Z (revision 42)") {
					t.Fatalf("wrong historical notice: %+v", n)
				}
				if strings.Contains(n.Content, "private-runtime") || len(util.ParseMentions(n.Content)) > 0 {
					t.Fatal("unsafe notice")
				}
			}
			var tasks int
			dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, id).Scan(&tasks)
			if tasks != 0 {
				t.Fatal("notice triggered work")
			}
		})
	}
}
