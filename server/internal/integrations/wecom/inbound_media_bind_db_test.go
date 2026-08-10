package wecom

// inbound_media_bind_db_test.go — the other half of inbound media: what
// happens to a MediaRef AFTER the resolver has stored the object.
//
// media_ingest_test.go proves the download/decrypt/upload works. It cannot
// see this defect, because it calls ResolveMedia directly and never asks who
// consumes what it returns. The answer, on the shipped code, was nobody: the
// wecom sessionBinder's BindMedia returned nil without touching the session
// store, so a photo landed in object storage, its intent row stayed 'pending'
// forever, no attachment row was ever created, and the agent kept seeing the
// bare "[Image]" placeholder. Nothing was logged on that path — the Router
// only warns when BindMedia returns an error, and returning nil is how you
// say "bound fine".
//
// So this test drives the whole real path — engine.Router.Handle over the
// real wecom ResolverSet, the real engine.ChatSession, a real Postgres and a
// real HTTP download of really-encrypted bytes — and asks the database the
// one question the user asked: is there an attachment on that message?

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func mediaBindTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("no database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}
	var migrated bool
	if err := pool.QueryRow(ctx, `
		SELECT to_regclass('public.attachment') IS NOT NULL
		   AND to_regclass('public.channel_media_pending_object') IS NOT NULL`).Scan(&migrated); err != nil || !migrated {
		pool.Close()
		t.Skip("channel media tables not present (database not migrated)")
	}
	t.Cleanup(pool.Close)
	return pool
}

type mediaBindFixture struct {
	workspaceID    pgtype.UUID
	userID         pgtype.UUID
	agentID        pgtype.UUID
	installationID pgtype.UUID
	botID          string
	senderID       string
}

