package wecom

// origin_gate_db_test.go — GetTaskChannelOrigin against real SQL.
//
// The origin gate used to be two calls with a Go `if` between them, and the
// fake in outbound_test.go could answer for both: a test could file a retry
// clone, watch which id the stamp was read for, and pin the whole rule without
// a database. Consolidating the gate into one query moved that rule into
// COALESCE(chat_input_task_id, id), where a fake cannot answer for it — it does
// not perform the join. So the rule is pinned here instead, against Postgres.
//
// The four rows below are the four shapes the classification has to keep apart,
// and each one is a case where getting it wrong costs something specific: a
// retry clone read off its own id answers "web UI" for a turn a room is waiting
// on; a task row that is gone read as "false" turns a lost reply into the most
// ordinary event in the deployment; a legacy row with no batch owner assumed
// channel-ingested reports every pre-MUL-4351 web turn as a channel turn whose
// route went missing.
//
// Skips when no migrated database is reachable, same as the other _db_ tests in
// this package.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// originLab is one workspace holding the task shapes the gate has to tell
// apart. Ids are the task ids a chat:done would carry.
type originLab struct {
	retryClone   string // owns nothing; inherits a parent whose batch IS stamped
	webUI        string // owns its batch; the batch carries no stamp
	channel      string // owns its batch; the batch IS stamped
	legacyPlain  string // chat_input_task_id NULL, own messages unstamped
	legacyInRoom string // chat_input_task_id NULL, own messages stamped
	legacyRetry  string // the auto-retry of legacyInRoom: NULL owner, owns nothing
	reaped       string // no agent_task_queue row at all
}

