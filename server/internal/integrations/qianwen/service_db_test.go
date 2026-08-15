package qianwen

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
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
// developer machines without Postgres (or without migrations 319-323) report a
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

type qianwenInstallAttempt struct {
	result InstallationResult
	err    error
}

const (
	qianwenDBOpenUserID = "opaque-qianwen-db-user"
	qianwenDBOpenUUID   = "opaque-qianwen-db-device"
)

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

	var requestTableReady, requestIndexReady, pairingTableReady, pairingUserIndexReady, pairingCodeIndexReady bool
	if err := pool.QueryRow(ctx, `
		SELECT
			to_regclass('public.qianwen_skill_request') IS NOT NULL,
			to_regclass('public.idx_qianwen_skill_request_installation_request') IS NOT NULL,
			to_regclass('public.qianwen_pairing_code') IS NOT NULL,
			to_regclass('public.idx_qianwen_pairing_code_installation_user') IS NOT NULL,
			to_regclass('public.idx_qianwen_pairing_code_installation_code') IS NOT NULL
	`).Scan(&requestTableReady, &requestIndexReady, &pairingTableReady, &pairingUserIndexReady, &pairingCodeIndexReady); err != nil {
		pool.Close()
		t.Fatalf("check Qianwen migrations: %v", err)
	}
	if !requestTableReady || !requestIndexReady || !pairingTableReady || !pairingUserIndexReady || !pairingCodeIndexReady {
		pool.Close()
		t.Skipf("Qianwen DB vertical slice skipped: migrations 319-323 are not applied (request_table=%v request_index=%v pairing_table=%v pairing_user_index=%v pairing_code_index=%v)",
			requestTableReady, requestIndexReady, pairingTableReady, pairingUserIndexReady, pairingCodeIndexReady)
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
	service, err := NewService(queries, sessions, tasks, pool, []byte("qianwen-db-test-deployment-secret"))
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
	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_user_binding (
			workspace_id, multica_user_id, installation_id,
			channel_type, channel_user_id, config
		) VALUES (
			$1, $2, $3, 'qianwen', $4,
			jsonb_build_object('open_uuid', $5::text, 'identity_scope', 'skill')
		)
	`, fixture.workspaceID, fixture.userID, fixture.installation.Installation.ID, qianwenDBOpenUserID, qianwenDBOpenUUID); err != nil {
		t.Fatalf("seed default Qianwen invocation binding: %v", err)
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
			name: "qianwen pairing code",
			sql: `DELETE FROM qianwen_pairing_code
				WHERE installation_id IN (
					SELECT id FROM channel_installation
					WHERE workspace_id = $1 AND channel_type = 'qianwen'
				)`,
			arg: f.workspaceID,
		},
		{
			name: "qianwen user binding",
			sql: `DELETE FROM channel_user_binding
				WHERE installation_id IN (
					SELECT id FROM channel_installation
					WHERE workspace_id = $1 AND channel_type = 'qianwen'
				)`,
			arg: f.workspaceID,
		},
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

func TestMintPairingCodePersistsOnlyKeyedDigestAndReplacesPreviousCode(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	fixture.service.pairingRandom = bytes.NewReader([]byte{
		0, 0, 0, 1,
		0, 0, 0, 2,
		0, 0, 0, 3,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	first, err := fixture.service.MintPairingCode(ctx, fixture.workspaceID, fixture.installation.Installation.ID, fixture.userID)
	if err != nil {
		t.Fatalf("first MintPairingCode() error = %v", err)
	}
	second, err := fixture.service.MintPairingCode(ctx, fixture.workspaceID, fixture.installation.Installation.ID, fixture.userID)
	if err != nil {
		t.Fatalf("second MintPairingCode() error = %v", err)
	}
	if first.Code != "00000001" || second.Code != "00000002" {
		t.Fatalf("one-time codes = (%q, %q), want deterministic eight-digit codes", first.Code, second.Code)
	}

	var storedDigest []byte
	var storedExpiresAt, storedCreatedAt time.Time
	var rows int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT code_digest, expires_at, created_at,
		       count(*) OVER ()
		FROM qianwen_pairing_code
		WHERE installation_id = $1 AND multica_user_id = $2
	`, fixture.installation.Installation.ID, fixture.userID).Scan(
		&storedDigest, &storedExpiresAt, &storedCreatedAt, &rows,
	); err != nil {
		t.Fatalf("load persisted pairing code: %v", err)
	}
	if rows != 1 {
		t.Fatalf("pairing rows = %d, want replacement in one row", rows)
	}
	if !storedExpiresAt.Equal(second.ExpiresAt) || storedExpiresAt.Sub(storedCreatedAt) != 10*time.Minute {
		t.Fatalf("stored TTL = %s - %s, returned expiry=%s; want exact 10m DB TTL", storedExpiresAt, storedCreatedAt, second.ExpiresAt)
	}

	wantDigest := func(code string) []byte {
		keyMAC := hmac.New(sha256.New, []byte("qianwen-db-test-deployment-secret"))
		_, _ = keyMAC.Write([]byte(pairingCodeKeyDomain))
		mac := hmac.New(sha256.New, keyMAC.Sum(nil))
		_, _ = mac.Write([]byte(pairingCodeMACDomain))
		_, _ = mac.Write(fixture.installation.Installation.ID.Bytes[:])
		_, _ = mac.Write([]byte(code))
		return mac.Sum(nil)
	}
	if !hmac.Equal(storedDigest, wantDigest(second.Code)) {
		t.Fatalf("stored digest = %x, want HMAC for latest code", storedDigest)
	}
	if hmac.Equal(storedDigest, wantDigest(first.Code)) {
		t.Fatal("second mint left the first pairing code valid")
	}
	if bytes.Contains(storedDigest, []byte(first.Code)) || bytes.Contains(storedDigest, []byte(second.Code)) {
		t.Fatalf("stored digest contains plaintext code bytes: %x", storedDigest)
	}

	if _, err := fixture.pool.Exec(ctx, `UPDATE channel_installation SET status = 'revoked' WHERE id = $1`, fixture.installation.Installation.ID); err != nil {
		t.Fatalf("revoke installation: %v", err)
	}
	if _, err := fixture.service.MintPairingCode(ctx, fixture.workspaceID, fixture.installation.Installation.ID, fixture.userID); !errors.Is(err, ErrInstallationNotFound) {
		t.Fatalf("MintPairingCode() after revoke error = %v, want ErrInstallationNotFound", err)
	}
}