// seedMediaBindFixture builds exactly what the wecom ResolverSet resolves
// against: an installation keyed by bot id, and a user binding keyed by the
// anonymized aibot userid. Everything hangs off one workspace so cleanup is
// a single cascade.
func seedMediaBindFixture(t *testing.T, pool *pgxpool.Pool) mediaBindFixture {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	f := mediaBindFixture{
		botID:    fmt.Sprintf("wb-bind-%d", suffix),
		senderID: fmt.Sprintf("T-bind-%d", suffix),
	}
	var runtimeID pgtype.UUID

	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"wecom media bind", fmt.Sprintf("wecom-media-bind-%d@multica.test", suffix)).Scan(&f.userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		if f.workspaceID.Valid {
			_, _ = pool.Exec(cleanup, `DELETE FROM channel_media_pending_object WHERE workspace_id = $1`, f.workspaceID)
			_, _ = pool.Exec(cleanup, `DELETE FROM channel_inbound_message_dedup WHERE installation_id = $1`, f.installationID)
			_, _ = pool.Exec(cleanup, `DELETE FROM workspace WHERE id = $1`, f.workspaceID)
		}
		if f.userID.Valid {
			_, _ = pool.Exec(cleanup, `DELETE FROM "user" WHERE id = $1`, f.userID)
		}
	})
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description) VALUES ($1, $2, '') RETURNING id`,
		"wecom media bind", fmt.Sprintf("wecom-media-bind-%d", suffix)).Scan(&f.workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		f.workspaceID, f.userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, owner_id)
		VALUES ($1, $2, 'local', 'multica_daemon', $3) RETURNING id`,
		f.workspaceID, fmt.Sprintf("wecom-bind-runtime-%d", suffix), f.userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id, owner_id)
		VALUES ($1, $2, 'local', $3, $4) RETURNING id`,
		f.workspaceID, fmt.Sprintf("wecom-bind-agent-%d", suffix), runtimeID, f.userID).Scan(&f.agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
		VALUES ($1, $2, 'wecom',
		        jsonb_build_object('app_id', $3::text, 'bot_id', $3::text, 'app_secret_encrypted', ''),
		        $4, 'active')
		RETURNING id`, f.workspaceID, f.agentID, f.botID, f.userID).Scan(&f.installationID); err != nil {
		t.Fatalf("create installation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
		VALUES ($1, $2, $3, 'wecom', $4)`,
		f.workspaceID, f.userID, f.installationID, f.senderID); err != nil {
		t.Fatalf("create user binding: %v", err)
	}
	return f
}

// ---- the Router's collaborators, reduced to what this path touches ----

type bindTestIssues struct{}

func (bindTestIssues) Create(context.Context, service.IssueCreateParams, service.IssueCreateOpts) (service.IssueCreateResult, error) {
	return service.IssueCreateResult{}, errors.New("no /issue in this test")
}
func (bindTestIssues) PublishAttachmentsChanged(context.Context, db.Issue, pgtype.UUID) {}

// bindTestTasks stands in for the task service. Enqueueing a real chat task
// would need a live runtime, and the run trigger is not what is under test —
// but the media-ready promotion IS called on the media path, so it is
// recorded rather than ignored.
//
// The counter is guarded because the two sides sit on different goroutines:
// the Router calls the promotion from the detached media goroutine it spawns
// in enqueueMedia, while the test reads the count on its own. The database
// poll the test waits on is not a synchronisation edge between them — it
// observes the bind transaction, which commits one call EARLIER than this one
// in resolveAndBindMedia, so an unguarded read is both a data race and, on the
// wrong interleaving, a read of zero.
type bindTestTasks struct {
	mu       sync.Mutex
	promoted int
}

func (*bindTestTasks) EnqueueChatTask(context.Context, db.ChatSession, pgtype.UUID, bool) (db.AgentTaskQueue, error) {
	return db.AgentTaskQueue{}, nil
}
func (b *bindTestTasks) PromoteChannelChatTasksIfMediaReady(context.Context, pgtype.UUID) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.promoted++
	return nil
}
func (*bindTestTasks) PromoteDeferredChannelIssueTask(context.Context, pgtype.UUID) error {
	return nil
}

// promotions is the only way to read the counter from the test goroutine.
func (b *bindTestTasks) promotions() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.promoted
}

// wecomImageCallback builds the inbound envelope for a standalone image,
// through the real frame decoder so the fixture cannot drift from the wire.
func wecomImageCallback(t *testing.T, botID, senderID, msgID, url string) channel.InboundMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"msgid":    msgID,
		"aibotid":  botID,
		"chattype": "single",
		"chatid":   senderID,
		"from":     map[string]any{"userid": senderID},
		"msgtype":  "image",
		"image":    map[string]any{"url": url, "aeskey": testAESKey},
	})
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}
	var mc aibotMsgCallback
	if err := json.Unmarshal(raw, &mc); err != nil {
		t.Fatalf("decode callback: %v", err)
	}
	text, ok := mc.ownText()
	if !ok {
		t.Fatal("image callback is not routable; the fixture is wrong")
	}
	return channelMessageFromCallback(botID, "", mc, text, "req-bind-1")
}

// stalledCOSServer accepts the request and then holds it open, so the media
// path is genuinely still in flight while the durable message is inspected.
func stalledCOSServer(release <-chan struct{}) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
}

// TestInboundImageBecomesAnAttachmentOnTheChatMessage is the regression for
// "the image downloads fine and the agent still only sees [Image]".
//
// Every assertion here is a thing the user could check by hand in the
// database, and on the shipped code all four were false while the file sat
// happily in object storage.
func TestInboundImageBecomesAnAttachmentOnTheChatMessage(t *testing.T) {
	pool := mediaBindTestDB(t)
	fixture := seedMediaBindFixture(t, pool)
	ctx := context.Background()
	queries := db.New(pool)

	plaintext := []byte("\xff\xd8\xff\xe0 not really a jpeg, but really encrypted")
	srv := cosServer(t, plaintext, `attachment; filename="holiday.jpg"`)
	defer srv.Close()

	// The production wiring, with two seams swapped for test doubles that do
	// not change the path: object bytes go to memory instead of disk/S3, and
	// the HTTP client also permits loopback so the httptest server is
	// reachable. The intent ledger is the REAL database one — the bind
	// transaction claims those rows, so a fake would hide the claim.
	storage := &fakeMediaStorage{}
	resolver := newTestResolver(storage, engine.NewDBMediaIntentLedger(queries), nil)
	session := engine.NewChatSession(queries, pool, TypeWecom, engine.SessionTitles{
		Group: "群聊", Direct: "单聊", Fallback: "会话",
	})
	tasks := &bindTestTasks{}
	router := engine.NewRouter(bindTestIssues{}, tasks, queries, engine.RouterConfig{
		MediaTimeout: 20 * time.Second,
		Logger:       testLogger(),
	})
	router.Register(TypeWecom, NewResolverSet(NewStore(queries), session, nil, resolver))

	msgID := fmt.Sprintf("MSGID-BIND-%d", time.Now().UnixNano())
	if err := router.Handle(ctx, wecomImageCallback(t, fixture.botID, fixture.senderID, msgID, srv.URL)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// The durable message lands on the ACK path; media binding is detached,
	// so poll rather than sleep. Drain is not usable here: it cancels the
	// media context, which is the very thing being waited on.
	var chatMessageID, sessionID pgtype.UUID
	var body string
	var pendingUntil pgtype.Timestamptz
	deadline := time.Now().Add(15 * time.Second)
	var attachments int
	for {
		if err := pool.QueryRow(ctx, `
			SELECT cm.id, cm.chat_session_id, cm.content, cm.channel_media_pending_until
			FROM chat_message cm
			JOIN chat_session cs ON cs.id = cm.chat_session_id
			WHERE cs.workspace_id = $1 AND cm.role = 'user'
			ORDER BY cm.created_at DESC LIMIT 1`,
			fixture.workspaceID).Scan(&chatMessageID, &sessionID, &body, &pendingUntil); err != nil {
			if time.Now().After(deadline) {
				t.Fatalf("no durable chat_message was ever written: %v", err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM attachment WHERE chat_message_id = $1`, chatMessageID).Scan(&attachments); err != nil {
			t.Fatalf("count attachments: %v", err)
		}
		// Both, not just the attachment row: the promotion is the call AFTER
		// the bind transaction in resolveAndBindMedia, so an attachment row is
		// not evidence that the media goroutine has reached the end of its
		// work. Waiting on the row alone leaves assertion 4 reading a counter
		// that is still about to be incremented.
		if (attachments > 0 && tasks.promotions() > 0) || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 1. The resolver did its job — this is the precondition, not the point.
	// If it fails, the defect is upstream of the binder and the rest of this
	// test is meaningless.
	if stored := storage.stored(); len(stored) != 1 {
		t.Fatalf("objects in storage = %d, want 1 — the download/decrypt/upload failed, so this test is not exercising the binder", len(stored))
	}
	if body != "[Image]" {
		t.Errorf("durable body = %q, want the %q placeholder", body, "[Image]")
	}

	// 2. The attachment row: what the agent actually reads.
	if attachments != 1 {
		t.Fatalf("attachment rows on the chat message = %d, want 1 — the image was stored but never bound, so the agent only ever sees %q", attachments, body)
	}
	var filename, contentType, url string
	var sizeBytes int64
	var attSession, uploader pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT filename, content_type, url, size_bytes, chat_session_id, uploader_id
		FROM attachment WHERE chat_message_id = $1`, chatMessageID).
		Scan(&filename, &contentType, &url, &sizeBytes, &attSession, &uploader); err != nil {
		t.Fatalf("load attachment: %v", err)
	}
	if filename != "holiday.jpg" {
		t.Errorf("filename = %q, want holiday.jpg (from the download's Content-Disposition)", filename)
	}
	if contentType != "image/jpeg" {
		t.Errorf("content_type = %q, want image/jpeg", contentType)
	}
	if sizeBytes != int64(len(plaintext)) {
		t.Errorf("size_bytes = %d, want the decrypted length %d", sizeBytes, len(plaintext))
	}
	if url != storage.ObjectURL(storage.stored()[0].key) {
		t.Errorf("attachment url = %q, does not point at the object that was stored", url)
	}
	if attSession != sessionID {
		t.Errorf("attachment chat_session_id = %v, want the message's session %v", attSession, sessionID)
	}
	if uploader != fixture.userID {
		t.Errorf("uploader_id = %v, want the bound sender %v", uploader, fixture.userID)
	}

	// 3. The intent ledger settled. A row left 'pending' is the ledger saying
	// an object was uploaded and never claimed — the reconciler would
	// eventually delete the very file the user sent.
	var pending int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM channel_media_pending_object WHERE workspace_id = $1`,
		fixture.workspaceID).Scan(&pending); err != nil {
		t.Fatalf("count pending intents: %v", err)
	}
	if pending != 0 {
		t.Errorf("channel_media_pending_object rows = %d, want 0 — the bind never claimed the intent, so the reconciler will reclaim a live attachment", pending)
	}

	// 4. The pending marker is cleared, which is what releases the agent run.
	if pendingUntil.Valid {
		t.Errorf("channel_media_pending_until = %v, want NULL after binding", pendingUntil.Time)
	}
	if tasks.promotions() == 0 {
		t.Error("PromoteChannelChatTasksIfMediaReady was never called")
	}
}

