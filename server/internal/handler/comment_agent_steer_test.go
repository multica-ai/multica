package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestAgentCommentSteersOnlyItsSquadWorkers(t *testing.T) {
	for _, tc := range []struct {
		name                                                         string
		unrelated, archived, mutual, noOriginator, self, wantSteered bool
	}{
		{name: "leader steers its worker", wantSteered: true},
		{name: "agent outside the worker's squads", unrelated: true},
		{name: "leader of an archived squad", archived: true},
		{name: "agents leading each other", mutual: true},
		{name: "leader whose chain may not invoke the worker", noOriginator: true},
		{name: "leader's own running turn", self: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSupplementFixture(t, "grok", "running", true)
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, f.issueID)
			callerAgent, callerTask := runningSteerableTask(t, f, "steer-caller")
			if !tc.unrelated {
				cols := testutil.Cols{}
				if tc.archived {
					cols["archived_at"] = testutil.Raw("now()")
				}
				dbfx.SquadMember(t, dbfx.Squad(t, "Crew", callerAgent, cols), "agent", f.agentID)
			}
			if tc.mutual {
				dbfx.SquadMember(t, dbfx.Squad(t, "Other crew", f.agentID), "agent", callerAgent)
			}
			if tc.noOriginator {
				dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id = NULL, accountable_user_id = NULL, originator_source = NULL WHERE id = $1`, callerTask)
			} else {
				dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id = $2, accountable_user_id = $2, originator_source = 'comment_source' WHERE id = $1`, callerTask, testUserID)
			}
			target, targetTask := f.agentID, f.taskID
			if tc.self {
				target, targetTask = callerAgent, callerTask
			}

			req := newRequest(http.MethodPost, "/api/issues/"+f.issueID+"/comments", map[string]any{
				"content": agentMention("Target", target) + " only fix web", "steer_task_ids": []string{targetTask},
			})
			req.Header.Set("X-Actor-Source", "task_token")
			req.Header.Set("X-Agent-ID", callerAgent)
			req.Header.Set("X-Task-ID", callerTask)
			var created CommentResponse
			testutil.Call(t, testHandler.CreateComment, withURLParam(req, "id", f.issueID)).Want(http.StatusCreated).JSON(&created)

			outcome, ok := outcomeFor(created.TriggerOutcomes, target)
			if !ok {
				t.Fatalf("no outcome for the mentioned agent: %+v", created.TriggerOutcomes)
			}
			if steered := outcome.Status == DispatchSteered; steered != tc.wantSteered {
				t.Fatalf("outcome = %+v, want steered=%v", outcome, tc.wantSteered)
			}
		})
	}
}
