package handler

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type contextLockTestStarter struct {
	pool          *pgxpool.Pool
	pauseAfter    string
	paused        chan struct{}
	release       chan struct{}
	observeBefore string
	observed      chan struct{}
}

func (s *contextLockTestStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &contextLockTestTx{Tx: tx, starter: s}, nil
}

type contextLockTestTx struct {
	pgx.Tx
	starter *contextLockTestStarter
}

func (tx *contextLockTestTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if tx.starter.observeBefore != "" && strings.Contains(query, tx.starter.observeBefore) {
		close(tx.starter.observed)
	}
	row := tx.Tx.QueryRow(ctx, query, args...)
	switch {
	case tx.starter.pauseAfter != "" && strings.Contains(query, tx.starter.pauseAfter):
		return &contextLockPauseRow{Row: row, paused: tx.starter.paused, release: tx.starter.release}
	default:
		return row
	}
}

type contextLockPauseRow struct {
	pgx.Row
	paused  chan struct{}
	release chan struct{}
}

func (row *contextLockPauseRow) Scan(dest ...any) error {
	if err := row.Row.Scan(dest...); err != nil {
		return err
	}
	close(row.paused)
	<-row.release
	return nil
}

// TestChannelContextLockOrder serializes the production enqueue and append
// paths at the exact lock edge that previously formed an ABBA cycle. Enqueue
// pauses while holding the target generation; a concurrent append then starts
// its binding lock. Both must complete after enqueue is released, proving that
// enqueue already owns binding before generation and append cannot invert it.
func TestChannelContextLockOrder(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler test fixture is required")
	}

	ctx := context.Background()
	agentID, _, _ := createRuntimeGuardAgent(t, ctx)
	queries := db.New(testPool)
	installationID := uuid.NewString()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO channel_installation (
			id, workspace_id, agent_id, channel_type, config, status, installer_user_id
		) VALUES ($1, $2, $3, 'slack', '{}'::jsonb, 'active', $4)
	`, installationID, testWorkspaceID, agentID, testUserID); err != nil {
		t.Fatalf("seed channel installation: %v", err)
	}

	session := engine.NewChatSession(queries, testPool, channel.Type("slack"), engine.SessionTitles{Direct: "Lock order test"})
	sessionID, err := session.EnsureSession(ctx, engine.EnsureSessionInput{
		WorkspaceID:    util.MustParseUUID(testWorkspaceID),
		AgentID:        util.MustParseUUID(agentID),
		InstallationID: util.MustParseUUID(installationID),
		Sender:         util.MustParseUUID(testUserID),
		BindingKey:     "lock-order-" + uuid.NewString(),
		ChatType:       channel.ChatTypeP2P,
	})
	if err != nil {
		t.Fatalf("ensure channel session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_chat_context_generation WHERE chat_session_id = $1`, sessionID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, sessionID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, installationID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})

	if err := session.MarkPendingFresh(ctx, sessionID, "new-boundary"); err != nil {
		t.Fatalf("mark pending fresh: %v", err)
	}
	initiator := util.MustParseUUID(testUserID)
	if _, err := session.AppendUserMessage(ctx, engine.AppendInput{
		SessionID: sessionID, Sender: initiator, InstallationID: util.MustParseUUID(installationID),
		Body: "first post-new message", MessageID: "first-" + uuid.NewString(),
	}); err != nil {
		t.Fatalf("append first post-new message: %v", err)
	}
	chatSession, err := queries.GetChatSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("load chat session: %v", err)
	}

	generationLocked := make(chan struct{})
	releaseEnqueue := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseEnqueue) }) }
	defer release()
	enqueueService := &service.TaskService{
		Queries: queries,
		TxStarter: &contextLockTestStarter{
			pool: testPool, pauseAfter: "LockChannelChatContextGenerationByRevision",
			paused: generationLocked, release: releaseEnqueue,
		},
		Bus: events.New(),
	}
	enqueueResult := make(chan error, 1)
	go func() {
		_, err := enqueueService.EnqueueChannelChatTask(ctx, chatSession, initiator, true, 2)
		enqueueResult <- err
	}()
	waitContextLockSignal(t, generationLocked, "enqueue did not lock generation")

	bindingAttempted := make(chan struct{})
	appendSession := engine.NewChatSession(queries, &contextLockTestStarter{
		pool: testPool, observeBefore: "LockChannelChatSessionBindingForContext", observed: bindingAttempted,
	}, channel.Type("slack"), engine.SessionTitles{Direct: "Lock order test"})
	appendResult := make(chan error, 1)
	go func() {
		_, err := appendSession.AppendUserMessage(ctx, engine.AppendInput{
			SessionID: sessionID, Sender: initiator, InstallationID: util.MustParseUUID(installationID),
			Body: "racing append", MessageID: "racing-" + uuid.NewString(),
		})
		appendResult <- err
	}()
	waitContextLockSignal(t, bindingAttempted, "append did not attempt binding lock")
	release()

	if err := waitContextLockResult(t, enqueueResult, "enqueue"); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if err := waitContextLockResult(t, appendResult, "append"); err != nil {
		t.Fatalf("append failed: %v", err)
	}

	var unowned int
	var revision pgtype.Int8
	if err := testPool.QueryRow(ctx, `
		SELECT count(*), max(channel_context_revision)
		FROM chat_message
		WHERE chat_session_id = $1 AND role = 'user' AND task_id IS NULL
	`, sessionID).Scan(&unowned, &revision); err != nil {
		t.Fatalf("inspect racing append: %v", err)
	}
	if unowned != 1 || !revision.Valid || revision.Int64 != 2 {
		t.Fatalf("racing append state = unowned:%d revision:%v, want 1/revision 2", unowned, revision)
	}
}

func waitContextLockSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(10 * time.Second):
		t.Fatal(failure)
	}
}

func waitContextLockResult(t *testing.T, result <-chan error, label string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not complete", label)
		return nil
	}
}