// TestInboundImageMessageHoldsTheAgentRunUntilMediaLands covers the second
// half of the same wiring gap. The budget the Router computes has to reach
// the append, or the message row carries a NULL channel_media_pending_until,
// EnqueueChatTask reads FireAt = NULL, and the run starts immediately on the
// placeholder while the download is still in flight — the same user-visible
// symptom, arrived at a different way.
func TestInboundImageMessageHoldsTheAgentRunUntilMediaLands(t *testing.T) {
	pool := mediaBindTestDB(t)
	fixture := seedMediaBindFixture(t, pool)
	ctx := context.Background()
	queries := db.New(pool)

	// A server that never answers: the message must be durable, and marked
	// as awaiting media, long before the download resolves.
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })
	srv := stalledCOSServer(blocked)
	defer srv.Close()

	storage := &fakeMediaStorage{}
	resolver := newTestResolver(storage, engine.NewDBMediaIntentLedger(queries), nil)
	session := engine.NewChatSession(queries, pool, TypeWecom, engine.SessionTitles{})
	router := engine.NewRouter(bindTestIssues{}, &bindTestTasks{}, queries, engine.RouterConfig{
		MediaTimeout: 25 * time.Second,
		Logger:       testLogger(),
	})
	router.Register(TypeWecom, NewResolverSet(NewStore(queries), session, nil, resolver))

	msgID := fmt.Sprintf("MSGID-HOLD-%d", time.Now().UnixNano())
	if err := router.Handle(ctx, wecomImageCallback(t, fixture.botID, fixture.senderID, msgID, srv.URL)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	var pendingUntil pgtype.Timestamptz
	var content string
	if err := pool.QueryRow(ctx, `
		SELECT cm.channel_media_pending_until, cm.content
		FROM chat_message cm
		JOIN chat_session cs ON cs.id = cm.chat_session_id
		WHERE cs.workspace_id = $1 AND cm.role = 'user'
		ORDER BY cm.created_at DESC LIMIT 1`, fixture.workspaceID).Scan(&pendingUntil, &content); err != nil {
		t.Fatalf("load durable message: %v", err)
	}
	if content != "[Image]" {
		t.Fatalf("durable body = %q, want the [Image] placeholder", content)
	}
	if !pendingUntil.Valid {
		t.Fatal("channel_media_pending_until is NULL on a message carrying an image — the chat task will fire at once and hand the agent the placeholder while the download is still running")
	}
	if !pendingUntil.Time.After(time.Now()) {
		t.Errorf("channel_media_pending_until = %v, already in the past — it buys the download no time at all", pendingUntil.Time)
	}
}

