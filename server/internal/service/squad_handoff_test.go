package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

// A member run that fails on a direct-handoff squad's issue goes back to the
// squad leader in its leader role, not to the member that handed the work
// over; under leader dispatch the coordinator stays the recovery target.
func TestDelegatedFailureRecoveryGoesToDirectSquadLeader(t *testing.T) {
	for _, mode := range []string{SquadHandoffModeDirect, SquadHandoffModeLeader} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f, svc := seedDelegatedFailureFixture(t)
			var leaderID, squadID string
			if err := f.pool.QueryRow(ctx, `
				INSERT INTO agent (
					workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility,
					max_concurrent_tasks, owner_id, instructions, custom_env, custom_args
				)
				VALUES ($1, 'squad-leader', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
				RETURNING id`, f.workspaceID, f.runtimeID, f.userID).Scan(&leaderID); err != nil {
				t.Fatalf("seed leader: %v", err)
			}
			if err := f.pool.QueryRow(ctx, `
				INSERT INTO squad (workspace_id, name, description, leader_id, creator_id, handoff_mode)
				VALUES ($1, 'handoff squad', '', $2, $3, $4)
				RETURNING id`, f.workspaceID, leaderID, f.userID, mode).Scan(&squadID); err != nil {
				t.Fatalf("seed squad: %v", err)
			}
			t.Cleanup(func() {
				f.pool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1`, squadID)
				f.pool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
			})
			for _, member := range []string{f.coordinator, f.worker} {
				if _, err := f.pool.Exec(ctx, `INSERT INTO squad_member (squad_id, member_type, member_id) VALUES ($1, 'agent', $2)`, squadID, member); err != nil {
					t.Fatalf("seed squad member: %v", err)
				}
			}
			if _, err := f.pool.Exec(ctx, `UPDATE issue SET assignee_type = 'squad', assignee_id = $2 WHERE id = $1`, f.issueID, squadID); err != nil {
				t.Fatalf("assign squad: %v", err)
			}
			var failedID pgtype.UUID
			if err := f.pool.QueryRow(ctx, `
				INSERT INTO agent_task_queue (
					agent_id, runtime_id, issue_id, status, priority, attempt, max_attempts,
					originator_user_id, accountable_user_id, originator_source, delegated_from_task_id
				)
				VALUES ($1, $2, $3, 'failed', 0, 1, 1, $4, $4, 'delegation', $5)
				RETURNING id`, f.worker, f.runtimeID, f.issueID, f.userID, f.sourceTask).Scan(&failedID); err != nil {
				t.Fatalf("seed failed member task: %v", err)
			}

			target, created, err := svc.ensureDelegatedFailureRecoveryComment(ctx, failedID)
			if err != nil || target == nil || !created {
				t.Fatalf("recovery signal: created=%v err=%v", created, err)
			}
			wantAgent := f.coordinator
			if mode == SquadHandoffModeDirect {
				wantAgent = leaderID
			}
			if util.UUIDToString(target.agent.ID) != wantAgent || (target.leaderSquad != nil) != (mode == SquadHandoffModeDirect) {
				t.Fatalf("recovery target = %s (leader squad %v), want %s", util.UUIDToString(target.agent.ID), target.leaderSquad != nil, wantAgent)
			}

			var handoffs []json.RawMessage
			rows, err := f.pool.Query(ctx, `SELECT details FROM activity_log WHERE issue_id = $1 AND action = $2`, f.issueID, SquadHandoffAction)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var d json.RawMessage
				if err := rows.Scan(&d); err != nil {
					t.Fatal(err)
				}
				handoffs = append(handoffs, d)
			}
			rows.Close()
			if mode == SquadHandoffModeLeader {
				if len(handoffs) != 0 {
					t.Fatalf("leader mode must not record a handoff, got %s", handoffs)
				}
				return
			}
			var d map[string]string
			if len(handoffs) != 1 || json.Unmarshal(handoffs[0], &d) != nil || d["kind"] != "leader" || d["reason"] != "run_failed" || d["to_id"] != leaderID {
				t.Fatalf("failure handoff = %s", handoffs)
			}

			// The sweeper must judge the leader, not the member that handed
			// off: that member going away cannot strand the signal, and the
			// leader's queued run covers it.
			if _, err := f.pool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, f.coordinator); err != nil {
				t.Fatalf("archive handing-off member: %v", err)
			}
			if !recoveryPending(t, svc, target.comment.ID) {
				t.Fatal("recovery signal not pending for the leader after the handing-off member was archived")
			}

			if outcome, err := svc.dispatchDelegatedFailureRecovery(ctx, target, pgtype.UUID{}); err != nil || outcome != delegatedFailureRecoveryReplayed {
				t.Fatalf("dispatch = %v, %v", outcome, err)
			}
			var isLeader bool
			var taskSquad string
			if err := f.pool.QueryRow(ctx, `
				SELECT is_leader_task, squad_id::text FROM agent_task_queue
				WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`, f.issueID, leaderID).Scan(&isLeader, &taskSquad); err != nil {
				t.Fatalf("load leader recovery run: %v", err)
			}
			if !isLeader || taskSquad != squadID {
				t.Fatalf("leader recovery run is_leader_task=%v squad=%s, want true/%s", isLeader, taskSquad, squadID)
			}
			if recoveryPending(t, svc, target.comment.ID) {
				t.Fatal("recovery signal still pending while the leader's recovery run is queued")
			}
		})
	}
}

func recoveryPending(t *testing.T, svc *TaskService, commentID pgtype.UUID) bool {
	t.Helper()
	pending, err := svc.Queries.ListPendingDelegatedFailureRecoveries(context.Background(), 1000)
	if err != nil {
		t.Fatalf("ListPendingDelegatedFailureRecoveries: %v", err)
	}
	for _, c := range pending {
		if c.ID == commentID {
			return true
		}
	}
	return false
}
