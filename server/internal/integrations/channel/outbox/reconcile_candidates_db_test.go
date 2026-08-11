package outbox

// reconcile_candidates_db_test.go — which terminal tasks the compensating scan
// treats as an undelivered reply.
//
// DB-backed because the decision is the candidate query's WHERE clause. A fake
// would return whatever the fake was told to return.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newCandidateFixture seeds an active installation bound to one chat session
// and returns the ids tasks need to hang off it. agent_task_queue predates the
// repo's no-foreign-key rule and still has FKs to agent, agent_runtime and
// chat_session, so the whole chain has to be real.
func newCandidateFixture(t *testing.T) (context.Context, *db.Queries, *pgxpool.Pool, taskOwners) {
	t.Helper()
	pool := orderTestDB(t)
	ctx := context.Background()

	var o taskOwners
	if err := pool.QueryRow(ctx, `
WITH ws AS (
    INSERT INTO workspace (name, slug, description)
    VALUES ('outbox candidates', 'outbox-candidates-' || gen_random_uuid(), '')
    RETURNING id
), rt AS (
    INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider)
    SELECT id, 'outbox candidate runtime', 'local', 'multica_daemon' FROM ws
    RETURNING id, workspace_id
), ag AS (
    INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
    SELECT rt.workspace_id, 'outbox candidate agent', 'local', rt.id FROM rt
    RETURNING id, workspace_id
), usr AS (
    INSERT INTO "user" (email, name)
    VALUES ('outbox-candidates-' || gen_random_uuid() || '@example.test', 'outbox candidates')
    RETURNING id
), cs AS (
    INSERT INTO chat_session (workspace_id, agent_id, creator_id)
    SELECT ag.workspace_id, ag.id, usr.id FROM ag, usr
    RETURNING id
), inst AS (
    INSERT INTO channel_installation
        (workspace_id, agent_id, channel_type, config, installer_user_id, status)
    SELECT ag.workspace_id, ag.id, 'wecom',
           jsonb_build_object('bot_id', 'candidate_test'), usr.id, 'active'
    FROM ag, usr
    RETURNING id
), bind AS (
    INSERT INTO channel_chat_session_binding
        (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type)
    SELECT cs.id, inst.id, 'wecom', 'GROUP_1', 'group' FROM cs, inst
    RETURNING chat_session_id
)
SELECT ws.id, ag.id, rt.id, cs.id, inst.id FROM ws, ag, rt, cs, inst, bind`,
	).Scan(&o.workspaceID, &o.agentID, &o.runtimeID, &o.sessionID, &o.installationID); err != nil {
		t.Fatalf("seed owners: %v", err)
	}
	t.Cleanup(func() {
		// workspace cascades to runtime, agent, chat_session and their tasks.
		_, _ = pool.Exec(ctx, `DELETE FROM channel_chat_session_binding WHERE installation_id = $1`, o.installationID)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE id = $1`, o.installationID)
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, o.workspaceID)
	})
	return ctx, db.New(pool), pool, o
}

type taskOwners struct {
	workspaceID    pgtype.UUID
	agentID        pgtype.UUID
	runtimeID      pgtype.UUID
	sessionID      pgtype.UUID
	installationID pgtype.UUID
}

func insertTerminalTask(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	o taskOwners, status string, completedAt time.Time) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := pool.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, completed_at)
VALUES ($1, $2, $3, $4, 2, $5)
RETURNING id`, o.agentID, o.runtimeID, o.sessionID, status, completedAt).Scan(&id); err != nil {
		t.Fatalf("insert %s task: %v", status, err)
	}
	return id
}

// insertRetryOf clones the shape MaybeRetryFailedTask produces: a fresh task
// carrying retry_of_task_id back to the attempt it re-runs.
func insertRetryOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool, o taskOwners, parent pgtype.UUID) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, retry_of_task_id)
VALUES ($1, $2, $3, 'running', 2, $4)`, o.agentID, o.runtimeID, o.sessionID, parent); err != nil {
		t.Fatalf("insert retry clone: %v", err)
	}
}

func candidateIDs(t *testing.T, ctx context.Context, q *db.Queries, from, to time.Time) map[string]bool {
	t.Helper()
	rows, err := q.ListChannelOutboundReconcileCandidates(ctx, db.ListChannelOutboundReconcileCandidatesParams{
		ChannelType: "wecom",
		SourceKinds: []string{"chat_done", "task_failed"},
		WindowStart: pgtype.Timestamptz{Time: from, Valid: true},
		WindowEnd:   pgtype.Timestamptz{Time: to, Valid: true},
		Limit:       100,
	})
	if err != nil {
		t.Fatalf("list candidates: %v", err)
	}
	got := make(map[string]bool, len(rows))
	for _, r := range rows {
		got[util.UUIDToString(r.TaskID)] = true
	}
	return got
}

// A failed attempt that has already been retried is not an undelivered reply.
// The retry reports its own outcome, so treating the parent as a missed delivery
// prints "the agent could not handle that message" and then the retry's answer
// underneath it.
//
// The scan cannot read retry_pending — that lives only in the task:failed event
// payload — so it has to read the lineage column MaybeRetryFailedTask writes.
func TestReconcileCandidates_ARetriedFailureIsNotACandidate(t *testing.T) {
	ctx, q, pool, o := newCandidateFixture(t)
	at := time.Now().Add(-time.Minute)

	retried := insertTerminalTask(t, ctx, pool, o, "failed", at)
	insertRetryOf(t, ctx, pool, o, retried)
	terminal := insertTerminalTask(t, ctx, pool, o, "failed", at)
	answered := insertTerminalTask(t, ctx, pool, o, "completed", at)

	got := candidateIDs(t, ctx, q, at.Add(-time.Minute), time.Now())

	if got[util.UUIDToString(retried)] {
		t.Error("a failed attempt with a retry already created was offered as an undelivered reply: " +
			"the user gets a failure notice and then the retry's answer below it")
	}
	// The other two must survive, or the fix has bought silence instead of order.
	if !got[util.UUIDToString(terminal)] {
		t.Error("a genuinely terminal failure is no longer a candidate — the notice would never be delivered")
	}
	if !got[util.UUIDToString(answered)] {
		t.Error("a completed task stopped being a candidate")
	}
}