// recordingIssues is an IssueCreator that answers yes and remembers what it
// was asked to file. The issue row itself is synthetic: nothing downstream of
// the create reads it here (an invalid IssueID skips the issue-media lock in
// BindMediaRefs), and what these tests need to know is whether an issue was
// filed at all and under what title.
type recordingIssues struct {
	mu     sync.Mutex
	params []service.IssueCreateParams
}

func (r *recordingIssues) Create(_ context.Context, p service.IssueCreateParams, _ service.IssueCreateOpts) (service.IssueCreateResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.params = append(r.params, p)
	return service.IssueCreateResult{Issue: db.Issue{Title: p.Title}}, nil
}

func (r *recordingIssues) PublishAttachmentsChanged(context.Context, db.Issue, pgtype.UUID) {}

func (r *recordingIssues) filed() []service.IssueCreateParams {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]service.IssueCreateParams(nil), r.params...)
}

// wecomMixedCallback builds a 图文混排 callback whose FIRST run is the
// screenshot and whose second is what the sender typed — the ordering that
// filed nothing. It goes through the real frame decoder and the real
// normalization, including the group mention strip, so nothing about the
// command source is decided by the fixture.
// spoken picks which kind of run carries the sender's words. Both are the
// sender's own words — WeCom recognises a voice run on its side and delivers
// the transcript — and the command source has to read them the same way.
func wecomMixedCallback(t *testing.T, botID, botDisplayName, senderID, msgID, chatType, chatID, imageURL, words string, spoken bool) channel.InboundMessage {
	t.Helper()
	wordRun := map[string]any{"msgtype": "text", "text": map[string]any{"content": words}}
	if spoken {
		wordRun = map[string]any{"msgtype": "voice", "voice": map[string]any{"content": words}}
	}
	raw, err := json.Marshal(map[string]any{
		"msgid":    msgID,
		"aibotid":  botID,
		"chattype": chatType,
		"chatid":   chatID,
		"from":     map[string]any{"userid": senderID},
		"msgtype":  "mixed",
		"mixed": map[string]any{"msg_item": []map[string]any{
			{"msgtype": "image", "image": map[string]any{"url": imageURL, "aeskey": testAESKey}},
			wordRun,
		}},
	})
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}
	var mc aibotMsgCallback
	if err := json.Unmarshal(raw, &mc); err != nil {
		t.Fatalf("decode callback: %v", err)
	}
	text, ok := mc.ownText()
	if !ok {
		t.Fatal("mixed callback is not routable; the fixture is wrong")
	}
	return channelMessageFromCallback(botID, botDisplayName, mc, text, "req-mixed-1")
}

