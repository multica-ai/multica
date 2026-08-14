package qianwen

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	taskservice "github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This suite is the live-Postgres vertical slice for the Qianwen request
// bridge. It intentionally uses the real session engine, TaskService
// transaction, generated queries, installation config, and Service. CI and
// developer machines without Postgres (or without migrations 319-320) report a
// clear SKIP rather than treating an unexecuted database test as green.

type qianwenServiceDBFixture struct {
	pool         *pgxpool.Pool
	queries      *db.Queries
	service      *Service
	workspaceID  pgtype.UUID
	userID       pgtype.UUID
	runtimeID    pgtype.UUID
	agentID      pgtype.UUID
	installation InstallationResult
}

type qianwenSubmitAttempt struct {
	result SubmitResult
	err    error
}

type qianwenFinalizeBarrier struct {
	reached     chan struct{}
	release     chan struct{}
	reachedOnce sync.Once
	releaseOnce sync.Once
}

func newQianwenFinalizeBarrier(t *testing.T) *qianwenFinalizeBarrier {
	t.Helper()

	barrier := &qianwenFinalizeBarrier{
		reached: make(chan struct{}),
		release: make(chan struct{}),
	}
	t.Cleanup(barrier.open)
	return barrier
}

func (b *qianwenFinalizeBarrier) wait(t *testing.T) {
	t.Helper()

	select {
	case <-b.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("Submit did not reach the pre-finalize barrier")
	}
}

func (b *qianwenFinalizeBarrier) open() {
	b.releaseOnce.Do(func() { close(b.release) })
}

type qianwenFinalizeBarrierSender struct {
	delegate ChannelTaskSender
	barrier  *qianwenFinalizeBarrier
}

func (s *qianwenFinalizeBarrierSender) SendChannelDirectChatMessage(
	ctx context.Context,
	session db.ChatSession,
	agent db.Agent,
	initiatorUserID pgtype.UUID,
	content string,
	finalize taskservice.ChannelDirectChatFinalize,
) (*taskservice.DirectChatSendResult, error) {
	return s.delegate.SendChannelDirectChatMessage(ctx, session, agent, initiatorUserID, content,
		func(finalizeCtx context.Context, qtx *db.Queries, task db.AgentTaskQueue) error {
			s.barrier.reachedOnce.Do(func() { close(s.barrier.reached) })
			select {
			case <-s.barrier.release:
			case <-finalizeCtx.Done():
				return finalizeCtx.Err()
			}
			return finalize(finalizeCtx, qtx, task)
		})
}

func newQianwenServiceDBPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Skipf("Qianwen DB vertical slice skipped: database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Qianwen DB vertical slice skipped: database unreachable: %v", err)
	}

	var tableReady, indexReady bool
	if err := pool.QueryRow(ctx, `
		SELECT
			to_regclass('public.qianwen_skill_request') IS NOT NULL,
			to_regclass('public.idx_qianwen_skill_request_installation_request') IS NOT NULL
	`).Scan(&tableReady, &indexReady); err != nil {
		pool.Close()
		t.Fatalf("check Qianwen ledger migration: %v", err)
	}
	if !tableReady || !indexReady {
		pool.Close()
		t.Skipf("Qianwen DB vertical slice skipped: migrations 319-320 are not applied (table=%v unique_index=%v)", tableReady, indexReady)
	}

	t.Cleanup(pool.Close)
	return pool
}

