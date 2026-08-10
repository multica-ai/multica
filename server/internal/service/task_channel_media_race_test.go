package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// raceInjectTxStarter wraps the pool so the transaction handed to
// EnqueueChatTask runs inject() immediately before the input-batch seal
// statement executes. That reproduces, deterministically, a channel media
// message whose append commits between the session-wide deadline read and
// LinkUnownedChannelChatMessagesToTask — the READ COMMITTED window where the
// seal sees a message the deadline read did not.
type raceInjectTxStarter struct {
	pool   *pgxpool.Pool
	inject func()
}

func (s *raceInjectTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &raceInjectTx{Tx: tx, inject: s.inject}, nil
}

type raceInjectTx struct {
	pgx.Tx
	inject func()
}

func (t *raceInjectTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "LinkUnownedChannelChatMessagesToTask") && t.inject != nil {
		t.inject()
		t.inject = nil
	}
	return t.Tx.Exec(ctx, sql, args...)
}

// TestEnqueueChatTaskDefersWhenMediaMessageCommitsDuringEnqueue pins the fix
// for the enqueue-vs-append race: a media-pending message sealed into the task
// after the deadline read must still leave the task deferred (not claimable)
// until its media binds or the persisted deadline expires.
func TestEnqueueChatTaskDefersWhenMediaMessageCommitsDuringEnqueue(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})
	// The message that armed this flush: plain text, no media marker.
	if _, err := pool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'user', 'look at this')`, chatSessionID); err != nil {
		t.Fatalf("seed text message: %v", err)
	}

	deadline := time.Now().Add(time.Minute)
	var mediaMessageID string
	svc := &TaskService{
		Queries: q,
		TxStarter: &raceInjectTxStarter{pool: pool, inject: func() {
			// A concurrent Handle appends an image message (with its media
			// marker) and commits — after the deadline read, before the seal.
			if err := pool.QueryRow(ctx, `
				INSERT INTO chat_message (chat_session_id, role, content, channel_media_pending_until)
				VALUES ($1, 'user', '[Image]', $2) RETURNING id`, chatSessionID, deadline).Scan(&mediaMessageID); err != nil {
				t.Errorf("inject media message: %v", err)
			}
		}},
		Bus: events.New(),
	}

	task, err := svc.EnqueueChatTask(ctx, db.ChatSession{
		ID:      util.MustParseUUID(chatSessionID),
		AgentID: util.MustParseUUID(agentID),
	}, util.MustParseUUID(userID), false)
	if err != nil {
		t.Fatalf("EnqueueChatTask: %v", err)
	}
	if mediaMessageID == "" {
		t.Fatal("race injection did not run")
	}

	// The injected message must be sealed into this task's input batch...
	var linkedTaskID pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT task_id FROM chat_message WHERE id = $1`, mediaMessageID).Scan(&linkedTaskID); err != nil {
		t.Fatalf("load sealed media message: %v", err)
	}
	if !linkedTaskID.Valid || linkedTaskID.Bytes != task.ID.Bytes {
		t.Fatalf("media message task_id = %s, want %s", util.UUIDToString(linkedTaskID), util.UUIDToString(task.ID))
	}
	// ...and therefore the task must not be claimable before the media binds.
	if task.Status != "deferred" || !task.FireAt.Valid {
		t.Fatalf("task = status %q fire_at %v, want deferred until the sealed media deadline", task.Status, task.FireAt)
	}
	if task.FireAt.Time.Before(deadline.Add(-time.Second)) || task.FireAt.Time.After(deadline.Add(time.Second)) {
		t.Fatalf("task fire_at = %v, want sealed media deadline %v", task.FireAt.Time, deadline)
	}

	// Media completion still promotes it through the normal path.
	if err := q.ClearChatMessageChannelMediaPending(ctx, db.ClearChatMessageChannelMediaPendingParams{
		ID:            util.MustParseUUID(mediaMessageID),
		ChatSessionID: util.MustParseUUID(chatSessionID),
	}); err != nil {
		t.Fatalf("clear pending media: %v", err)
	}
	if err := svc.PromoteChannelChatTasksIfMediaReady(ctx, util.MustParseUUID(chatSessionID)); err != nil {
		t.Fatalf("PromoteChannelChatTasksIfMediaReady: %v", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&status); err != nil {
		t.Fatalf("load promoted task: %v", err)
	}
	if status != "queued" {
		t.Fatalf("promoted task status = %q, want queued", status)
	}
}