// runMixedIssueThroughTheRouter drives one 图文混排 through the real Router,
// the real ChatSession and a real Postgres, and reports what got filed and
// what the durable message says.
func runMixedIssueThroughTheRouter(t *testing.T, botDisplayName, chatType, chatID, words string, spoken bool) (*recordingIssues, string) {
	t.Helper()
	pool := mediaBindTestDB(t)
	fixture := seedMediaBindFixture(t, pool)
	ctx := context.Background()
	queries := db.New(pool)

	srv := cosServer(t, []byte("\xff\xd8\xff\xe0 a screenshot"), `attachment; filename="shot.jpg"`)
	defer srv.Close()

	storage := &fakeMediaStorage{}
	resolver := newTestResolver(storage, engine.NewDBMediaIntentLedger(queries), nil)
	session := engine.NewChatSession(queries, pool, TypeWecom, engine.SessionTitles{
		Group: "群聊", Direct: "单聊", Fallback: "会话",
	})
	issues := &recordingIssues{}
	router := engine.NewRouter(issues, &bindTestTasks{}, queries, engine.RouterConfig{
		MediaTimeout: 20 * time.Second,
		Logger:       testLogger(),
	})
	router.Register(TypeWecom, NewResolverSet(NewStore(queries), session, nil, resolver))

	if chatID == "" {
		chatID = fixture.senderID
	}
	msgID := fmt.Sprintf("MSGID-MIXED-%d", time.Now().UnixNano())
	msg := wecomMixedCallback(t, fixture.botID, botDisplayName, fixture.senderID, msgID, chatType, chatID, srv.URL, words, spoken)
	if err := router.Handle(ctx, msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	var body string
	deadline := time.Now().Add(15 * time.Second)
	for {
		err := pool.QueryRow(ctx, `
			SELECT cm.content
			FROM chat_message cm
			JOIN chat_session cs ON cs.id = cm.chat_session_id
			WHERE cs.workspace_id = $1 AND cm.role = 'user'
			ORDER BY cm.created_at DESC LIMIT 1`, fixture.workspaceID).Scan(&body)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no durable chat_message was ever written: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return issues, body
}

// TestIssueAfterAnImageIsStillFiled is the user-visible half of the ordering
// defect, checked where the user would check it: was an issue created.
//
// The sender attaches the screenshot, types "/issue 登录坏了", and sends one
// 图文混排. ownText renders it "[Image]\n/issue 登录坏了", the engine's parser
// reads the first non-empty line and only the first, and on the shipped code
// that line was "[Image]" — no issue, no error, no reply saying why. Typing
// the same two things the other way round worked, which is the part that makes
// it impossible to report.
func TestIssueAfterAnImageIsStillFiled(t *testing.T) {
	issues, body := runMixedIssueThroughTheRouter(t, "", "single", "", "/issue 登录坏了", false)

	filed := issues.filed()
	if len(filed) != 1 {
		t.Fatalf("issues created = %d, want 1 — the sender attached a screenshot, typed /issue 登录坏了, and nothing was filed and nothing said why", len(filed))
	}
	if filed[0].Title != "登录坏了" {
		t.Errorf("issue title = %q, want 登录坏了", filed[0].Title)
	}
	// The transcript still shows the attachment and where it sat. Cutting the
	// placeholder out of the body is the other way to break this, and it costs
	// the agent the one signal that a picture was sent.
	if want := "[Image]\n/issue 登录坏了"; body != want {
		t.Errorf("durable body = %q, want %q", body, want)
	}
}

// TestGroupMentionedIssueAfterAnImageIsStillFiled is the conflict-resolution
// test. It fails if EITHER half is lost.
//
// In a group the @-mention is how the bot is reached, so it arrives glued to
// the front of the text run: "@Multica Bot /issue 登录坏了". main strips that
// (stripLeadingMentions) and this PR keeps the placeholders out of the
// command source, and the parser needs both to see "/issue" on the line it
// reads. Take main's version of ws_frame.go wholesale and the strip goes and
// the parser sees "@Multica Bot"; derive the command from the resolved body
// and it sees "[Image]". Either way no issue is filed, and this is the
// assertion that says so.
func TestGroupMentionedIssueAfterAnImageIsStillFiled(t *testing.T) {
	issues, body := runMixedIssueThroughTheRouter(t, "Multica Bot", "group", "GROUP-BIND-1", "@Multica Bot /issue 登录坏了", false)

	filed := issues.filed()
	if len(filed) != 1 {
		t.Fatalf("issues created = %d, want 1 — an @-mentioned /issue with a screenshot in a group filed nothing", len(filed))
	}
	if filed[0].Title != "登录坏了" {
		t.Errorf("issue title = %q, want 登录坏了 (a title carrying the bot's own name means the mention strip was lost)", filed[0].Title)
	}
	if want := "[Image]\n@Multica Bot /issue 登录坏了"; body != want {
		t.Errorf("durable body = %q, want %q — the transcript keeps who was addressed", body, want)
	}
}

// TestGroupMentionedSpokenIssueAfterAnImageIsStillFiled is the same question
// with the words spoken instead of typed, which is the third change meeting
// the other two: WeCom recognises a voice run on its side and hands over the
// transcript, so those are the sender's own words and a spoken "/issue" is a
// command. Read the command off the typed field and it is empty; read it off
// the resolved body and it starts with "[Image]"; skip the mention strip and
// it starts with the bot's own name. All three have to hold at once, and
// unlike the dispatch-level test this one asks the database whether an issue
// exists.
func TestGroupMentionedSpokenIssueAfterAnImageIsStillFiled(t *testing.T) {
	issues, body := runMixedIssueThroughTheRouter(t, "Multica Bot", "group", "GROUP-BIND-2", "@Multica Bot /issue 登录坏了", true)

	filed := issues.filed()
	if len(filed) != 1 {
		t.Fatalf("issues created = %d, want 1 — an @-mentioned /issue said out loud with a screenshot filed nothing", len(filed))
	}
	if filed[0].Title != "登录坏了" {
		t.Errorf("issue title = %q, want 登录坏了", filed[0].Title)
	}
	if want := "[Image]\n@Multica Bot /issue 登录坏了"; body != want {
		t.Errorf("durable body = %q, want %q — a spoken run reads into the body like a typed one", body, want)
	}
}