func newQianwenServiceDBFixture(t *testing.T) *qianwenServiceDBFixture {
	t.Helper()

	pool := newQianwenServiceDBPool(t)
	queries := db.New(pool)
	suffix := uuid.NewString()
	fixture := &qianwenServiceDBFixture{
		pool:        pool,
		queries:     queries,
		workspaceID: util.MustParseUUID(uuid.NewString()),
		userID:      util.MustParseUUID(uuid.NewString()),
		runtimeID:   util.MustParseUUID(uuid.NewString()),
		agentID:     util.MustParseUUID(uuid.NewString()),
	}
	t.Cleanup(func() { fixture.cleanup(t) })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `
		INSERT INTO "user" (id, name, email)
		VALUES ($1, 'Qianwen DB Test User', $2)
	`, fixture.userID, "qianwen-db-"+suffix+"@multica.test"); err != nil {
		t.Fatalf("seed Qianwen user: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspace (id, name, slug)
		VALUES ($1, 'Qianwen DB Test Workspace', $2)
	`, fixture.workspaceID, "qianwen-db-"+suffix); err != nil {
		t.Fatalf("seed Qianwen workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, fixture.workspaceID, fixture.userID); err != nil {
		t.Fatalf("seed Qianwen workspace member: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_runtime (
			id, workspace_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id
		)
		VALUES ($1, $2, 'Qianwen DB Runtime', 'cloud', 'codex', 'online', '', '{}'::jsonb, $3)
	`, fixture.runtimeID, fixture.workspaceID, fixture.userID); err != nil {
		t.Fatalf("seed Qianwen runtime: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent (
			id, workspace_id, name, runtime_mode, runtime_config, runtime_id,
			visibility, max_concurrent_tasks, owner_id, instructions,
			custom_env, custom_args
		)
		VALUES (
			$1, $2, 'Qianwen DB Agent', 'cloud', '{}'::jsonb, $3,
			'workspace', 1, $4, '', '{}'::jsonb, '[]'::jsonb
		)
	`, fixture.agentID, fixture.workspaceID, fixture.runtimeID, fixture.userID); err != nil {
		t.Fatalf("seed Qianwen agent: %v", err)
	}

	sessions := engine.NewChatSession(queries, pool, TypeQianwen, engine.SessionTitles{
		Direct:   "Qianwen glasses request",
		Fallback: "Qianwen glasses request",
	})
	tasks := &taskservice.TaskService{Queries: queries, TxStarter: pool, Bus: events.New()}
	service, err := NewService(queries, sessions, tasks)
	if err != nil {
		t.Fatalf("construct Qianwen service: %v", err)
	}
	fixture.service = service
	fixture.installation, err = service.InstallPersonal(ctx, fixture.workspaceID, fixture.agentID, fixture.userID)
	if err != nil {
		t.Fatalf("install Qianwen personal bridge: %v", err)
	}
	if fixture.installation.Installation.ChannelType != string(TypeQianwen) ||
		fixture.installation.Installation.Status != "active" ||
		!verifyAccessToken(fixture.installation.Installation.Config, fixture.installation.AccessToken) {
		t.Fatalf("persisted Qianwen installation/config is not usable: %+v", fixture.installation.Installation)
	}
	return fixture
}

func (f *qianwenServiceDBFixture) cleanup(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	statements := []struct {
		name string
		sql  string
		arg  any
	}{
		{
			name: "qianwen request ledger",
			sql: `DELETE FROM qianwen_skill_request
				WHERE installation_id IN (
					SELECT id FROM channel_installation
					WHERE workspace_id = $1 AND channel_type = 'qianwen'
				)`,
			arg: f.workspaceID,
		},
		{name: "chat messages", sql: `DELETE FROM chat_message WHERE chat_session_id IN (SELECT id FROM chat_session WHERE workspace_id = $1)`, arg: f.workspaceID},
		{name: "agent tasks", sql: `DELETE FROM agent_task_queue WHERE agent_id = $1`, arg: f.agentID},
		{name: "channel bindings", sql: `DELETE FROM channel_chat_session_binding WHERE installation_id IN (SELECT id FROM channel_installation WHERE workspace_id = $1)`, arg: f.workspaceID},
		{name: "chat sessions", sql: `DELETE FROM chat_session WHERE workspace_id = $1`, arg: f.workspaceID},
		{name: "channel installation", sql: `DELETE FROM channel_installation WHERE workspace_id = $1`, arg: f.workspaceID},
		{name: "agent", sql: `DELETE FROM agent WHERE id = $1`, arg: f.agentID},
		{name: "runtime", sql: `DELETE FROM agent_runtime WHERE id = $1`, arg: f.runtimeID},
		{name: "member", sql: `DELETE FROM member WHERE workspace_id = $1`, arg: f.workspaceID},
		{name: "workspace", sql: `DELETE FROM workspace WHERE id = $1`, arg: f.workspaceID},
		{name: "user", sql: `DELETE FROM "user" WHERE id = $1`, arg: f.userID},
	}
	for _, statement := range statements {
		if _, err := f.pool.Exec(ctx, statement.sql, statement.arg); err != nil {
			t.Errorf("cleanup %s: %v", statement.name, err)
		}
	}
}

func (f *qianwenServiceDBFixture) submit(ctx context.Context, requestID, query string) (SubmitResult, error) {
	return f.service.Submit(ctx, f.installation.ConnectionID, f.installation.AccessToken, SubmitRequest{
		RequestID: requestID,
		Query:     query,
	})
}

func (f *qianwenServiceDBFixture) serviceStoppedBeforeFinalize(t *testing.T) (*Service, *qianwenFinalizeBarrier) {
	t.Helper()

	barrier := newQianwenFinalizeBarrier(t)
	service, err := newService(f.queries, f.service.sessions, &qianwenFinalizeBarrierSender{
		delegate: f.service.tasks,
		barrier:  barrier,
	})
	if err != nil {
		t.Fatalf("construct barrier Qianwen service: %v", err)
	}
	return service, barrier
}

func startQianwenSubmit(ctx context.Context, service *Service, connectionID, token, requestID, query string) <-chan qianwenSubmitAttempt {
	done := make(chan qianwenSubmitAttempt, 1)
	go func() {
		result, err := service.Submit(ctx, connectionID, token, SubmitRequest{
			RequestID: requestID,
			Query:     query,
		})
		done <- qianwenSubmitAttempt{result: result, err: err}
	}()
	return done
}

func awaitQianwenSubmit(t *testing.T, done <-chan qianwenSubmitAttempt) qianwenSubmitAttempt {
	t.Helper()

	select {
	case attempt := <-done:
		return attempt
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for Qianwen Submit")
		return qianwenSubmitAttempt{}
	}
}

func (f *qianwenServiceDBFixture) assertRequestUnpublished(t *testing.T, ctx context.Context, requestID, query string) db.QianwenSkillRequest {
	t.Helper()

	ledger, err := f.queries.GetQianwenRequest(ctx, db.GetQianwenRequestParams{
		InstallationID: f.installation.Installation.ID,
		RequestID:      util.MustParseUUID(requestID),
	})
	if err != nil {
		t.Fatalf("load unpublished request ledger: %v", err)
	}
	wantHash := sha256.Sum256([]byte(query))
	if !bytes.Equal(ledger.QuerySha256, wantHash[:]) {
		t.Fatalf("unpublished ledger query hash = %x, want %x", ledger.QuerySha256, wantHash)
	}
	if !ledger.ChatSessionID.Valid {
		t.Fatalf("unpublished ledger lacks its recoverable session: %+v", ledger)
	}
	if ledger.TaskID.Valid {
		t.Fatalf("unauthorized Submit published task_id %s", util.UUIDToString(ledger.TaskID))
	}
	if ledger.ClaimToken.Valid || ledger.ClaimExpiresAt.Valid {
		t.Fatalf("failed Submit retained its claim lease: token=%v expiry=%v", ledger.ClaimToken, ledger.ClaimExpiresAt)
	}

	var taskCount, messageCount int
	if err := f.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM agent_task_queue WHERE chat_session_id = $1),
			(SELECT count(*) FROM chat_message WHERE chat_session_id = $1)
	`, ledger.ChatSessionID).Scan(&taskCount, &messageCount); err != nil {
		t.Fatalf("count rolled-back request rows: %v", err)
	}
	if taskCount != 0 || messageCount != 0 {
		t.Fatalf("unauthorized Submit retained task/message rows: tasks=%d messages=%d", taskCount, messageCount)
	}
	return ledger
}

