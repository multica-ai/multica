package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func assignMentionFixtureSquad(t *testing.T, f supplementFixture) string {
	t.Helper()
	squadID := dbfx.Squad(t, "Explicit leader mention", f.agentID)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'squad', assignee_id = $2 WHERE id = $1`, f.issueID, squadID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, f.issueID)
	dbfx.Cleanup(t, `DELETE FROM activity_log WHERE issue_id = $1`, f.issueID)
	return squadID
}

func recordMentionEvaluation(t *testing.T, issueID, agentID, taskID, squadID string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running' WHERE id = $1`, taskID)
	testutil.Call(t, testHandler.RecordSquadLeaderEvaluation,
		evaluationRequest(issueID, agentID, taskID, "no_action")).Want(http.StatusCreated)
	got := loadEvaluations(t, issueID)
	if len(got) != 1 || got[0].ActorID != agentID || got[0].SquadID != squadID || got[0].Outcome != "no_action" {
		t.Fatalf("recorded wrong leader evaluation: %+v", got)
	}
}

func TestLeaderAgentMentionPreservesSelfTriggerRules(t *testing.T) {
	for _, mode := range []string{"leader", "same-squad-worker", "cross-issue-leader"} {
		t.Run(mode, func(t *testing.T) {
			f := newSupplementFixture(t, "claude", "running", false)
			squadID := assignMentionFixtureSquad(t, f)
			dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id = $2, accountable_user_id = $2, originator_source = 'comment_source', delivered_comment_ids = ARRAY[trigger_comment_id] WHERE id = $1`, f.taskID, testUserID)
			dbfx.Exec(t, `UPDATE agent_task_queue SET is_leader_task = $2, squad_id = $3 WHERE id = $1`,
				f.taskID, mode != "same-squad-worker", squadID)
			issueID := f.issueID
			if mode == "cross-issue-leader" {
				issueID = dbfx.Issue(t, "Cross-issue leader handoff", testutil.Cols{"assignee_type": "squad", "assignee_id": squadID})
				dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
			}
			body := map[string]any{"content": agentMention("Leader", f.agentID) + " next step"}
			if issueID == f.issueID {
				body["parent_id"] = f.triggerID
			}
			r := withURLParam(agentRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", body, f.agentID, f.taskID), "id", issueID)
			var created CommentResponse
			testutil.Call(t, testHandler.CreateComment, r).Want(http.StatusCreated).JSON(&created)
			n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task AND squad_id = $3`, issueID, f.agentID, squadID)
			if mode != "cross-issue-leader" {
				if n != 0 {
					t.Fatalf("same-issue %s self-mention queued %d more leader turns: %+v", mode, n, created.TriggerOutcomes)
				}
				outcome := findCommentOutcome(t, created.TriggerOutcomes, f.agentID)
				if outcome.Status != DispatchDeferred || outcome.ReasonCode != ReasonAlreadyActive {
					t.Fatalf("same-issue self-mention outcome: %+v", outcome)
				}
				completeTaskViaDaemon(t, f.taskID)
				if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`, issueID, f.agentID); n != 0 {
					t.Fatalf("same-issue %s self-mention replay queued %d turns", mode, n)
				}
			} else if n != 1 {
				t.Fatalf("legitimate %s handoff queued %d leader turns: %+v", mode, n, created.TriggerOutcomes)
			}
		})
	}
}

func TestLeaderAgentMentionMergesOnlyCompatibleTaskRoles(t *testing.T) {
	for _, role := range []string{"generic", "same-leader", "worker", "different-squad-leader"} {
		t.Run(role, func(t *testing.T) {
			f := newSupplementFixture(t, "claude", "queued", false)
			squadID := assignMentionFixtureSquad(t, f)
			dbfx.Exec(t, `UPDATE agent_task_queue SET comment_thread_id = $2 WHERE id = $1`, f.taskID, f.triggerID)
			switch role {
			case "same-leader", "worker":
				dbfx.Exec(t, `UPDATE agent_task_queue SET is_leader_task = $2, squad_id = $3 WHERE id = $1`, f.taskID, role == "same-leader", squadID)
			case "different-squad-leader":
				otherSquad := dbfx.Squad(t, "Other leader context", f.agentID)
				dbfx.Exec(t, `UPDATE agent_task_queue SET is_leader_task = TRUE, squad_id = $2 WHERE id = $1`, f.taskID, otherSquad)
			}
			before, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(f.taskID))
			if err != nil {
				t.Fatal(err)
			}
			var created CommentResponse
			postSteeredComment(t, f.issueID, f.triggerID, agentMention("Leader", f.agentID)+" review this").Want(http.StatusCreated).JSON(&created)
			outcome := findCommentOutcome(t, created.TriggerOutcomes, f.agentID)
			wantTaskCount := 1
			if role == "generic" || role == "same-leader" {
				if outcome.Status != DispatchCoalesced {
					t.Fatalf("compatible %s was not coalesced: %+v", role, outcome)
				}
				recordMentionEvaluation(t, f.issueID, f.agentID, f.taskID, squadID)
			} else {
				if outcome.Status != DispatchDeferred {
					t.Fatalf("incompatible %s consumed leader input: %+v", role, outcome)
				}
				after, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(f.taskID))
				if err != nil {
					t.Fatal(err)
				}
				if after.TriggerCommentID != before.TriggerCommentID || after.SquadID != before.SquadID || after.IsLeaderTask != before.IsLeaderTask {
					t.Fatalf("incompatible %s task was modified", role)
				}
				claim := claimTaskRaw(t, f.runtimeID, "")
				if claim.code != http.StatusOK || claim.task == nil {
					t.Fatalf("claim original %s turn: %d %s", role, claim.code, claim.body)
				}
				if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id = $1 AND status IN ('dispatched', 'running')`, f.taskID); n != 1 {
					t.Fatal("original task was not claimed")
				}
				if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id = $1 AND $2 = ANY(delivered_comment_ids)`, f.taskID, created.ID); n != 0 {
					t.Fatal("incompatible role acknowledged the leader's input")
				}
				dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running' WHERE id = $1`, f.taskID)
				completeTaskViaDaemon(t, f.taskID)
				var followupID string
				if err := testPool.QueryRow(context.Background(), `SELECT id FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task AND squad_id = $3 AND (trigger_comment_id = $4 OR $4 = ANY(coalesced_comment_ids))`, f.issueID, f.agentID, squadID, created.ID).Scan(&followupID); err != nil {
					t.Fatalf("deferred leader input was not replayed after %s: %v", role, err)
				}
				recordMentionEvaluation(t, f.issueID, f.agentID, followupID, squadID)
				wantTaskCount = 2
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, f.issueID); n != wantTaskCount {
				t.Fatalf("role-aware merge created %d tasks", n)
			}
		})
	}
}