func TestMintPairingCodeRetriesCrossUserCodeCollision(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var secondUserID pgtype.UUID
	if err := fixture.pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Qianwen Pairing Collision User', $1)
		RETURNING id
	`, "qianwen-pairing-collision-"+uuid.NewString()+"@multica.test").Scan(&secondUserID); err != nil {
		t.Fatalf("insert second pairing user: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, fixture.workspaceID, secondUserID); err != nil {
		t.Fatalf("add second pairing user to workspace: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `UPDATE agent SET permission_mode = 'public_to' WHERE id = $1`, fixture.agentID); err != nil {
		t.Fatalf("make collision-test agent invokable: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
		VALUES ($1, 'workspace', $2, $3)
	`, fixture.agentID, fixture.workspaceID, fixture.userID); err != nil {
		t.Fatalf("grant workspace invocation for collision test: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fixture.pool.Exec(context.Background(), `DELETE FROM qianwen_pairing_code WHERE multica_user_id = $1`, secondUserID)
		_, _ = fixture.pool.Exec(context.Background(), `DELETE FROM agent_invocation_target WHERE agent_id = $1 AND target_type = 'workspace' AND target_id = $2`, fixture.agentID, fixture.workspaceID)
		_, _ = fixture.pool.Exec(context.Background(), `UPDATE agent SET permission_mode = 'private' WHERE id = $1`, fixture.agentID)
		_, _ = fixture.pool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, fixture.workspaceID, secondUserID)
		_, _ = fixture.pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, secondUserID)
	})

	fixture.service.pairingRandom = bytes.NewReader([]byte{
		0, 0, 0, 1,
		0, 0, 0, 1,
		0, 0, 0, 2,
	})
	first, err := fixture.service.MintPairingCode(ctx, fixture.workspaceID, fixture.installation.Installation.ID, fixture.userID)
	if err != nil {
		t.Fatalf("first user's MintPairingCode() error = %v", err)
	}
	second, err := fixture.service.MintPairingCode(ctx, fixture.workspaceID, fixture.installation.Installation.ID, secondUserID)
	if err != nil {
		t.Fatalf("second user's MintPairingCode() error = %v", err)
	}
	if first.Code != "00000001" || second.Code != "00000002" {
		t.Fatalf("pairing codes = (%q, %q), want collision retry to 00000002", first.Code, second.Code)
	}

	var rows, distinctDigests int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT count(*), count(DISTINCT code_digest)
		FROM qianwen_pairing_code
		WHERE installation_id = $1
		  AND multica_user_id IN ($2, $3)
	`, fixture.installation.Installation.ID, fixture.userID, secondUserID).Scan(&rows, &distinctDigests); err != nil {
		t.Fatalf("inspect pairing collision rows: %v", err)
	}
	if rows != 2 || distinctDigests != 2 {
		t.Fatalf("pairing rows=%d distinct_digests=%d, want 2 unambiguous rows", rows, distinctDigests)
	}
}

func TestRedeemPairingCodeAtomicallyBindsOpaqueIdentityAndReturnsStableOutcomeOnRetry(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.UnixMilli(1786723200000)
	fixture.service.now = func() time.Time { return now }

	minted, err := fixture.service.MintPairingCode(ctx, fixture.workspaceID, fixture.installation.Installation.ID, fixture.userID)
	if err != nil {
		t.Fatalf("MintPairingCode() error = %v", err)
	}
	request := PairingRedeemRequest{
		Code: minted.Code,
		Identity: InvocationMetadata{
			OpenUserID: "opaque/User+Cipher==",
			OpenUUID:   "opaque/Device+Cipher==",
			Timestamp:  fmt.Sprint(now.UnixMilli()),
			Nonce:      "0123456789abcdef0123456789abcdef",
		},
	}
	signPairingRedeemRequest(fixture.installation.AccessToken, &request)

	result, err := fixture.service.RedeemPairingCode(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, request)
	if err != nil {
		t.Fatalf("RedeemPairingCode() error = %v", err)
	}
	if result.InstallationID != fixture.installation.Installation.ID || result.MulticaUserID != fixture.userID {
		t.Fatalf("RedeemPairingCode() result = %+v, want installation/user from pairing row", result)
	}

	binding, err := fixture.queries.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{
		InstallationID: fixture.installation.Installation.ID,
		ChannelUserID:  request.Identity.OpenUserID,
	})
	if err != nil {
		t.Fatalf("load Qianwen user binding: %v", err)
	}
	if binding.MulticaUserID != fixture.userID || binding.ChannelType != string(TypeQianwen) {
		t.Fatalf("binding = %+v, want paired Multica user and qianwen type", binding)
	}
	var bindingConfig map[string]string
	if err := json.Unmarshal(binding.Config, &bindingConfig); err != nil {
		t.Fatalf("decode binding config: %v", err)
	}
	if bindingConfig["open_uuid"] != request.Identity.OpenUUID || bindingConfig["identity_scope"] != "skill" {
		t.Fatalf("binding config = %#v, want exact opaque open_uuid and skill scope", bindingConfig)
	}

	var pendingCodes int
	if err := fixture.pool.QueryRow(ctx,
		`SELECT count(*) FROM qianwen_pairing_code WHERE installation_id = $1 AND multica_user_id = $2`,
		fixture.installation.Installation.ID, fixture.userID,
	).Scan(&pendingCodes); err != nil {
		t.Fatalf("count pending pairing codes: %v", err)
	}
	if pendingCodes != 0 {
		t.Fatalf("pending pairing codes = %d, want consumed", pendingCodes)
	}

	replayed, err := fixture.service.RedeemPairingCode(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, request)
	if err != nil {
		t.Fatalf("exact retry RedeemPairingCode() error = %v, want prior successful outcome", err)
	}
	if replayed != result {
		t.Fatalf("exact retry result = %+v, want prior result %+v", replayed, result)
	}

	request.Identity.Nonce = "fedcba9876543210fedcba9876543210"
	request.Identity.Timestamp = fmt.Sprint(now.Add(time.Second).UnixMilli())
	signPairingRedeemRequest(fixture.installation.AccessToken, &request)
	retried, err := fixture.service.RedeemPairingCode(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, request)
	if err != nil {
		t.Fatalf("provider retry with a new nonce RedeemPairingCode() error = %v, want prior successful outcome", err)
	}
	if retried != result {
		t.Fatalf("provider retry result = %+v, want prior result %+v", retried, result)
	}
}

func signPairingRedeemRequest(token string, request *PairingRedeemRequest) {
	canonical := strings.Join([]string{
		"QIANWEN-HMAC-SHA256-V1",
		"binding_redeem",
		request.Identity.Timestamp,
		request.Identity.Nonce,
		request.Identity.OpenUserID,
		request.Identity.OpenUUID,
		request.Code,
	}, "\n")
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(canonical))
	request.Identity.Signature = hex.EncodeToString(mac.Sum(nil))
}

func signedQianwenDBSubmitInvocation(token string, request SubmitRequest, now time.Time) SubmitInvocation {
	invocation := SubmitInvocation{
		Request: request,
		Identity: InvocationMetadata{
			OpenUserID: qianwenDBOpenUserID,
			OpenUUID:   qianwenDBOpenUUID,
			Timestamp:  fmt.Sprint(now.UnixMilli()),
			Nonce:      "0123456789abcdef0123456789abcdef",
		},
	}
	canonical, err := CanonicalSubmitInvocation(invocation)
	if err != nil {
		panic(fmt.Sprintf("canonicalize Qianwen DB submit fixture: %v", err))
	}
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(canonical))
	invocation.Identity.Signature = hex.EncodeToString(mac.Sum(nil))
	return invocation
}

func signedQianwenDBStatusInvocation(token, requestID string, now time.Time) StatusInvocation {
	invocation := StatusInvocation{
		RequestID: requestID,
		Identity: InvocationMetadata{
			OpenUserID: qianwenDBOpenUserID,
			OpenUUID:   qianwenDBOpenUUID,
			Timestamp:  fmt.Sprint(now.UnixMilli()),
			Nonce:      "fedcba9876543210fedcba9876543210",
		},
	}
	canonical, err := CanonicalStatusInvocation(invocation)
	if err != nil {
		panic(fmt.Sprintf("canonicalize Qianwen DB status fixture: %v", err))
	}
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(canonical))
	invocation.Identity.Signature = hex.EncodeToString(mac.Sum(nil))
	return invocation
}

func (f *qianwenServiceDBFixture) submit(ctx context.Context, requestID, query string) (SubmitResult, error) {
	invocation := signedQianwenDBSubmitInvocation(f.installation.AccessToken, SubmitRequest{
		RequestID: requestID,
		Query:     query,
	}, f.service.now())
	return f.service.Submit(ctx, f.installation.ConnectionID, f.installation.AccessToken, invocation)
}

func (f *qianwenServiceDBFixture) serviceStoppedBeforeFinalize(t *testing.T) (*Service, *qianwenFinalizeBarrier) {
	t.Helper()

	barrier := newQianwenFinalizeBarrier(t)
	service, err := newService(f.queries, f.service.sessions, &qianwenFinalizeBarrierSender{
		delegate: f.service.tasks,
		barrier:  barrier,
	}, []byte("qianwen-db-test-deployment-secret"))
	if err != nil {
		t.Fatalf("construct barrier Qianwen service: %v", err)
	}
	return service, barrier
}

func startQianwenSubmit(ctx context.Context, service *Service, connectionID, token, requestID, query string) <-chan qianwenSubmitAttempt {
	done := make(chan qianwenSubmitAttempt, 1)
	go func() {
		invocation := signedQianwenDBSubmitInvocation(token, SubmitRequest{
			RequestID: requestID,
			Query:     query,
		}, service.now())
		result, err := service.Submit(ctx, connectionID, token, invocation)
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

func waitForBlockedQianwenClaim(t *testing.T, pool *pgxpool.Pool, done <-chan qianwenSubmitAttempt) {
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
				  AND query LIKE '%name: ClaimQianwenRequest%'
			)
		`).Scan(&blocked); err != nil {
			t.Fatalf("observe blocked Qianwen claim: %v", err)
		}
		if blocked {
			return
		}
		select {
		case attempt := <-done:
			t.Fatalf("Qianwen Submit returned before reaching the workspace claim fence: result=%+v error=%v", attempt.result, attempt.err)
		default:
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("Qianwen claim did not block behind the workspace teardown lock")
		}
	}
}