func waitForBlockedQianwenClaim(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE datname = current_database()
				  AND pid <> pg_backend_pid()
				  AND state = 'active'
				  AND wait_event_type = 'Lock'
				  AND query LIKE '%INSERT INTO qianwen_skill_request%'
			)
		`).Scan(&blocked); err != nil {
			t.Fatalf("observe blocked Qianwen claim: %v", err)
		}
		if blocked {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("Qianwen claim did not block behind the workspace teardown lock")
		}
	}
}

func (f *qianwenServiceDBFixture) hardDeleteChatLikeHandler(t *testing.T, ctx context.Context, sessionID pgtype.UUID) {
	t.Helper()

	session, err := f.queries.GetChatSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("load chat before hard delete: %v", err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin chat hard-delete transaction: %v", err)
	}
	defer tx.Rollback(ctx)
	qtx := f.queries.WithTx(tx)
	if _, err := qtx.LockChatSessionForDelete(ctx, session.ID); err != nil {
		t.Fatalf("lock chat for hard delete: %v", err)
	}
	if _, err := qtx.GetAgentForClaimUpdate(ctx, session.AgentID); err != nil {
		t.Fatalf("lock chat agent for hard delete: %v", err)
	}
	if _, err := qtx.CancelAgentTasksByChatSession(ctx, session.ID); err != nil {
		t.Fatalf("cancel chat tasks before hard delete: %v", err)
	}
	if err := qtx.DeleteChannelChatSessionBindingBySession(ctx, session.ID); err != nil {
		t.Fatalf("delete chat channel binding: %v", err)
	}
	if err := qtx.DeleteChannelOutboundCardMessagesBySession(ctx, session.ID); err != nil {
		t.Fatalf("delete chat outbound cards: %v", err)
	}
	if err := qtx.DeleteChatDraftRestoresBySession(ctx, session.ID); err != nil {
		t.Fatalf("delete chat draft restores: %v", err)
	}
	if err := qtx.DeleteAgentBuilderDraft(ctx, session.ID); err != nil {
		t.Fatalf("delete chat agent-builder draft: %v", err)
	}
	if err := qtx.DeleteChatSession(ctx, db.DeleteChatSessionParams{ID: session.ID, WorkspaceID: session.WorkspaceID}); err != nil {
		t.Fatalf("hard delete chat session: %v", err)
	}
	if err := qtx.DeleteAgentLabelAssignmentsByAgent(ctx, session.AgentID); err != nil {
		t.Fatalf("delete chat agent label assignments: %v", err)
	}
	if err := qtx.DeleteSystemAgentByID(ctx, session.AgentID); err != nil {
		t.Fatalf("delete builder chat agent: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit chat hard delete: %v", err)
	}
}

func (f *qianwenServiceDBFixture) assertPersistedRequest(t *testing.T, ctx context.Context, requestID, query string) db.QianwenSkillRequest {
	t.Helper()

	requestUUID := util.MustParseUUID(requestID)
	ledger, err := f.queries.GetQianwenRequest(ctx, db.GetQianwenRequestParams{
		InstallationID: f.installation.Installation.ID,
		RequestID:      requestUUID,
	})
	if err != nil {
		t.Fatalf("load request ledger: %v", err)
	}
	wantHash := sha256.Sum256([]byte(query))
	if !bytes.Equal(ledger.QuerySha256, wantHash[:]) {
		t.Fatalf("ledger query hash = %x, want %x", ledger.QuerySha256, wantHash)
	}
	if !ledger.ChatSessionID.Valid || !ledger.TaskID.Valid {
		t.Fatalf("ledger lacks durable session/task pointers: %+v", ledger)
	}
	if ledger.ClaimToken.Valid || ledger.ClaimExpiresAt.Valid {
		t.Fatalf("completed ledger retained its claim lease: token=%v expiry=%v", ledger.ClaimToken, ledger.ClaimExpiresAt)
	}

	var taskCount int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_task_queue
		WHERE chat_session_id = $1
		  AND regenerate_quick_actions_for IS NULL
	`, ledger.ChatSessionID).Scan(&taskCount); err != nil {
		t.Fatalf("count request tasks: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("request produced %d tasks, want exactly 1", taskCount)
	}

	var messageCount int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM chat_message
		WHERE chat_session_id = $1
		  AND task_id = $2
		  AND role = 'user'
		  AND channel_ingested
		  AND content = $3
	`, ledger.ChatSessionID, ledger.TaskID, query).Scan(&messageCount); err != nil {
		t.Fatalf("count channel-ingested input messages: %v", err)
	}
	if messageCount != 1 {
		t.Fatalf("request produced %d matching channel-ingested user messages, want exactly 1", messageCount)
	}

	var storedSessionID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `SELECT chat_session_id FROM agent_task_queue WHERE id = $1`, ledger.TaskID).Scan(&storedSessionID); err != nil {
		t.Fatalf("load ledger task: %v", err)
	}
	if !storedSessionID.Valid || storedSessionID.Bytes != ledger.ChatSessionID.Bytes {
		t.Fatalf("ledger task belongs to session %s, want %s", util.UUIDToString(storedSessionID), util.UUIDToString(ledger.ChatSessionID))
	}
	return ledger
}

func (f *qianwenServiceDBFixture) completeTaskWithAssistantOutput(t *testing.T, ctx context.Context, ledger db.QianwenSkillRequest, output string) {
	t.Helper()

	if _, err := f.pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'completed', completed_at = now()
		WHERE id = $1
	`, ledger.TaskID); err != nil {
		t.Fatalf("complete foreign request task: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO chat_message (
			chat_session_id, role, content, task_id, message_kind
		)
		VALUES ($1, 'assistant', $2, $3, 'message')
	`, ledger.ChatSessionID, output, ledger.TaskID); err != nil {
		t.Fatalf("insert foreign assistant output: %v", err)
	}
}

func TestQianwenServiceDBVerticalSlice(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)

	t.Run("first submit atomically persists one task message and ledger pointer", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		requestID := uuid.NewString()
		query := "check the current branch"

		result, err := fixture.submit(ctx, requestID, query)
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		if result.RequestID != requestID || result.Status != "accepted" {
			t.Fatalf("Submit result = %+v, want accepted request %s", result, requestID)
		}
		fixture.assertPersistedRequest(t, ctx, requestID, query)
	})

	t.Run("concurrent and repeated same payload creates one task", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		requestID := uuid.NewString()
		query := "run the focused parser tests"

		const workers = 12
		start := make(chan struct{})
		errs := make(chan error, workers)
		var wait sync.WaitGroup
		wait.Add(workers)
		for range workers {
			go func() {
				defer wait.Done()
				<-start
				result, err := fixture.submit(ctx, requestID, query)
				if err == nil && (result.RequestID != requestID || result.Status != "accepted") {
					err = fmt.Errorf("unexpected result: %+v", result)
				}
				errs <- err
			}()
		}
		close(start)
		wait.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("concurrent Submit: %v", err)
			}
		}

		fixture.assertPersistedRequest(t, ctx, requestID, query)
		if _, err := fixture.submit(ctx, requestID, query); err != nil {
			t.Fatalf("repeat Submit: %v", err)
		}
		fixture.assertPersistedRequest(t, ctx, requestID, query)
	})

	t.Run("same request id with different payload conflicts", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		requestID := uuid.NewString()
		query := "summarize the repository status"

		if _, err := fixture.submit(ctx, requestID, query); err != nil {
			t.Fatalf("seed Submit: %v", err)
		}
		if _, err := fixture.submit(ctx, requestID, query+" and modify it"); !errors.Is(err, ErrRequestConflict) {
			t.Fatalf("conflicting Submit error = %v, want ErrRequestConflict", err)
		}
		fixture.assertPersistedRequest(t, ctx, requestID, query)
	})

	t.Run("status and idempotency survive binding deletion", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		requestID := uuid.NewString()
		query := "inspect the pending task"

		if _, err := fixture.submit(ctx, requestID, query); err != nil {
			t.Fatalf("seed Submit: %v", err)
		}
		ledger := fixture.assertPersistedRequest(t, ctx, requestID, query)
		deleted, err := fixture.pool.Exec(ctx, `
			DELETE FROM channel_chat_session_binding
			WHERE installation_id = $1 AND channel_chat_id = $2
		`, fixture.installation.Installation.ID, requestID)
		if err != nil {
			t.Fatalf("delete channel binding: %v", err)
		}
		if deleted.RowsAffected() != 1 {
			t.Fatalf("deleted %d channel bindings, want 1", deleted.RowsAffected())
		}

		status, err := fixture.service.Status(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, requestID)
		if err != nil {
			t.Fatalf("Status after binding deletion: %v", err)
		}
		if status.TaskID != util.UUIDToString(ledger.TaskID) {
			t.Fatalf("Status task_id = %q, want ledger task %q", status.TaskID, util.UUIDToString(ledger.TaskID))
		}
		if _, err := fixture.submit(ctx, requestID, query); err != nil {
			t.Fatalf("repeat Submit after binding deletion: %v", err)
		}
		fixture.assertPersistedRequest(t, ctx, requestID, query)

		var bindingCount int
		if err := fixture.pool.QueryRow(ctx, `
			SELECT count(*) FROM channel_chat_session_binding
			WHERE installation_id = $1 AND channel_chat_id = $2
		`, fixture.installation.Installation.ID, requestID).Scan(&bindingCount); err != nil {
			t.Fatalf("count bindings after idempotent replay: %v", err)
		}
		if bindingCount != 0 {
			t.Fatalf("idempotent replay recreated %d bindings, want 0", bindingCount)
		}
	})

	t.Run("status rejects a ledger root task from another request session", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		requestID := uuid.NewString()
		foreignRequestID := uuid.NewString()
		if _, err := fixture.submit(ctx, requestID, "inspect only this request"); err != nil {
			t.Fatalf("submit protected request: %v", err)
		}
		protected := fixture.assertPersistedRequest(t, ctx, requestID, "inspect only this request")
		if _, err := fixture.submit(ctx, foreignRequestID, "produce foreign output"); err != nil {
			t.Fatalf("submit foreign request: %v", err)
		}
		foreign := fixture.assertPersistedRequest(t, ctx, foreignRequestID, "produce foreign output")
		const secret = "foreign assistant output must stay in its own session"
		fixture.completeTaskWithAssistantOutput(t, ctx, foreign, secret)

		updated, err := fixture.pool.Exec(ctx, `
			UPDATE qianwen_skill_request
			SET task_id = $1, updated_at = now()
			WHERE installation_id = $2 AND request_id = $3
		`, foreign.TaskID, fixture.installation.Installation.ID, util.MustParseUUID(requestID))
		if err != nil {
			t.Fatalf("forge ledger root task: %v", err)
		}
		if updated.RowsAffected() != 1 {
			t.Fatalf("forged %d ledger rows, want 1", updated.RowsAffected())
		}

		status, err := fixture.service.Status(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, requestID)
		if err != nil {
			t.Fatalf("Status with forged ledger root: %v", err)
		}
		if status.Output != "" || status.TaskID == util.UUIDToString(foreign.TaskID) {
			t.Fatalf("forged root exposed foreign session state: %+v (protected task %s)", status, util.UUIDToString(protected.TaskID))
		}
	})

	t.Run("status rejects a retry edge into another request session", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		requestID := uuid.NewString()
		foreignRequestID := uuid.NewString()
		if _, err := fixture.submit(ctx, requestID, "inspect the retry chain"); err != nil {
			t.Fatalf("submit protected request: %v", err)
		}
		protected := fixture.assertPersistedRequest(t, ctx, requestID, "inspect the retry chain")
		if _, err := fixture.submit(ctx, foreignRequestID, "produce unrelated retry output"); err != nil {
			t.Fatalf("submit foreign request: %v", err)
		}
		foreign := fixture.assertPersistedRequest(t, ctx, foreignRequestID, "produce unrelated retry output")
		const secret = "foreign retry output must stay in its own session"
		fixture.completeTaskWithAssistantOutput(t, ctx, foreign, secret)

		updated, err := fixture.pool.Exec(ctx, `
			UPDATE agent_task_queue AS child
			SET retry_of_task_id = $1,
			    attempt = parent.attempt + 1
			FROM agent_task_queue AS parent
			WHERE child.id = $2 AND parent.id = $1
		`, protected.TaskID, foreign.TaskID)
		if err != nil {
			t.Fatalf("forge cross-session retry edge: %v", err)
		}
		if updated.RowsAffected() != 1 {
			t.Fatalf("forged %d retry rows, want 1", updated.RowsAffected())
		}

		status, err := fixture.service.Status(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, requestID)
		if err != nil {
			t.Fatalf("Status with forged retry edge: %v", err)
		}
		if status.Output != "" || status.TaskID != util.UUIDToString(protected.TaskID) {
			t.Fatalf("cross-session retry affected protected status: %+v, want root task %s with no output", status, util.UUIDToString(protected.TaskID))
		}
	})
}

func TestQianwenServiceDBWorkspaceDeleteFencesNewClaim(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tx, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin workspace teardown: %v", err)
	}
	defer tx.Rollback(ctx)
	qtx := fixture.queries.WithTx(tx)
	if _, err := qtx.LockWorkspaceForDelete(ctx, fixture.workspaceID); err != nil {
		t.Fatalf("LockWorkspaceForDelete: %v", err)
	}

	requestID := uuid.NewString()
	done := startQianwenSubmit(ctx, fixture.service, fixture.installation.ConnectionID, fixture.installation.AccessToken, requestID, "run after workspace teardown")
	waitForBlockedQianwenClaim(t, fixture.pool)
	if err := qtx.DeleteWorkspace(ctx, fixture.workspaceID); err != nil {
		t.Fatalf("delete locked workspace: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit workspace teardown: %v", err)
	}

	attempt := awaitQianwenSubmit(t, done)
	if !errors.Is(attempt.err, ErrUnauthorized) {
		t.Fatalf("Submit after workspace teardown error = %v, want ErrUnauthorized", attempt.err)
	}
	var ledgerCount, taskCount, messageCount int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM qianwen_skill_request WHERE installation_id = $1),
			(SELECT count(*) FROM agent_task_queue WHERE agent_id = $2),
			(SELECT count(*) FROM chat_message WHERE chat_session_id IN (
				SELECT id FROM chat_session WHERE workspace_id = $3
			))
	`, fixture.installation.Installation.ID, fixture.agentID, fixture.workspaceID).Scan(&ledgerCount, &taskCount, &messageCount); err != nil {
		t.Fatalf("inspect rows after workspace teardown race: %v", err)
	}
	if ledgerCount != 0 || taskCount != 0 || messageCount != 0 {
		t.Fatalf("workspace teardown race left rows behind: ledger=%d tasks=%d messages=%d", ledgerCount, taskCount, messageCount)
	}
}