func TestLeaderAgentMentionSteersOnlyMatchingLeaderRole(t *testing.T) {
	for _, all := range []bool{false, true} {
		for _, role := range []string{"worker", "matching-leader", "different-squad-leader"} {
			t.Run(role+map[bool]string{false: "/chosen", true: "/all"}[all], func(t *testing.T) {
				f := newSupplementFixture(t, "claude", "running", true)
				squadID := assignMentionFixtureSquad(t, f)
				taskSquad := squadID
				if role == "different-squad-leader" {
					taskSquad = dbfx.Squad(t, "Other steering context", f.agentID)
				}
				dbfx.Exec(t, `UPDATE agent_task_queue SET is_leader_task = $2, squad_id = $3 WHERE id = $1`, f.taskID, role != "worker", taskSquad)
				body := map[string]any{"content": agentMention("Leader", f.agentID) + " evaluate this", "parent_id": f.triggerID}
				if all {
					body["steer"] = true
				} else {
					body["steer_task_ids"] = []string{f.taskID}
				}
				var created CommentResponse
				testutil.Call(t, testHandler.CreateComment, withURLParam(newRequest(http.MethodPost, "/api/issues/"+f.issueID+"/comments", body), "id", f.issueID)).Want(http.StatusCreated).JSON(&created)
				outcome := findCommentOutcome(t, created.TriggerOutcomes, f.agentID)
				if role == "matching-leader" {
					if outcome.Status != DispatchSteered || len(created.Supplements) != 1 {
						t.Fatalf("matching leader did not receive steering: %+v", created)
					}
					recordMentionEvaluation(t, f.issueID, f.agentID, f.taskID, squadID)
				} else {
					if outcome.Status != DispatchQueued || len(created.Supplements) != 0 {
						t.Fatalf("incompatible %s steering did not fall back to leader routing: %+v", role, created)
					}
					var taskID string
					if err := testPool.QueryRow(context.Background(), `SELECT id FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task AND squad_id = $3`, f.issueID, f.agentID, squadID).Scan(&taskID); err != nil {
						t.Fatal(err)
					}
					recordMentionEvaluation(t, f.issueID, f.agentID, taskID, squadID)
				}
			})
		}
	}
}