func startQianwenInstall(ctx context.Context, service *Service, workspaceID, agentID, installerID pgtype.UUID) <-chan qianwenInstallAttempt {
	done := make(chan qianwenInstallAttempt, 1)
	go func() {
		result, err := service.InstallPersonal(ctx, workspaceID, agentID, installerID)
		done <- qianwenInstallAttempt{result: result, err: err}
	}()
	return done
}

func waitForBlockedQianwenInstall(t *testing.T, pool *pgxpool.Pool, done <-chan qianwenInstallAttempt) {
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
				  AND query LIKE '%name: InstallQianwenPersonal%'
			)
		`).Scan(&blocked); err != nil {
			t.Fatalf("observe blocked Qianwen install: %v", err)
		}
		if blocked {
			return
		}
		select {
		case attempt := <-done:
			t.Fatalf("Qianwen install returned before reaching the workspace lifecycle fence: result=%+v error=%v", attempt.result, attempt.err)
		default:
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("Qianwen install did not block behind workspace teardown")
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

		status, err := fixture.service.Status(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken,
			signedQianwenDBStatusInvocation(fixture.installation.AccessToken, requestID, fixture.service.now()))
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

		status, err := fixture.service.Status(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken,
			signedQianwenDBStatusInvocation(fixture.installation.AccessToken, requestID, fixture.service.now()))
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

		status, err := fixture.service.Status(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken,
			signedQianwenDBStatusInvocation(fixture.installation.AccessToken, requestID, fixture.service.now()))
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
	waitForBlockedQianwenClaim(t, fixture.pool, done)
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
	if err := fixture.service.Revoke(ctx, fixture.workspaceID, fixture.installation.Installation.ID); err != nil {
		t.Fatalf("revoke installation before finalize: %v", err)
	}
	barrier.open()

	attempt := awaitQianwenSubmit(t, done)
	if !errors.Is(attempt.err, ErrUnauthorized) {
		t.Fatalf("Submit finalized after revoke with error %v, want ErrUnauthorized", attempt.err)
	}
	fixture.assertRequestUnpublished(t, ctx, requestID, query)
}

func TestQianwenServiceDBActiveReinstallConflictsWithoutChangingCredential(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	before, err := fixture.service.GetInWorkspace(ctx, fixture.installation.Installation.ID, fixture.workspaceID)
	if err != nil {
		t.Fatalf("load installation before repeated install: %v", err)
	}

	_, err = fixture.service.InstallPersonal(ctx, fixture.workspaceID, fixture.agentID, fixture.userID)
	if !errors.Is(err, ErrInstallationAlreadyActive) {
		t.Fatalf("repeated active InstallPersonal() error = %v, want ErrInstallationAlreadyActive", err)
	}
	const callers = 8
	start := make(chan struct{})
	done := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			_, installErr := fixture.service.InstallPersonal(ctx, fixture.workspaceID, fixture.agentID, fixture.userID)
			done <- installErr
		}()
	}
	close(start)
	for range callers {
		if installErr := <-done; !errors.Is(installErr, ErrInstallationAlreadyActive) {
			t.Fatalf("concurrent active InstallPersonal() error = %v, want ErrInstallationAlreadyActive", installErr)
		}
	}

	after, err := fixture.service.GetInWorkspace(ctx, fixture.installation.Installation.ID, fixture.workspaceID)
	if err != nil {
		t.Fatalf("load installation after repeated install: %v", err)
	}
	if before.ID != after.ID || before.InstallerUserID != after.InstallerUserID || before.Status != after.Status ||
		!bytes.Equal(before.Config, after.Config) || before.InstalledAt != after.InstalledAt || before.UpdatedAt != after.UpdatedAt {
		t.Fatalf("active repeated install mutated installation\nbefore=%+v\nafter=%+v", before, after)
	}

	requestID := uuid.NewString()
	query := "run with the original credential"
	result, err := fixture.service.Submit(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken,
		signedQianwenDBSubmitInvocation(fixture.installation.AccessToken, SubmitRequest{RequestID: requestID, Query: query}, fixture.service.now()))
	if err != nil {
		t.Fatalf("Submit with original credential after conflict: %v", err)
	}
	if result.RequestID != requestID || result.Status != "accepted" {
		t.Fatalf("original credential Submit result = %+v, want accepted request %s", result, requestID)
	}
	fixture.assertPersistedRequest(t, ctx, requestID, query)
}

func TestQianwenServiceDBConcurrentReconnectHasOneCredentialWinner(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := fixture.service.Revoke(ctx, fixture.workspaceID, fixture.installation.Installation.ID); err != nil {
		t.Fatalf("revoke before concurrent reconnect: %v", err)
	}

	start := make(chan struct{})
	done := make(chan qianwenInstallAttempt, 2)
	for range 2 {
		go func() {
			<-start
			result, err := fixture.service.InstallPersonal(ctx, fixture.workspaceID, fixture.agentID, fixture.userID)
			done <- qianwenInstallAttempt{result: result, err: err}
		}()
	}
	close(start)
	attempts := []qianwenInstallAttempt{<-done, <-done}
	var winner InstallationResult
	winners, conflicts := 0, 0
	for _, attempt := range attempts {
		switch {
		case attempt.err == nil:
			winner = attempt.result
			winners++
		case errors.Is(attempt.err, ErrInstallationAlreadyActive):
			conflicts++
		default:
			t.Fatalf("concurrent reconnect error = %v", attempt.err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("concurrent reconnect outcomes = winners %d conflicts %d, want 1/1", winners, conflicts)
	}
	if winner.Installation.ID != fixture.installation.Installation.ID || winner.Installation.Status != "active" {
		t.Fatalf("winning reconnect installation = %+v", winner.Installation)
	}
	if winner.ConnectionID == fixture.installation.ConnectionID || winner.AccessToken == fixture.installation.AccessToken {
		t.Fatal("revoked reconnect reused the old credential")
	}

	stored, err := fixture.service.GetInWorkspace(ctx, winner.Installation.ID, fixture.workspaceID)
	if err != nil {
		t.Fatalf("load winning reconnect: %v", err)
	}
	if !verifyAccessToken(stored.Config, winner.AccessToken) || verifyAccessToken(stored.Config, fixture.installation.AccessToken) {
		t.Fatal("stored reconnect credential does not match the sole winner")
	}
	requestID := uuid.NewString()
	if _, err := fixture.service.Status(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken,
		signedQianwenDBStatusInvocation(fixture.installation.AccessToken, requestID, fixture.service.now())); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old credential after reconnect error = %v, want ErrUnauthorized", err)
	}
	if _, err := fixture.service.Status(ctx, winner.ConnectionID, winner.AccessToken,
		signedQianwenDBStatusInvocation(winner.AccessToken, requestID, fixture.service.now())); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("winning credential without a new binding error = %v, want ErrRequestNotFound", err)
	}
}

func TestQianwenServiceDBWorkspaceDeleteFencesReinstall(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := fixture.service.Revoke(ctx, fixture.workspaceID, fixture.installation.Installation.ID); err != nil {
		t.Fatalf("revoke before reconnect race: %v", err)
	}

	deleteTx, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin workspace teardown: %v", err)
	}
	defer deleteTx.Rollback(ctx)
	qtx := fixture.queries.WithTx(deleteTx)
	if _, err := qtx.LockWorkspaceForDelete(ctx, fixture.workspaceID); err != nil {
		t.Fatalf("lock workspace for teardown: %v", err)
	}
	if err := qtx.DeleteWorkspace(ctx, fixture.workspaceID); err != nil {
		t.Fatalf("sweep workspace before reconnect: %v", err)
	}

	done := startQianwenInstall(ctx, fixture.service, fixture.workspaceID, fixture.agentID, fixture.userID)
	waitForBlockedQianwenInstall(t, fixture.pool, done)
	if err := deleteTx.Commit(ctx); err != nil {
		t.Fatalf("commit workspace teardown: %v", err)
	}
	attempt := <-done
	if !errors.Is(attempt.err, ErrInstallationNotFound) {
		t.Fatalf("InstallPersonal after workspace teardown = %+v, error %v; want ErrInstallationNotFound", attempt.result, attempt.err)
	}

	var installations, workspaces int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM channel_installation WHERE workspace_id = $1),
			(SELECT count(*) FROM workspace WHERE id = $1)
	`, fixture.workspaceID).Scan(&installations, &workspaces); err != nil {
		t.Fatalf("inspect install/workspace rows after race: %v", err)
	}
	if installations != 0 || workspaces != 0 {
		t.Fatalf("workspace teardown race left installations=%d workspaces=%d, want zero", installations, workspaces)
	}
}