func seedOriginLab(t *testing.T, pool *pgxpool.Pool) originLab {
	t.Helper()
	ctx := context.Background()
	newID := func() string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
			t.Fatalf("seed: mint id: %v", err)
		}
		return id
	}
	tag := strings.ReplaceAll(newID(), "-", "")[:12]
	wsID, userID, agentID, sessionID := newID(), newID(), newID(), newID()
	lab := originLab{
		retryClone: newID(), webUI: newID(), channel: newID(),
		legacyPlain: newID(), legacyInRoom: newID(), legacyRetry: newID(), reaped: newID(),
	}
	parentID := newID() // the retry clone's batch owner

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %s: %v", strings.SplitN(strings.TrimSpace(sql), "\n", 2)[0], err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM chat_message WHERE chat_session_id = $1`, sessionID)
		_, _ = pool.Exec(ctx, `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, sessionID)
		_, _ = pool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, sessionID)
		_, _ = pool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_task_delivery WHERE task_id = ANY($1)`,
			[]string{lab.legacyInRoom, lab.legacyRetry})
		_, _ = pool.Exec(ctx, `DELETE FROM agent_runtime WHERE workspace_id = $1`, wsID)
		_, _ = pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	exec(`INSERT INTO workspace (id, name, slug) VALUES ($1, $2, $3)`,
		wsID, "origin "+tag, "origin-"+tag)
	exec(`INSERT INTO "user" (id, name, email) VALUES ($1, $2, $3)`,
		userID, "Origin "+tag, "origin-"+tag+"@example.com")
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode) VALUES ($1, $2, $3, 'local')`,
		agentID, wsID, "origin-agent-"+tag)
	exec(`INSERT INTO chat_session (id, workspace_id, agent_id, creator_id) VALUES ($1, $2, $3, $4)`,
		sessionID, wsID, agentID, userID)

	// completed_at is not decoration: agent_task_queue_active_requires_runtime
	// insists a row is either attached to a runtime or finished.
	task := func(id string, owner any) {
		t.Helper()
		exec(`INSERT INTO agent_task_queue (id, agent_id, chat_session_id, status, completed_at, chat_input_task_id)
		      VALUES ($1, $2, $3, 'completed', now(), $4)`, id, agentID, sessionID, owner)
	}
	msg := func(owner string, ingested bool) {
		t.Helper()
		exec(`INSERT INTO chat_message (chat_session_id, role, content, task_id, channel_ingested)
		      VALUES ($1, 'user', 'q', $2, $3)`, sessionID, owner, ingested)
	}

	// The retry chain: FailTask's child inherits chat_input_task_id, and the
	// user's message stays tagged with the parent that sealed the batch.
	task(parentID, parentID)
	task(lab.retryClone, parentID)
	msg(parentID, true)

	task(lab.webUI, lab.webUI)
	msg(lab.webUI, false)

	task(lab.channel, lab.channel)
	msg(lab.channel, true)

	// Migration 158 left BOTH legacy direct rows and channel tasks NULL here.
	// Which of the two a NULL row was is answerable only from its own messages.
	task(lab.legacyPlain, nil)
	msg(lab.legacyPlain, false)

	task(lab.legacyInRoom, nil)
	msg(lab.legacyInRoom, true)

	// THE SEVENTH SHAPE, built the way FailTask builds it rather than by hand.
	// CreateRetryTask copies chat_input_task_id verbatim — agent.sql says a
	// plain copy is deliberate, because legacy and channel parents carry NULL
	// and must stay NULL — so the clone of a NULL-owner parent owns nothing and
	// the verdict alone reads it as web UI. CopyChannelTaskDelivery then hands
	// it the parent's WeCom route anyway. A hand-built row could drift from
	// either; these cannot.
	// CreateRetryTask makes the clone ACTIVE, and
	// agent_task_queue_active_requires_runtime (migration 251) needs a runtime
	// on an active row. A real parent has one and the clone inherits it, so the
	// fixture gives the parent one rather than relaxing the constraint.
	runtimeID := newID()
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider)
	      VALUES ($1, $2, $3, 'local', 'claude_code')`,
		runtimeID, wsID, "origin-runtime-"+tag)
	exec(`UPDATE agent_task_queue SET runtime_id = $1 WHERE id = $2`, runtimeID, lab.legacyInRoom)

	q := db.New(pool)
	clone, err := q.CreateRetryTask(context.Background(), db.CreateRetryTaskParams{
		ID:        mustPgUUID(t, lab.legacyInRoom),
		NewTaskID: mustPgUUID(t, lab.legacyRetry),
	})
	if err != nil {
		t.Fatalf("CreateRetryTask: %v", err)
	}
	if clone.ChatInputTaskID.Valid {
		t.Fatalf("the clone inherited owner %v; this case only exists because it stays NULL",
			clone.ChatInputTaskID)
	}

	// The other half of what FailTask writes. The parent's route is seeded flat
	// because it is the inbound path's output, not the retry's; the clone's is
	// made by the query FailTask actually calls.
	exec(`INSERT INTO channel_task_delivery
	        (task_id, binding_id, installation_id, channel_type, channel_chat_id, chat_type, route_revision)
	      VALUES ($1, $2, $3, 'wecom', $4, 'group', 1)`,
		lab.legacyInRoom, newID(), newID(), "room-"+tag)
	if err := q.CopyChannelTaskDelivery(context.Background(), db.CopyChannelTaskDeliveryParams{
		ChildTaskID:  mustPgUUID(t, lab.legacyRetry),
		ParentTaskID: mustPgUUID(t, lab.legacyInRoom),
	}); err != nil {
		t.Fatalf("CopyChannelTaskDelivery: %v", err)
	}
	route, err := q.GetChannelTaskDelivery(context.Background(), mustPgUUID(t, lab.legacyRetry))
	if err != nil {
		t.Fatalf("the clone has no route: %v — without one it is not a row anyone is waiting on, "+
			"and the case stops being the one that regressed", err)
	}
	if route.ChannelChatID != "room-"+tag {
		t.Fatalf("the clone's route points at %q, want the parent's room", route.ChannelChatID)
	}

	return lab
}