// TestEnqueueChatTaskLocksOutAConcurrentArchiveButNotInboundMessages is the
// other half of the archive guard, and it is about the lock MODE rather than
// the status check.
//
// The handler-side test covers archive-then-flush: the archive has committed,
// so the re-read under the lock sees 'archived' and the enqueue refuses. This
// one covers the overlap — an archive arriving while an enqueue is mid-flight.
// It must not be able to slip past: if it committed inside our transaction its
// cancel would run against a task row that does not exist yet, and the enqueue
// would go on to commit that row onto a closed conversation. So the archive has
// to block on the lock, come through afterwards, and cancel what it then sees.
//
// The same transaction must NOT block the room's next message. Every inbound
// message is an INSERT into chat_message, FK'd to this chat_session, so it takes
// FOR KEY SHARE on the row we hold — and FOR UPDATE (what the send, delete and
// draft paths take) conflicts with exactly that. Under FOR UPDATE a group's
// next message would wait for the previous message's enqueue to finish. Both
// halves are asserted from inside the enqueue transaction, which is the only
// moment either is observable.
func TestEnqueueChatTaskLocksOutAConcurrentArchiveButNotInboundMessages(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, channel_ingested)
		VALUES ($1, 'user', 'first', TRUE)`, chatSessionID); err != nil {
		t.Fatalf("seed inbound message: %v", err)
	}

	// Both probes run on their own connections, from inside the enqueue
	// transaction, with a short lock_timeout: "blocked" and "not blocked" are
	// then a returned error rather than a wall-clock guess.
	var archiveErr, appendErr error
	svc := &TaskService{
		Queries: q,
		TxStarter: &raceInjectTxStarter{pool: pool, inject: func() {
			archiveErr = probeUnderLock(ctx, pool,
				`UPDATE chat_session SET status = 'archived' WHERE id = $1`, chatSessionID)
			appendErr = probeUnderLock(ctx, pool,
				`INSERT INTO chat_message (chat_session_id, role, content, channel_ingested)
				 VALUES ($1, 'user', 'second', TRUE)`, chatSessionID)
		}},
		Bus: events.New(),
	}

	task, err := svc.EnqueueChatTask(ctx, db.ChatSession{
		ID:      util.MustParseUUID(chatSessionID),
		AgentID: util.MustParseUUID(agentID),
	}, util.MustParseUUID(userID), false)
	if err != nil {
		t.Fatalf("EnqueueChatTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
	})

	if !isLockTimeout(archiveErr) {
		t.Fatalf("an archive landing mid-enqueue got %v, want to be blocked on the chat_session lock — it would otherwise commit while this task row is still invisible, cancel nothing, and leave a queued turn on a closed conversation", archiveErr)
	}
	if appendErr != nil {
		t.Fatalf("the room's next message could not be appended during an enqueue: %v — inbound ingestion must not wait behind the debounced flush of the message before it", appendErr)
	}
}

// probeUnderLock runs one statement on its own connection with a short
// lock_timeout, so a row lock held by the caller's transaction surfaces as
// SQLSTATE 55P03 instead of hanging the test. The probe always rolls back: it
// is asking whether it *could* proceed, not changing anything.
func probeUnderLock(ctx context.Context, pool *pgxpool.Pool, sql, chatSessionID string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '500ms'`); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, sql, chatSessionID)
	return err
}

// isLockTimeout reports the "I was blocked" answer: SQLSTATE 55P03,
// lock_not_available.
func isLockTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "55P03"
}

