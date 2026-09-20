package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestPersonalExecutionSettings(t *testing.T) {
	for _, provider := range []string{"claude", "codex", "qwen"} {
		t.Run(provider, func(t *testing.T) {
			pool := sharedTestPool(t)
			ctx := context.Background()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := tx.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			id := func(sql string, args ...any) pgtype.UUID {
				t.Helper()
				var value pgtype.UUID
				if err := tx.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			a := id(`INSERT INTO "user"(name,email) VALUES ('A',gen_random_uuid()::text||'@settings.test') RETURNING id`)
			b := id(`INSERT INTO "user"(name,email) VALUES ('B',gen_random_uuid()::text||'@settings.test') RETURNING id`)
			ws := id(`INSERT INTO workspace(name,slug) VALUES ('execution',gen_random_uuid()::text) RETURNING id`)
			exec(`INSERT INTO member(workspace_id,user_id,role) VALUES ($1,$2,'owner'),($1,$3,'member')`, ws, a, b)
			runtime := func(user pgtype.UUID) pgtype.UUID {
				return id(`INSERT INTO agent_runtime(workspace_id,name,provider,runtime_mode,status,owner_id,visibility) VALUES ($1,'machine',$2,'local','online',$3,'private') RETURNING id`, ws, provider, user)
			}
			ra, rb, rb2 := runtime(a), runtime(b), runtime(b)
			aid := id(`INSERT INTO agent(workspace_id,name,runtime_mode,owner_id,runtime_id,permission_mode,max_concurrent_tasks,model) VALUES ($1,'shared','local',$2,$3,'public_to',1,'shared-model') RETURNING id`, ws, a, ra)
			exec(`INSERT INTO agent_invocation_target(agent_id,target_type,target_id) VALUES ($1,'workspace',$2)`, aid, ws)
			q := db.New(tx)
			svc := NewTaskService(q, nil, nil, events.New())
			preference := func(runtime pgtype.UUID, mode, model string, limit int32) {
				t.Helper()
				_, err := q.UpsertAgentRuntimePreference(ctx, db.UpsertAgentRuntimePreferenceParams{WorkspaceID: ws, UserID: b, AgentID: aid, RuntimeID: runtime, ModelMode: mode, Model: model, MaxConcurrentTasks: pgtype.Int4{Int32: limit, Valid: true}})
				if err != nil {
					t.Fatal(err)
				}
			}
			n := 0
			enqueue := func(user pgtype.UUID) db.AgentTaskQueue {
				t.Helper()
				n++
				iid := id(`INSERT INTO issue(workspace_id,title,status,priority,creator_id,creator_type,assignee_type,assignee_id,number,position) VALUES ($1,'task','todo','none',$2,'member','agent',$3,$4,0) RETURNING id`, ws, user, aid, n)
				issue, err := q.GetIssue(ctx, iid)
				if err != nil {
					t.Fatal(err)
				}
				task, err := svc.EnqueueTaskForIssue(ctx, issue)
				if err != nil {
					t.Fatal(err)
				}
				// now() is constant in this transaction. Give the fixture a strict
				// queue order before asserting which equal-priority task is claimed.
				exec(`UPDATE agent_task_queue SET created_at = now() + $2 * interval '1 second' WHERE id=$1`, task.ID, n)
				return task
			}
			claim := func(runtime pgtype.UUID, want *db.AgentTaskQueue) {
				t.Helper()
				task, err := svc.claimTask(ctx, aid, runtime)
				if err != nil {
					t.Fatal(err)
				}
				if want == nil {
					if task != nil {
						t.Fatalf("over capacity: %s", util.UUIDToString(task.ID))
					}
					return
				}
				if task == nil || task.ID != want.ID {
					t.Fatalf("claim got %v want %s", task, util.UUIDToString(want.ID))
				}
			}
			preference(rb, "custom", "b-model", 2)
			at := enqueue(a)
			b1 := enqueue(b)
			b2 := enqueue(b)
			b3 := enqueue(b)
			claim(ra, &at)
			claim(rb, &b1)
			claim(rb, &b2)
			claim(rb, nil)
			// Lowering capacity never cancels running work; the next start waits.
			preference(rb2, "runtime_default", "", 1)
			next := enqueue(b)
			claim(rb2, nil)
			exec(`UPDATE agent_task_queue SET status='completed' WHERE id=$1`, b1.ID)
			claim(rb2, nil)
			exec(`UPDATE agent_task_queue SET status='completed' WHERE id=$1`, b2.ID)
			claim(rb2, &next)
			claim(rb, nil)
			// Older queued work retains its machine/model, but observes the live limit.
			exec(`UPDATE agent_task_queue SET status='completed' WHERE id=$1`, next.ID)
			claim(rb, &b3)
			assertModel := func(task db.AgentTaskQueue, want string) {
				t.Helper()
				routing, err := ParseRuntimeRouting(task.RuntimeRouting)
				if err != nil {
					t.Fatal(err)
				}
				route, err := routing.Route(util.UUIDToString(aid))
				if err != nil || route.Model == nil || *route.Model != want {
					t.Fatalf("model not frozen: %s / %v", task.RuntimeRouting, err)
				}
			}
			assertModel(at, "shared-model")
			assertModel(b1, "b-model")
			assertModel(next, "")
			agent, _ := q.GetAgent(ctx, aid)
			child, err := svc.resolveTaskRuntime(ctx, q, agent, b, b1.ID)
			if err != nil {
				t.Fatal(err)
			}
			var rootJSON, childJSON any
			json.Unmarshal(b1.RuntimeRouting, &rootJSON)
			json.Unmarshal(child.Routing, &childJSON)
			if string(b1.RuntimeRouting) != string(child.Routing) {
				t.Fatalf("child changed frozen configuration: %v %v", rootJSON, childJSON)
			}
			// Personal execution does not inherit the shared model, even on the same provider.
			exec(`UPDATE agent SET model='shared-new' WHERE id=$1`, aid)
			preference(rb, "inherit", "", 1)
			inherited := enqueue(b)
			assertModel(inherited, "")
			assertModel(b1, "b-model")
			// Inheritance must never send another provider's model ID.
			other := "codex"
			if provider == "codex" {
				other = "qwen"
			}
			exec(`UPDATE agent_runtime SET provider=$2 WHERE id=$1`, rb2, other)
			preference(rb2, "inherit", "", 1)
			cross := enqueue(b)
			assertModel(cross, "")
			exec(`UPDATE agent_task_queue SET status='completed' WHERE id=$1`, b3.ID)
			claim(rb2, &cross)
		})
	}
}