func TestGetTaskChannelOrigin_RealSQL(t *testing.T) {
	pool := twoReplicaDB(t)
	lab := seedOriginLab(t, pool)
	q := db.New(pool)

	for _, tc := range []struct {
		name     string
		taskID   string
		ingested bool
		// ownerUnknown is the second fact the query reports. It is not a
		// detail: it is what lets the delivery branch fail open on a row whose
		// verdict cannot speak for it, and the row that regressed is exactly
		// one where ingested is false and this is true.
		ownerUnknown bool
		why          string
	}{
		{
			name: "a retry clone reaches its parent's batch", taskID: lab.retryClone, ingested: true,
			why: "the clone owns no messages of its own; read off its own id it answers 'web UI' " +
				"for a turn a room is waiting on, which is the retry half of MUL-4988",
		},
		{
			name: "a question typed in the web UI", taskID: lab.webUI, ingested: false,
			why: "the ordinary completion in the deployment, and the one that must not warn",
		},
		{
			name: "a question asked in the room", taskID: lab.channel, ingested: true,
			why: "the turn a missing delivery row is actionable for",
		},
		{
			name: "a legacy row with no batch owner, asked in Multica", taskID: lab.legacyPlain, ingested: false,
			ownerUnknown: true,
			why: "NULL chat_input_task_id is 'legacy row OR channel task' (migration 158). Reading " +
				"NULL as channel-ingested reports every pre-MUL-4351 web turn as a channel turn " +
				"whose route went missing",
		},
		{
			name: "a legacy row with no batch owner, asked in the room", taskID: lab.legacyInRoom, ingested: true,
			ownerUnknown: true,
			why: "COALESCE falls back to the task's own id, which is the same key migration 427 " +
				"used to decide this row was owed a delivery route",
		},
		{
			// THE SEVENTH SHAPE, and the one that regressed. Built through the
			// real CreateRetryTask and CopyChannelTaskDelivery so the fixture
			// cannot drift from what FailTask actually writes.
			name: "the auto-retry of a legacy row with no batch owner", taskID: lab.legacyRetry,
			ingested: false, ownerUnknown: true,
			why: "CreateRetryTask copies a NULL owner verbatim and the clone owns no messages, so " +
				"the verdict alone reads it as web UI — while CopyChannelTaskDelivery has already " +
				"given it the parent's WeCom route. batch_owner_unknown is what lets the delivery " +
				"branch keep failing open, which is how the reply a room is waiting on still leaves",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := q.GetTaskChannelOrigin(context.Background(), mustPgUUID(t, tc.taskID))
			if err != nil {
				t.Fatalf("GetTaskChannelOrigin: %v", err)
			}
			if got.ChannelIngested != tc.ingested {
				t.Errorf("channel_ingested = %v, want %v — %s", got.ChannelIngested, tc.ingested, tc.why)
			}
			if got.BatchOwnerUnknown != tc.ownerUnknown {
				t.Errorf("batch_owner_unknown = %v, want %v — it is what lets the delivery branch "+
					"fail open on a row the verdict cannot speak for", got.BatchOwnerUnknown, tc.ownerUnknown)
			}
		})
	}

	t.Run("a task row that is gone", func(t *testing.T) {
		_, err := q.GetTaskChannelOrigin(context.Background(), mustPgUUID(t, lab.reaped))
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("err = %v, want pgx.ErrNoRows — the absence of a verdict has to stay "+
				"distinguishable from a negative one, or a reply reaped mid-completion is filed "+
				"as the most ordinary event in the deployment", err)
		}
	})
}

func mustPgUUID(t *testing.T, id string) pgtype.UUID {
	t.Helper()
	var out pgtype.UUID
	if err := out.Scan(id); err != nil {
		t.Fatalf("parse uuid %q: %v", id, err)
	}
	return out
}