func TestDeferredChannelIssueTaskPromotesAfterMediaSettlement(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, assignee_type, assignee_id, creator_type, creator_id, number)
		VALUES ($1, 'Channel media', 'todo', 'none', 'agent', $2, 'member', $3, 880001)
		RETURNING id`, workspaceID, agentID, userID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	bus := events.New()
	queued := 0
	bus.Subscribe(protocol.EventTaskQueued, func(events.Event) { queued++ })
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: bus}
	deadline := time.Now().Add(time.Minute)
	task, err := svc.EnqueueDeferredChannelIssueTask(ctx, db.Issue{
		ID:           issueID,
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   util.MustParseUUID(agentID),
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		Priority:     "none",
	}, deadline)
	if err != nil {
		t.Fatalf("EnqueueDeferredChannelIssueTask: %v", err)
	}
	if task.Status != "deferred" || !task.FireAt.Valid {
		t.Fatalf("task = status %q fire_at %v, want deferred", task.Status, task.FireAt)
	}
	if queued != 0 {
		t.Fatalf("queued events before media settlement = %d, want 0", queued)
	}
	hasPending, err := q.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issueID,
		AgentID: util.MustParseUUID(agentID),
	})
	if err != nil || !hasPending {
		t.Fatalf("media-gated task pending check = %v, %v; want true, nil", hasPending, err)
	}
	hasActive, err := q.HasActiveTaskForIssueAndAgent(ctx, db.HasActiveTaskForIssueAndAgentParams{
		IssueID: issueID,
		AgentID: util.MustParseUUID(agentID),
	})
	if err != nil || !hasActive {
		t.Fatalf("media-gated task active check = %v, %v; want true, nil", hasActive, err)
	}
	var commentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'More context') RETURNING id`, issueID, workspaceID, userID).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	merged, err := q.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
		IssueID:                 issueID,
		AgentID:                 util.MustParseUUID(agentID),
		NewTriggerCommentID:     commentID,
		NewOriginatorUserID:     util.MustParseUUID(userID),
		NewAccountableUserID:    util.MustParseUUID(userID),
		NewOriginatorSource:     pgtype.Text{String: "direct_human", Valid: true},
		NewTriggerEvidenceKind:  pgtype.Text{String: "comment", Valid: true},
		NewTriggerEvidenceRefID: commentID,
	})
	if err != nil || merged.ID != task.ID {
		t.Fatalf("merge into media-gated task = %+v, %v; want task %s", merged, err, util.UUIDToString(task.ID))
	}

	if err := svc.PromoteDeferredChannelIssueTask(ctx, task.ID); err != nil {
		t.Fatalf("PromoteDeferredChannelIssueTask: %v", err)
	}
	if queued != 1 {
		t.Fatalf("queued events after promotion = %d, want 1", queued)
	}
	var status string
	var fireAt pgtype.Timestamptz
	if err := pool.QueryRow(ctx, `SELECT status, fire_at FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&status, &fireAt); err != nil {
		t.Fatalf("load promoted task: %v", err)
	}
	if status != "queued" || fireAt.Valid {
		t.Fatalf("promoted task = status %q fire_at %v, want queued with no deadline", status, fireAt)
	}
	if err := svc.PromoteDeferredChannelIssueTask(ctx, task.ID); err != nil {
		t.Fatalf("idempotent promotion: %v", err)
	}
	if queued != 1 {
		t.Fatalf("queued events after idempotent promotion = %d, want 1", queued)
	}
}

func TestDeferredChannelIssueTaskConflictsWithQueuedSiblingAtDatabase(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()

	var indexDefinition string
	if err := pool.QueryRow(ctx, `SELECT pg_get_indexdef('idx_one_pending_task_per_issue_agent_v2'::regclass)`).Scan(&indexDefinition); err != nil {
		t.Fatalf("load pending-task index: %v", err)
	}
	if !strings.Contains(indexDefinition, "channel_issue_media_pending") {
		t.Skip("channel-media pending-task uniqueness migration is not applied")
	}

	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	var issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, assignee_type, assignee_id, creator_type, creator_id, number)
		VALUES ($1, 'Channel media uniqueness', 'todo', 'none', 'agent', $2, 'member', $3, 880002)
		RETURNING id`, workspaceID, agentID, userID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := svc.EnqueueDeferredChannelIssueTask(ctx, db.Issue{
		ID:           issueID,
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   util.MustParseUUID(agentID),
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		Priority:     "none",
	}, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("EnqueueDeferredChannelIssueTask: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)`, task.AgentID, task.RuntimeID, issueID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != "idx_one_pending_task_per_issue_agent_v2" {
		t.Fatalf("queued sibling insert error = %v, want unique violation on pending-task index", err)
	}
}
