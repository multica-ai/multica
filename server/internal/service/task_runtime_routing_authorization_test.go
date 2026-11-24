package service

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A frozen personal route survives selection changes, but never authority changes.
func TestPersonalRuntimeLiveAuthorizationAcrossClaimSurfaces(t *testing.T) {
	sharedTestPool(t)
	cases := []struct {
		name, mutation string
		allowed        bool
	}{
		{name: "frozen personal runtime after default rebind", allowed: true},
		{name: "execution membership revoked", mutation: `DELETE FROM member WHERE user_id=(SELECT owner_id FROM agent_runtime WHERE id=$2) AND workspace_id=(SELECT workspace_id FROM agent WHERE id=$1)`},
		{name: "invocation permission revoked", mutation: `DELETE FROM agent_invocation_target WHERE agent_id=$1 AND $2::uuid IS NOT NULL`},
		{name: "runtime ownership transferred", mutation: `UPDATE agent_runtime SET owner_id=(SELECT owner_id FROM agent WHERE id=$1) WHERE id=$2`},
		{name: "runtime becomes ownerless", mutation: `UPDATE agent_runtime SET owner_id=NULL WHERE id=$2 AND $1::uuid IS NOT NULL`},
		{name: "agent archived", mutation: `UPDATE agent SET archived_at=now() WHERE id=$1 AND $2::uuid IS NOT NULL`},
		{name: "snapshot version unsupported", mutation: `UPDATE agent_task_queue SET runtime_routing=jsonb_set(runtime_routing,'{version}','2') WHERE agent_id=$1 AND runtime_id=$2`},
		{name: "snapshot execution user missing", mutation: `UPDATE agent_task_queue SET runtime_routing=runtime_routing-'execution_user_id' WHERE agent_id=$1 AND runtime_id=$2`},
		{name: "snapshot provider mismatch", mutation: `UPDATE agent_task_queue SET runtime_routing=jsonb_set(runtime_routing,ARRAY['routes',$1::text,'provider'],'"other-provider"') WHERE agent_id=$1 AND runtime_id=$2`},
		{name: "snapshot runtime mismatch", mutation: `UPDATE agent_task_queue SET runtime_routing=jsonb_set(runtime_routing,ARRAY['routes',$1::text,'runtime_id'],'"00000000-0000-0000-0000-000000000000"') WHERE agent_id=$1 AND runtime_id=$2`},
		{name: "snapshot owner mismatch", mutation: `UPDATE agent_task_queue SET runtime_routing=jsonb_set(runtime_routing,ARRAY['routes',$1::text,'runtime_owner_id'],'"00000000-0000-0000-0000-000000000000"') WHERE agent_id=$1 AND runtime_id=$2`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, surface := range []string{"singular candidates", "batch candidates", "fresh claim", "stale claim", "stale batch claim"} {
				t.Run(surface, func(t *testing.T) {
					f := newRuntimeClaimAccessFixture(t, "private", false, false, "queued")
					ctx := context.Background()
					exec := func(query string) {
						t.Helper()
						if _, err := f.pool.Exec(ctx, query, f.agentID, f.runtimeID); err != nil {
							t.Fatal(err)
						}
					}
					exec(`UPDATE agent SET permission_mode='public_to' WHERE id=$1 AND $2::uuid IS NOT NULL`)
					exec(`INSERT INTO agent_invocation_target(agent_id,target_type,target_id) SELECT id,'workspace',workspace_id FROM agent WHERE id=$1 AND $2::uuid IS NOT NULL`)
					exec(`UPDATE agent_task_queue t SET runtime_routing=jsonb_build_object('version',1,'execution_user_id',r.owner_id::text,'routes',jsonb_build_object(t.agent_id::text,jsonb_build_object('runtime_id',r.id::text,'runtime_owner_id',r.owner_id::text,'provider',r.provider,'source','personal'))) FROM agent_runtime r WHERE t.agent_id=$1 AND t.runtime_id=$2 AND r.id=t.runtime_id`)
					if tc.mutation != "" {
						exec(tc.mutation)
					}
					q := db.New(f.pool)
					want := 0
					if tc.allowed {
						want = 1
					}
					switch surface {
					case "singular candidates":
						rows, err := q.ListQueuedClaimCandidatesByRuntime(ctx, f.runtimeID)
						if err != nil || len(rows) != want {
							t.Fatalf("candidates=%d want=%d error=%v", len(rows), want, err)
						}
					case "batch candidates":
						rows, err := q.ListQueuedClaimCandidatesByRuntimes(ctx, []pgtype.UUID{f.runtimeID})
						if err != nil || len(rows) != want {
							t.Fatalf("candidates=%d want=%d error=%v", len(rows), want, err)
						}
					case "fresh claim":
						row, err := q.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{AgentID: f.agentID, RuntimeID: f.runtimeID, PrepareLeaseSecs: 60, RuntimeStaleSecs: RuntimeClaimFreshnessSeconds})
						assertRuntimeClaimOutcome(t, f, row, err, tc.allowed, "queued")
					case "stale claim":
						makeRuntimeClaimTaskStale(t, f)
						row, err := q.ReclaimStaleDispatchedTaskForRuntime(ctx, db.ReclaimStaleDispatchedTaskForRuntimeParams{RuntimeID: f.runtimeID, PrepareLeaseSecs: 60, RuntimeStaleSecs: RuntimeClaimFreshnessSeconds, ClaimRecoverySecs: 1})
						assertRuntimeClaimOutcome(t, f, row, err, tc.allowed, "dispatched")
					case "stale batch claim":
						makeRuntimeClaimTaskStale(t, f)
						rows, err := q.ReclaimStaleDispatchedTasksForRuntimes(ctx, db.ReclaimStaleDispatchedTasksForRuntimesParams{RuntimeIds: []pgtype.UUID{f.runtimeID}, PrepareLeaseSecs: 60, RuntimeStaleSecs: RuntimeClaimFreshnessSeconds, ClaimRecoverySecs: 1, MaxTasks: 1})
						if err != nil || len(rows) != want {
							t.Fatalf("claims=%d want=%d error=%v", len(rows), want, err)
						}
						if !tc.allowed {
							var status string
							if err := f.pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id=$1`, f.taskID).Scan(&status); err != nil || status != "dispatched" {
								t.Fatalf("denied claim mutated status=%q error=%v", status, err)
							}
						}
					}
				})
			}
		})
	}
}

func makeRuntimeClaimTaskStale(t *testing.T, f runtimeClaimAccessFixture) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `UPDATE agent_task_queue SET status='dispatched', dispatched_at=now()-interval '10 minutes', prepare_lease_expires_at=now()-interval '1 minute' WHERE id=$1`, f.taskID); err != nil {
		t.Fatal(err)
	}
}

func assertRuntimeClaimOutcome(t *testing.T, f runtimeClaimAccessFixture, row db.AgentTaskQueue, err error, allowed bool, originalStatus string) {
	t.Helper()
	if allowed {
		if err != nil || util.UUIDToString(row.ID) != f.taskID {
			t.Fatalf("claim task=%s error=%v", util.UUIDToString(row.ID), err)
		}
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("denied claim error=%v", err)
	}
	var status string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id=$1`, f.taskID).Scan(&status); err != nil || status != originalStatus {
		t.Fatalf("denied claim status=%q want=%q error=%v", status, originalStatus, err)
	}
}