func TestQianwenServiceDBMemberRemovalFencesReinstall(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	member, err := fixture.queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		WorkspaceID: fixture.workspaceID,
		UserID:      fixture.userID,
	})
	if err != nil {
		t.Fatalf("load installer membership: %v", err)
	}

	deleteTx, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin member removal: %v", err)
	}
	defer deleteTx.Rollback(ctx)
	qtx := fixture.queries.WithTx(deleteTx)
	if err := qtx.LockSubscriberWrites(ctx, db.LockSubscriberWritesParams{
		WorkspaceID: fixture.workspaceID,
		UserID:      fixture.userID,
	}); err != nil {
		t.Fatalf("lock member lifecycle: %v", err)
	}
	if err := qtx.RevokeQianwenInstallationsByInstaller(ctx, db.RevokeQianwenInstallationsByInstallerParams{
		WorkspaceID:   fixture.workspaceID,
		MulticaUserID: fixture.userID,
	}); err != nil {
		t.Fatalf("revoke installer credentials: %v", err)
	}
	if err := qtx.DeleteMember(ctx, member.ID); err != nil {
		t.Fatalf("delete installer membership: %v", err)
	}

	done := startQianwenInstall(ctx, fixture.service, fixture.workspaceID, fixture.agentID, fixture.userID)
	waitForBlockedQianwenInstall(t, fixture.pool, done)
	if err := deleteTx.Commit(ctx); err != nil {
		t.Fatalf("commit member removal: %v", err)
	}
	attempt := <-done
	if !errors.Is(attempt.err, ErrInstallationNotFound) {
		t.Fatalf("InstallPersonal after member removal = %+v, error %v; want ErrInstallationNotFound", attempt.result, attempt.err)
	}

	stored, err := fixture.service.GetInWorkspace(ctx, fixture.installation.Installation.ID, fixture.workspaceID)
	if err != nil {
		t.Fatalf("load installation after member removal race: %v", err)
	}
	if stored.Status != "revoked" {
		t.Fatalf("installation status after member removal race = %q, want revoked", stored.Status)
	}
}