func TestQianwenServiceDBRevokeBeforeFinalizeRollsBackSubmit(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	service, barrier := fixture.serviceStoppedBeforeFinalize(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	requestID := uuid.NewString()
	query := "do not publish after revoke"

	done := startQianwenSubmit(ctx, service, fixture.installation.ConnectionID, fixture.installation.AccessToken, requestID, query)
	barrier.wait(t)
	if err := fixture.service.Revoke(ctx, fixture.installation.Installation.ID); err != nil {
		t.Fatalf("revoke installation before finalize: %v", err)
	}
	barrier.open()

	attempt := awaitQianwenSubmit(t, done)
	if !errors.Is(attempt.err, ErrUnauthorized) {
		t.Fatalf("Submit finalized after revoke with error %v, want ErrUnauthorized", attempt.err)
	}
	fixture.assertRequestUnpublished(t, ctx, requestID, query)
}

func TestQianwenServiceDBRotateBeforeFinalizeLetsNewCredentialRetry(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	service, barrier := fixture.serviceStoppedBeforeFinalize(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	requestID := uuid.NewString()
	query := "run once with the rotated credential"

	done := startQianwenSubmit(ctx, service, fixture.installation.ConnectionID, fixture.installation.AccessToken, requestID, query)
	barrier.wait(t)
	rotated, err := fixture.service.InstallPersonal(ctx, fixture.workspaceID, fixture.agentID, fixture.userID)
	if err != nil {
		t.Fatalf("rotate installation before finalize: %v", err)
	}
	if rotated.Installation.ID != fixture.installation.Installation.ID {
		t.Fatalf("rotation changed installation id from %s to %s", util.UUIDToString(fixture.installation.Installation.ID), util.UUIDToString(rotated.Installation.ID))
	}
	barrier.open()

	attempt := awaitQianwenSubmit(t, done)
	if !errors.Is(attempt.err, ErrUnauthorized) {
		t.Fatalf("old credential Submit after rotation error = %v, want ErrUnauthorized", attempt.err)
	}
	fixture.assertRequestUnpublished(t, ctx, requestID, query)
	result, err := fixture.service.Submit(ctx, rotated.ConnectionID, rotated.AccessToken, SubmitRequest{RequestID: requestID, Query: query})
	if err != nil {
		t.Fatalf("Submit same request with rotated credential: %v", err)
	}
	if result.RequestID != requestID || result.Status != "accepted" {
		t.Fatalf("rotated credential Submit result = %+v, want accepted request %s", result, requestID)
	}
	fixture.assertPersistedRequest(t, ctx, requestID, query)
}

func TestQianwenServiceDBMemberDeleteBeforeFinalizeRollsBackSubmit(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	service, barrier := fixture.serviceStoppedBeforeFinalize(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	requestID := uuid.NewString()
	query := "do not publish after member removal"

	member, err := fixture.queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		WorkspaceID: fixture.workspaceID,
		UserID:      fixture.userID,
	})
	if err != nil {
		t.Fatalf("load installer membership: %v", err)
	}
	done := startQianwenSubmit(ctx, service, fixture.installation.ConnectionID, fixture.installation.AccessToken, requestID, query)
	barrier.wait(t)
	if err := fixture.queries.DeleteMember(ctx, member.ID); err != nil {
		t.Fatalf("delete installer membership before finalize: %v", err)
	}
	barrier.open()

	attempt := awaitQianwenSubmit(t, done)
	if !errors.Is(attempt.err, ErrUnauthorized) {
		t.Fatalf("Submit finalized after member removal with error %v, want ErrUnauthorized", attempt.err)
	}
	fixture.assertRequestUnpublished(t, ctx, requestID, query)
}

func TestQianwenServiceDBStatusSurvivesChatHardDelete(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	requestID := uuid.NewString()
	query := "retain task status after deleting its chat"
	const output = "completed before the chat was deleted"

	if _, err := fixture.submit(ctx, requestID, query); err != nil {
		t.Fatalf("seed Submit: %v", err)
	}
	ledger := fixture.assertPersistedRequest(t, ctx, requestID, query)
	fixture.completeTaskWithAssistantOutput(t, ctx, ledger, output)
	before, err := fixture.service.Status(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, requestID)
	if err != nil {
		t.Fatalf("Status before chat hard delete: %v", err)
	}
	if before.Status != "completed" || before.TaskID != util.UUIDToString(ledger.TaskID) || before.Output != output {
		t.Fatalf("Status before chat hard delete = %+v", before)
	}

	fixture.hardDeleteChatLikeHandler(t, ctx, ledger.ChatSessionID)
	after, err := fixture.service.Status(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, requestID)
	if err != nil {
		t.Fatalf("Status after chat hard delete: %v", err)
	}
	if after.Status != "completed" || after.TaskID != util.UUIDToString(ledger.TaskID) || after.Output != "" {
		t.Fatalf("Status after chat hard delete = %+v, want completed task %s with empty output", after, util.UUIDToString(ledger.TaskID))
	}

	var tasksBeforeReplay int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE agent_id = $1`, fixture.agentID).Scan(&tasksBeforeReplay); err != nil {
		t.Fatalf("count tasks before idempotent replay: %v", err)
	}
	if _, err := fixture.submit(ctx, requestID, query); err != nil {
		t.Fatalf("repeat Submit after chat hard delete: %v", err)
	}
	var tasksAfterReplay int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE agent_id = $1`, fixture.agentID).Scan(&tasksAfterReplay); err != nil {
		t.Fatalf("count tasks after idempotent replay: %v", err)
	}
	if tasksAfterReplay != tasksBeforeReplay || tasksAfterReplay != 1 {
		t.Fatalf("chat hard-delete replay changed task count from %d to %d, want exactly 1", tasksBeforeReplay, tasksAfterReplay)
	}
}
