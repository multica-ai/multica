package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Use the shared test database and roll back every fixture and mutation.
func TestTaskRuntimeAllowedSQL(t *testing.T) {
	ctx := context.Background()
	tx, err := sharedTestPool(t).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func(query string, args ...any) string {
		t.Helper()
		var v string
		if err := tx.QueryRow(ctx, query, args...).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	owner := id(`INSERT INTO "user"(name,email) VALUES ('routing owner',gen_random_uuid()::text || '@test.invalid') RETURNING id`)
	executor := id(`INSERT INTO "user"(name,email) VALUES ('routing executor',gen_random_uuid()::text || '@test.invalid') RETURNING id`)
	ws := id(`INSERT INTO workspace(name,slug) VALUES ('routing test',gen_random_uuid()::text) RETURNING id`)
	exec(`INSERT INTO member(workspace_id,user_id,role) VALUES ($1,$2,'owner'),($1,$3,'member')`, ws, owner, executor)
	runtime := func(user string) string {
		return id(`INSERT INTO agent_runtime(workspace_id,name,runtime_mode,provider,owner_id,visibility,daemon_id,status,last_seen_at) VALUES ($1,'fake','local','codex',$2,'private',gen_random_uuid()::text,'online',now()) RETURNING id`, ws, user)
	}
	defaultRuntime, personalRuntime, nextDefault := runtime(owner), runtime(executor), runtime(owner)
	agent := id(`INSERT INTO agent(workspace_id,name,runtime_mode,owner_id,runtime_id,permission_mode) VALUES ($1,'routing source','local',$2,$3,'public_to') RETURNING id`, ws, owner, defaultRuntime)
	exec(`INSERT INTO agent_invocation_target(agent_id,target_type,target_id) VALUES ($1,'member',$2)`, agent, executor)
	snapshot := func(runtimeID, runtimeOwner, source string) []byte {
		b, err := json.Marshal(map[string]any{"version": 1, "execution_user_id": executor, "routes": map[string]any{agent: map[string]string{"runtime_id": runtimeID, "runtime_owner_id": runtimeOwner, "provider": "codex", "source": source}}})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	personal := snapshot(personalRuntime, executor, "personal")
	frozenDefault := snapshot(defaultRuntime, owner, "default")
	checks := []struct {
		name    string
		runtime string
		routing []byte
		mutate  string
		args    []any
		want    bool
	}{
		{name: "personal private runtime", runtime: personalRuntime, routing: personal, want: true},
		{name: "wrong snapshot owner", runtime: personalRuntime, routing: snapshot(personalRuntime, owner, "personal")},
		{name: "wrong selected runtime", runtime: defaultRuntime, routing: personal},
		{name: "foreign personal runtime", runtime: defaultRuntime, routing: snapshot(defaultRuntime, owner, "personal")},
		{name: "default private runtime owned by agent owner remains shared", runtime: defaultRuntime, routing: frozenDefault, want: true},
		{name: "default private runtime owned by another user is rejected", runtime: personalRuntime, routing: snapshot(personalRuntime, executor, "default")},
		{name: "default route invocation revoked", runtime: defaultRuntime, routing: frozenDefault, mutate: `DELETE FROM agent_invocation_target WHERE agent_id=$1`, args: []any{agent}},
		{name: "default route", runtime: defaultRuntime, routing: frozenDefault, mutate: `UPDATE agent_runtime SET visibility='public' WHERE id=$1`, args: []any{defaultRuntime}, want: true},
		{name: "default route survives default switch", runtime: defaultRuntime, routing: frozenDefault, mutate: `WITH publish_runtime AS (UPDATE agent_runtime SET visibility='public' WHERE id=$3) UPDATE agent SET runtime_id=$2 WHERE id=$1`, args: []any{agent, nextDefault, defaultRuntime}, want: true},
		{name: "legacy current default", runtime: defaultRuntime, want: true},
		{name: "legacy personal forbidden", runtime: personalRuntime},
		{name: "legacy default switch invalidates", runtime: defaultRuntime, mutate: `UPDATE agent SET runtime_id=$2 WHERE id=$1`, args: []any{agent, nextDefault}},
		{name: "invocation revoked", runtime: personalRuntime, routing: personal, mutate: `DELETE FROM agent_invocation_target WHERE agent_id=$1`, args: []any{agent}},
		{name: "membership revoked despite explicit target", runtime: personalRuntime, routing: personal, mutate: `DELETE FROM member WHERE workspace_id=$1 AND user_id=$2`, args: []any{ws, executor}},
		{name: "runtime owner changed", runtime: personalRuntime, routing: personal, mutate: `UPDATE agent_runtime SET owner_id=$2 WHERE id=$1`, args: []any{personalRuntime, owner}},
		{name: "runtime provider changed", runtime: personalRuntime, routing: personal, mutate: `UPDATE agent_runtime SET provider='claude' WHERE id=$1`, args: []any{personalRuntime}},
		{name: "source archived", runtime: personalRuntime, routing: personal, mutate: `UPDATE agent SET archived_at=now() WHERE id=$1`, args: []any{agent}},
		{name: "malformed snapshot", runtime: personalRuntime, routing: []byte(`{"version":1,"execution_user_id":"bad uuid","routes":{}}`)},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			exec("SAVEPOINT routing_case")
			defer exec("ROLLBACK TO SAVEPOINT routing_case")
			if check.mutate != "" {
				exec(check.mutate, check.args...)
			}
			var allowed bool
			if err := tx.QueryRow(ctx, `SELECT task_runtime_allowed($1::uuid,$2::uuid,$3::jsonb)`, agent, check.runtime, check.routing).Scan(&allowed); err != nil {
				t.Fatalf("runtime authorization unavailable: %v", err)
			}
			if allowed != check.want {
				t.Fatalf("allowed=%v, want %v", allowed, check.want)
			}
		})
	}

	t.Run("shared default runtime token uses execution user", func(t *testing.T) {
		exec("SAVEPOINT shared_default_token")
		defer exec("ROLLBACK TO SAVEPOINT shared_default_token")
		q := db.New(tx)
		exec(`UPDATE agent_runtime SET visibility='public' WHERE id=$1`, defaultRuntime)
		task, err := q.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
			ID: dbid.NewV7(), AgentID: util.MustParseUUID(agent), RuntimeID: util.MustParseUUID(defaultRuntime),
			RuntimeRouting: frozenDefault,
		})
		if err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE agent_task_queue SET status='running',started_at=now() WHERE id=$1`, task.ID)
		exec(`INSERT INTO task_token(token_hash,task_id,agent_id,workspace_id,user_id,expires_at) VALUES ('execution-user-token',$1,$2,$3,$4,now()+interval '1 hour'),('runtime-owner-token',$1,$2,$3,$5,now()+interval '1 hour')`, task.ID, agent, ws, executor, owner)
		if _, err := q.GetTaskTokenByHash(ctx, "execution-user-token"); err != nil {
			t.Fatalf("shared-runtime execution user token rejected: %v", err)
		}
		if _, err := q.GetTaskTokenByHash(ctx, "runtime-owner-token"); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("shared-runtime machine owner's token accepted: %v", err)
		}
	})

	t.Run("generated claim recovery token retry and history fences", func(t *testing.T) {
		exec("SAVEPOINT generated_queries")
		defer exec("ROLLBACK TO SAVEPOINT generated_queries")
		q := db.New(tx)
		agentUUID, runtimeUUID := util.MustParseUUID(agent), util.MustParseUUID(personalRuntime)
		// Audit attribution need not be the execution principal on descendants.
		auditUser := id(`INSERT INTO "user"(name,email) VALUES ('audit only',gen_random_uuid()::text || '@test.invalid') RETURNING id`)
		created, err := q.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{ID: dbid.NewV7(), AgentID: agentUUID, RuntimeID: runtimeUUID, Priority: 2, OriginatorUserID: util.MustParseUUID(auditUser), AccountableUserID: util.MustParseUUID(auditUser), RuntimeRouting: personal})
		if err != nil {
			t.Fatal(err)
		}
		candidates, err := q.ListQueuedClaimCandidatesByRuntime(ctx, runtimeUUID)
		if err != nil || len(candidates) != 1 || candidates[0].ID != created.ID {
			t.Fatalf("personal candidates=%v, err=%v", len(candidates), err)
		}
		batch, err := q.ListQueuedClaimCandidatesByRuntimes(ctx, []pgtype.UUID{runtimeUUID})
		if err != nil || len(batch) != 1 {
			t.Fatalf("personal batch candidates=%v, err=%v", len(batch), err)
		}
		claimed, err := q.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{AgentID: agentUUID, RuntimeID: runtimeUUID, PrepareLeaseSecs: 30, RuntimeStaleSecs: RuntimeClaimFreshnessSeconds})
		if err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE agent SET runtime_id=$2 WHERE id=$1`, agent, nextDefault)
		exec(`INSERT INTO task_token(token_hash,task_id,agent_id,workspace_id,user_id,expires_at) VALUES ('routing-token',$1,$2,$3,$4,now()+interval '1 hour')`, claimed.ID, agent, ws, executor)
		if _, err = q.GetTaskTokenByHash(ctx, "routing-token"); err != nil {
			t.Fatalf("routed token rejected: %v", err)
		}
		exec(`UPDATE agent_task_queue SET dispatched_at=now()-interval '1 hour',prepare_lease_expires_at=now()-interval '1 minute' WHERE id=$1`, claimed.ID)
		recovered, err := q.ReclaimStaleDispatchedTaskForRuntime(ctx, db.ReclaimStaleDispatchedTaskForRuntimeParams{RuntimeID: runtimeUUID, ClaimRecoverySecs: 1, PrepareLeaseSecs: 30, RuntimeStaleSecs: RuntimeClaimFreshnessSeconds})
		if err != nil || recovered.ID != created.ID {
			t.Fatalf("routed recovery failed: %v", err)
		}
		exec(`UPDATE agent_task_queue SET dispatched_at=now()-interval '1 hour',prepare_lease_expires_at=now()-interval '1 minute' WHERE id=$1`, claimed.ID)
		recoveredBatch, err := q.ReclaimStaleDispatchedTasksForRuntimes(ctx, db.ReclaimStaleDispatchedTasksForRuntimesParams{RuntimeIds: []pgtype.UUID{runtimeUUID}, MaxTasks: 1, ClaimRecoverySecs: 1, PrepareLeaseSecs: 30, RuntimeStaleSecs: RuntimeClaimFreshnessSeconds})
		if err != nil || len(recoveredBatch) != 1 {
			t.Fatalf("batch routed recovery failed: %v", err)
		}
		exec(`UPDATE agent_task_queue SET status='failed' WHERE id=$1`, claimed.ID)
		retry, err := q.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: claimed.ID, NewTaskID: dbid.NewV7()})
		if err != nil {
			t.Fatal(err)
		}
		var got, want any
		if err = json.Unmarshal(retry.RuntimeRouting, &got); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(personal, &want); err != nil {
			t.Fatal(err)
		}
		if retry.RuntimeID != runtimeUUID || !reflect.DeepEqual(got, want) {
			t.Fatal("retry changed runtime or snapshot")
		}
		exec(`DELETE FROM agent_invocation_target WHERE agent_id=$1`, agent)
		candidates, err = q.ListQueuedClaimCandidatesByRuntime(ctx, runtimeUUID)
		if err != nil || len(candidates) != 0 {
			t.Fatalf("revoked route remains candidate: %v", err)
		}
		if _, err = q.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{AgentID: agentUUID, RuntimeID: runtimeUUID, PrepareLeaseSecs: 30, RuntimeStaleSecs: RuntimeClaimFreshnessSeconds}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("revoked route claim error=%v", err)
		}

		exec(`UPDATE agent_task_queue SET status='deferred',fire_at=now()+interval '1 hour' WHERE id=$1`, retry.ID)
		cancelled, err := q.CancelAgentTasksByRuntimeOrAgent(ctx, db.CancelAgentTasksByRuntimeOrAgentParams{RuntimeIds: []pgtype.UUID{runtimeUUID}})
		if err != nil || len(cancelled) != 1 || cancelled[0].ID != retry.ID {
			t.Fatalf("deferred routed task not cancelled before runtime deletion: count=%d, err=%v", len(cancelled), err)
		}

		if _, err = q.UpsertAgentRuntimePreference(ctx, db.UpsertAgentRuntimePreferenceParams{WorkspaceID: util.MustParseUUID(ws), UserID: util.MustParseUUID(executor), AgentID: agentUUID, RuntimeID: runtimeUUID, ModelMode: "runtime_default"}); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE agent_task_queue SET completed_at=now() WHERE agent_id=$1 AND status IN ('failed','cancelled')`, agent)
		exec(`UPDATE agent SET runtime_id=$2,archived_at=now() WHERE id=$1`, agent, personalRuntime)
		if n, err := q.UnbindTasksFromRuntime(ctx, runtimeUUID); err != nil || n != 2 {
			t.Fatalf("detach routed history count=%d, err=%v", n, err)
		}
		if rows, err := q.UnbindUserAgentsFromRuntime(ctx, runtimeUUID); err != nil || len(rows) != 1 {
			t.Fatalf("preserve routed source count=%d, err=%v", len(rows), err)
		}
		if err = q.DeleteAgentRuntime(ctx, runtimeUUID); err != nil {
			t.Fatal(err)
		}
		history, err := q.GetAgentTask(ctx, created.ID)
		if err != nil || history.RuntimeID.Valid || len(history.RuntimeRouting) == 0 {
			t.Fatalf("routed history missing after runtime deletion: %v", err)
		}
		preference, err := q.GetAgentRuntimePreference(ctx, db.GetAgentRuntimePreferenceParams{WorkspaceID: util.MustParseUUID(ws), UserID: util.MustParseUUID(executor), AgentID: agentUUID})
		if err != nil || preference.RuntimeID != runtimeUUID {
			t.Fatalf("deletion silently reset personal preference: %v", err)
		}
	})
}