func TestQianwenServiceDBAgentArchiveFencesReinstall(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := fixture.service.Revoke(ctx, fixture.workspaceID, fixture.installation.Installation.ID); err != nil {
		t.Fatalf("revoke before agent archive race: %v", err)
	}

	archiveTx, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin agent archive: %v", err)
	}
	defer archiveTx.Rollback(ctx)
	qtx := fixture.queries.WithTx(archiveTx)
	if _, err := qtx.ArchiveAgent(ctx, db.ArchiveAgentParams{ID: fixture.agentID, ArchivedBy: fixture.userID}); err != nil {
		t.Fatalf("archive agent: %v", err)
	}

	done := startQianwenInstall(ctx, fixture.service, fixture.workspaceID, fixture.agentID, fixture.userID)
	waitForBlockedQianwenInstall(t, fixture.pool, done)
	if err := archiveTx.Commit(ctx); err != nil {
		t.Fatalf("commit agent archive: %v", err)
	}
	attempt := <-done
	if !errors.Is(attempt.err, ErrInstallationNotFound) {
		t.Fatalf("InstallPersonal after agent archive = %+v, error %v; want ErrInstallationNotFound", attempt.result, attempt.err)
	}

	stored, err := fixture.service.GetInWorkspace(ctx, fixture.installation.Installation.ID, fixture.workspaceID)
	if err != nil {
		t.Fatalf("load installation after agent archive race: %v", err)
	}
	if stored.Status != "revoked" {
		t.Fatalf("installation status after agent archive race = %q, want revoked", stored.Status)
	}
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
	before, err := fixture.service.Status(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken,
		signedQianwenDBStatusInvocation(fixture.installation.AccessToken, requestID, fixture.service.now()))
	if err != nil {
		t.Fatalf("Status before chat hard delete: %v", err)
	}
	if before.Status != "completed" || before.TaskID != util.UUIDToString(ledger.TaskID) || before.Output != output {
		t.Fatalf("Status before chat hard delete = %+v", before)
	}

	fixture.hardDeleteChatLikeHandler(t, ctx, ledger.ChatSessionID)
	after, err := fixture.service.Status(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken,
		signedQianwenDBStatusInvocation(fixture.installation.AccessToken, requestID, fixture.service.now()))
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
