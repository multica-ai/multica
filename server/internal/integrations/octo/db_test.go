package octo_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// These tests exercise the octo_* queries against a real PostgreSQL instance.
// They follow the repo convention (see internal/handler/handler_test.go): read
// DATABASE_URL, and skip — never fail — when no database is reachable, so the
// suite is a no-op locally without a DB but runs for real in CI.

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	if pool, err := pgxpool.New(ctx, dbURL); err == nil {
		if perr := pool.Ping(ctx); perr == nil {
			testPool = pool
		} else {
			fmt.Printf("octo DB tests will skip: database not reachable: %v\n", perr)
			pool.Close()
		}
	} else {
		fmt.Printf("octo DB tests will skip: cannot connect: %v\n", err)
	}
	code := m.Run()
	if testPool != nil {
		testPool.Close()
	}
	os.Exit(code)
}

// requireDB skips a test when no database is configured, so mock-only tests in
// the package still run locally without a database.
func requireDB(t *testing.T) {
	t.Helper()
	if testPool == nil {
		t.Skip("no database available (set DATABASE_URL)")
	}
}

func randToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// fixture creates a throwaway workspace + user + member + agent and returns
// their IDs, registering cleanup that cascades everything away.
func fixture(t *testing.T) (workspaceID, userID, agentID pgtype.UUID) {
	t.Helper()
	ctx := context.Background()

	slug := "octo-test-" + randToken()[:8]
	email := "octo-test-" + randToken()[:8] + "@example.com"

	// workspace
	err := testPool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug) VALUES ($1, $2) RETURNING id`,
		"Octo Test WS", slug).Scan(&workspaceID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	// user
	err = testPool.QueryRow(ctx,
		`INSERT INTO "user" (email, name) VALUES ($1, $2) RETURNING id`,
		email, "Octo Tester").Scan(&userID)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	// member
	_, err = testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		workspaceID, userID)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	// agent requires runtime_mode and a NOT NULL runtime_id (migration 004),
	// so create an agent_runtime first.
	var runtimeID pgtype.UUID
	err = testPool.QueryRow(ctx,
		`INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider)
		 VALUES ($1, 'Octo Runtime', 'local', 'octo_test') RETURNING id`,
		workspaceID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create agent_runtime: %v", err)
	}
	err = testPool.QueryRow(ctx,
		`INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		 VALUES ($1, $2, 'local', $3) RETURNING id`,
		workspaceID, "Octo Agent", runtimeID).Scan(&agentID)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	wsID, uID := workspaceID, userID
	t.Cleanup(func() {
		// Deleting the workspace cascades to member/agent/installation/etc.
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, uID)
	})
	return workspaceID, userID, agentID
}

func newInstallation(t *testing.T, q *db.Queries, wsID, userID, agentID pgtype.UUID) db.OctoInstallation {
	t.Helper()
	inst, err := q.CreateOctoInstallation(context.Background(), db.CreateOctoInstallationParams{
		WorkspaceID:       wsID,
		AgentID:           agentID,
		BotTokenEncrypted: []byte("ciphertext"),
		RobotID:           "robot_" + randToken(),
		BotName:           "Octo-Z",
		OwnerUid:          "owner_uid_x",
		ApiUrl:            "https://im.example/api",
		WsUrl:             "wss://im.example/ws",
		InstallerUserID:   userID,
	})
	if err != nil {
		t.Fatalf("CreateOctoInstallation: %v", err)
	}
	return inst
}

func TestOctoInstallation_CRUD(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)

	inst := newInstallation(t, q, wsID, userID, agentID)
	if inst.Status != "active" {
		t.Errorf("status = %q, want active", inst.Status)
	}

	// GetByRobotID — the inbound routing path.
	got, err := q.GetOctoInstallationByRobotID(context.Background(), inst.RobotID)
	if err != nil {
		t.Fatalf("GetByRobotID: %v", err)
	}
	if got.ID != inst.ID {
		t.Errorf("GetByRobotID returned wrong row")
	}

	// GetByAgent — one bot per agent.
	got2, err := q.GetOctoInstallationByAgent(context.Background(), db.GetOctoInstallationByAgentParams{
		WorkspaceID: wsID, AgentID: agentID,
	})
	if err != nil || got2.ID != inst.ID {
		t.Fatalf("GetByAgent: %v", err)
	}

	// List active includes it.
	active, err := q.ListActiveOctoInstallations(context.Background())
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if !containsInstallation(active, inst.ID) {
		t.Errorf("ListActive missing the new installation")
	}

	// Revoke → no longer active.
	if err := q.SetOctoInstallationStatus(context.Background(), db.SetOctoInstallationStatusParams{
		ID: inst.ID, Status: "revoked",
	}); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	active2, _ := q.ListActiveOctoInstallations(context.Background())
	if containsInstallation(active2, inst.ID) {
		t.Errorf("revoked installation still listed active")
	}
}

func containsInstallation(list []db.OctoInstallation, id pgtype.UUID) bool {
	for _, i := range list {
		if i.ID == id {
			return true
		}
	}
	return false
}

func TestOctoInstallation_UpsertOnConflict(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)

	first := newInstallation(t, q, wsID, userID, agentID)

	// Upsert on the same (workspace, agent) updates rather than duplicates.
	second, err := q.UpsertOctoInstallation(context.Background(), db.UpsertOctoInstallationParams{
		WorkspaceID:       wsID,
		AgentID:           agentID,
		BotTokenEncrypted: []byte("new-ciphertext"),
		RobotID:           "robot_updated_" + randToken(),
		BotName:           "Octo-Z2",
		OwnerUid:          "owner2",
		ApiUrl:            "https://im.example/api",
		WsUrl:             "wss://im.example/ws",
		InstallerUserID:   userID,
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("upsert created a new row (%v) instead of updating (%v)", second.ID, first.ID)
	}
	if second.BotName != "Octo-Z2" {
		t.Errorf("upsert did not refresh bot_name: %q", second.BotName)
	}
}

func TestOctoInboundDedup_TwoPhaseClaim(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)
	inst := newInstallation(t, q, wsID, userID, agentID)
	ctx := context.Background()
	msgID := "msg_" + randToken()

	// First claim succeeds.
	claim, err := q.ClaimOctoInboundDedup(ctx, db.ClaimOctoInboundDedupParams{
		InstallationID: inst.ID, MessageID: msgID,
	})
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}

	// Second claim while in-flight (within 60s) returns no rows.
	_, err = q.ClaimOctoInboundDedup(ctx, db.ClaimOctoInboundDedupParams{
		InstallationID: inst.ID, MessageID: msgID,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("second concurrent claim: err = %v, want pgx.ErrNoRows", err)
	}

	// Mark with the WRONG token is fenced out (0 rows).
	n, err := q.MarkOctoInboundDedupProcessed(ctx, db.MarkOctoInboundDedupProcessedParams{
		InstallationID: inst.ID, MessageID: msgID, ClaimToken: randUUID(),
	})
	if err != nil {
		t.Fatalf("mark wrong token: %v", err)
	}
	if n != 0 {
		t.Errorf("mark with wrong token affected %d rows, want 0", n)
	}

	// Mark with the correct token succeeds (1 row).
	n, err = q.MarkOctoInboundDedupProcessed(ctx, db.MarkOctoInboundDedupProcessedParams{
		InstallationID: inst.ID, MessageID: msgID, ClaimToken: claim.ClaimToken,
	})
	if err != nil {
		t.Fatalf("mark correct token: %v", err)
	}
	if n != 1 {
		t.Errorf("mark with correct token affected %d rows, want 1", n)
	}

	// After terminal, a replay claim returns no rows even much later.
	_, err = q.ClaimOctoInboundDedup(ctx, db.ClaimOctoInboundDedupParams{
		InstallationID: inst.ID, MessageID: msgID,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("claim after terminal mark: err = %v, want pgx.ErrNoRows", err)
	}
}

func TestOctoInboundDedup_ReleaseAllowsReclaim(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)
	inst := newInstallation(t, q, wsID, userID, agentID)
	ctx := context.Background()
	msgID := "msg_" + randToken()

	claim, err := q.ClaimOctoInboundDedup(ctx, db.ClaimOctoInboundDedupParams{
		InstallationID: inst.ID, MessageID: msgID,
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	// Release with the correct token removes the in-flight claim.
	n, err := q.ReleaseOctoInboundDedup(ctx, db.ReleaseOctoInboundDedupParams{
		InstallationID: inst.ID, MessageID: msgID, ClaimToken: claim.ClaimToken,
	})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if n != 1 {
		t.Errorf("release affected %d rows, want 1", n)
	}

	// Reclaim succeeds immediately (no staleness wait).
	if _, err := q.ClaimOctoInboundDedup(ctx, db.ClaimOctoInboundDedupParams{
		InstallationID: inst.ID, MessageID: msgID,
	}); err != nil {
		t.Errorf("reclaim after release failed: %v", err)
	}
}

// TestOctoInboundDedup_StaleReclaim covers the crash-recovery branch: an
// in-flight claim (processed_at IS NULL) older than the 60s staleness TTL must
// be re-claimable, minting a fresh claim_token so the original (crashed) owner
// can no longer Mark/Release it. We back-date received_at instead of sleeping.
func TestOctoInboundDedup_StaleReclaim(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)
	inst := newInstallation(t, q, wsID, userID, agentID)
	ctx := context.Background()
	msgID := "msg_" + randToken()

	first, err := q.ClaimOctoInboundDedup(ctx, db.ClaimOctoInboundDedupParams{
		InstallationID: inst.ID, MessageID: msgID,
	})
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}

	// Age the in-flight claim past the 60s staleness window.
	if _, err := testPool.Exec(ctx,
		`UPDATE octo_inbound_dedup
		 SET received_at = now() - INTERVAL '61 seconds'
		 WHERE installation_id = $1 AND message_id = $2`,
		inst.ID, msgID); err != nil {
		t.Fatalf("back-date received_at: %v", err)
	}

	// A fresh claim now succeeds (the stale in-flight row is re-taken).
	second, err := q.ClaimOctoInboundDedup(ctx, db.ClaimOctoInboundDedupParams{
		InstallationID: inst.ID, MessageID: msgID,
	})
	if err != nil {
		t.Fatalf("stale reclaim should succeed: %v", err)
	}
	// The reclaim must mint a new owner fence; the crashed owner's token is dead.
	if second.ClaimToken == first.ClaimToken {
		t.Errorf("stale reclaim reused the original claim_token; owner fence not rotated")
	}

	// The original (crashed) owner can no longer Mark — its token lost the row.
	n, err := q.MarkOctoInboundDedupProcessed(ctx, db.MarkOctoInboundDedupProcessedParams{
		InstallationID: inst.ID, MessageID: msgID, ClaimToken: first.ClaimToken,
	})
	if err != nil {
		t.Fatalf("mark with stale token: %v", err)
	}
	if n != 0 {
		t.Errorf("stale owner's Mark affected %d rows, want 0", n)
	}
}

func TestOctoBindingToken_ConsumeOnce(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)
	inst := newInstallation(t, q, wsID, userID, agentID)
	ctx := context.Background()

	hash := "hash_" + randToken()
	_, err := q.CreateOctoBindingToken(ctx, db.CreateOctoBindingTokenParams{
		TokenHash:      hash,
		WorkspaceID:    wsID,
		InstallationID: inst.ID,
		OctoUid:        "uid_x",
		ExpiresAt:      pgtype.Timestamptz{Time: time.Now().Add(10 * time.Minute), Valid: true},
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	// First consume succeeds.
	if _, err := q.ConsumeOctoBindingToken(ctx, hash); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	// Second consume returns no rows (single-use).
	if _, err := q.ConsumeOctoBindingToken(ctx, hash); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("second consume: err = %v, want pgx.ErrNoRows (already consumed)", err)
	}
}

// TestOctoBindingToken_ConsumeExpired covers the `expires_at > now()` guard:
// a token that has lapsed must not be redeemable, even though it was never
// consumed. The DB CHECK caps TTL at 15 minutes so we mint with a ~1s lifetime
// and back-date it past expiry rather than sleeping.
func TestOctoBindingToken_ConsumeExpired(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)
	inst := newInstallation(t, q, wsID, userID, agentID)
	ctx := context.Background()

	hash := "hash_" + randToken()
	if _, err := q.CreateOctoBindingToken(ctx, db.CreateOctoBindingTokenParams{
		TokenHash:      hash,
		WorkspaceID:    wsID,
		InstallationID: inst.ID,
		OctoUid:        "uid_x",
		ExpiresAt:      pgtype.Timestamptz{Time: time.Now().Add(1 * time.Minute), Valid: true},
	}); err != nil {
		t.Fatalf("create token: %v", err)
	}

	// Force the token past its expiry without tripping the TTL-cap CHECK
	// (the cap is relative to created_at, which we leave untouched).
	if _, err := testPool.Exec(ctx,
		`UPDATE octo_binding_token SET expires_at = now() - INTERVAL '1 second'
		 WHERE token_hash = $1`, hash); err != nil {
		t.Fatalf("expire token: %v", err)
	}

	if _, err := q.ConsumeOctoBindingToken(ctx, hash); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("consume of expired token: err = %v, want pgx.ErrNoRows", err)
	}
}

func TestOctoBindingToken_TTLCapRejected(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)
	inst := newInstallation(t, q, wsID, userID, agentID)
	ctx := context.Background()

	// expires_at beyond the 15-minute DB CHECK cap must be rejected with the
	// check_violation SQLSTATE (23514) — asserting the specific code ensures the
	// CHECK is what fired, not some unrelated error or a future schema rename
	// that silently stops enforcing it.
	_, err := q.CreateOctoBindingToken(ctx, db.CreateOctoBindingTokenParams{
		TokenHash:      "hash_" + randToken(),
		WorkspaceID:    wsID,
		InstallationID: inst.ID,
		OctoUid:        "uid_x",
		ExpiresAt:      pgtype.Timestamptz{Time: time.Now().Add(30 * time.Minute), Valid: true},
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("expected check_violation (23514) for >15min TTL, got %v", err)
	}
}

func TestOctoChatSessionBinding_BothDirections(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)
	inst := newInstallation(t, q, wsID, userID, agentID)
	ctx := context.Background()

	// A chat_session is required (FK). Create a minimal one.
	var sessionID pgtype.UUID
	err := testPool.QueryRow(ctx,
		`INSERT INTO chat_session (workspace_id, agent_id, creator_id) VALUES ($1,$2,$3) RETURNING id`,
		wsID, agentID, userID).Scan(&sessionID)
	if err != nil {
		t.Fatalf("create chat_session: %v", err)
	}

	channelID := "ch_" + randToken()
	_, err = q.CreateOctoChatSessionBinding(ctx, db.CreateOctoChatSessionBindingParams{
		ChatSessionID:   sessionID,
		InstallationID:  inst.ID,
		OctoChannelID:   channelID,
		OctoChannelType: 1,
	})
	if err != nil {
		t.Fatalf("create binding: %v", err)
	}

	// Forward: by (installation, channel).
	fwd, err := q.GetOctoChatSessionBinding(ctx, db.GetOctoChatSessionBindingParams{
		InstallationID: inst.ID, OctoChannelID: channelID,
	})
	if err != nil || fwd.ChatSessionID != sessionID {
		t.Fatalf("forward lookup: %v", err)
	}

	// Reverse: by session.
	rev, err := q.GetOctoChatSessionBindingBySession(ctx, sessionID)
	if err != nil || rev.OctoChannelID != channelID {
		t.Fatalf("reverse lookup: %v", err)
	}
}

// TestOctoWSLease_CAS exercises the compare-and-swap lease predicate that keeps
// exactly one replica consuming an installation's WebSocket. The branches:
//   - empty holder → token A claims it
//   - live holder, different token B → no rows (B is fenced out)
//   - live holder, same token A → renewal succeeds
//   - expired holder → token B takes over
//   - Release with a non-holder token must NOT clear the live holder's lease
func TestOctoWSLease_CAS(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)
	inst := newInstallation(t, q, wsID, userID, agentID)
	ctx := context.Background()

	tokenA := pgtype.Text{String: "lease-A-" + randToken(), Valid: true}
	tokenB := pgtype.Text{String: "lease-B-" + randToken(), Valid: true}
	future := pgtype.Timestamptz{Time: time.Now().Add(30 * time.Second), Valid: true}

	// (a) No current holder → A claims the lease.
	got, err := q.AcquireOctoWSLease(ctx, db.AcquireOctoWSLeaseParams{
		NewToken: tokenA, NewExpiresAt: future, ID: inst.ID,
	})
	if err != nil {
		t.Fatalf("initial acquire by A: %v", err)
	}
	if got.WsLeaseToken != tokenA {
		t.Fatalf("lease token = %v, want A", got.WsLeaseToken)
	}

	// (b) A different replica B cannot steal a live lease → no rows.
	_, err = q.AcquireOctoWSLease(ctx, db.AcquireOctoWSLeaseParams{
		NewToken: tokenB, NewExpiresAt: future, ID: inst.ID,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("B acquiring a live lease: err = %v, want pgx.ErrNoRows", err)
	}

	// (c) The current holder A renews its own lease → succeeds.
	renewed := pgtype.Timestamptz{Time: time.Now().Add(60 * time.Second), Valid: true}
	got, err = q.AcquireOctoWSLease(ctx, db.AcquireOctoWSLeaseParams{
		NewToken: tokenA, NewExpiresAt: renewed, ID: inst.ID,
	})
	if err != nil {
		t.Fatalf("renewal by A: %v", err)
	}
	if got.WsLeaseToken != tokenA {
		t.Errorf("after renewal lease token = %v, want A", got.WsLeaseToken)
	}

	// (d) Force the lease to expire; B may now take over.
	if _, err := testPool.Exec(ctx,
		`UPDATE octo_installation SET ws_lease_expires_at = now() - INTERVAL '1 second'
		 WHERE id = $1`, inst.ID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	got, err = q.AcquireOctoWSLease(ctx, db.AcquireOctoWSLeaseParams{
		NewToken: tokenB, NewExpiresAt: future, ID: inst.ID,
	})
	if err != nil {
		t.Fatalf("B taking over expired lease: %v", err)
	}
	if got.WsLeaseToken != tokenB {
		t.Errorf("after takeover lease token = %v, want B", got.WsLeaseToken)
	}

	// (e) Release with the WRONG token must NOT clear B's live lease.
	if err := q.ReleaseOctoWSLease(ctx, db.ReleaseOctoWSLeaseParams{
		ID: inst.ID, CurrentToken: tokenA,
	}); err != nil {
		t.Fatalf("release with wrong token: %v", err)
	}
	after, err := q.GetOctoInstallation(ctx, inst.ID)
	if err != nil {
		t.Fatalf("reload installation: %v", err)
	}
	if after.WsLeaseToken != tokenB {
		t.Errorf("wrong-token release cleared the live holder's lease: token = %v, want B", after.WsLeaseToken)
	}

	// (f) Release with the correct (holder) token clears the lease.
	if err := q.ReleaseOctoWSLease(ctx, db.ReleaseOctoWSLeaseParams{
		ID: inst.ID, CurrentToken: tokenB,
	}); err != nil {
		t.Fatalf("release with holder token: %v", err)
	}
	cleared, err := q.GetOctoInstallation(ctx, inst.ID)
	if err != nil {
		t.Fatalf("reload after release: %v", err)
	}
	if cleared.WsLeaseToken.Valid {
		t.Errorf("holder release did not clear lease token: %v", cleared.WsLeaseToken)
	}

	// (g) A revoked installation cannot have its lease (re)acquired (status guard).
	if err := q.SetOctoInstallationStatus(ctx, db.SetOctoInstallationStatusParams{
		ID: inst.ID, Status: "revoked",
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := q.AcquireOctoWSLease(ctx, db.AcquireOctoWSLeaseParams{
		NewToken: tokenA, NewExpiresAt: future, ID: inst.ID,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("acquire on revoked installation: err = %v, want pgx.ErrNoRows", err)
	}
}

// TestOctoWSLease_DoesNotBumpUpdatedAt is a regression guard: lease
// acquire/renew and release must NOT advance updated_at. The hub treats an
// advancing updated_at as a reconfigure signal and restarts the supervisor —
// if a lease renewal bumped updated_at, every renewal would look like a
// reconfigure and the connection would churn in a perpetual restart loop,
// never holding the lease (caught in E2E acceptance after the reconfigure fix).
func TestOctoWSLease_DoesNotBumpUpdatedAt(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)
	inst := newInstallation(t, q, wsID, userID, agentID)
	ctx := context.Background()
	token := pgtype.Text{String: "node-1:" + randToken(), Valid: true}
	future := pgtype.Timestamptz{Time: time.Now().Add(90 * time.Second), Valid: true}

	baseline := inst.UpdatedAt.Time

	// Acquire then renew (same token re-acquires) — updated_at must stay put.
	for i := 0; i < 2; i++ {
		if _, err := q.AcquireOctoWSLease(ctx, db.AcquireOctoWSLeaseParams{
			NewToken: token, NewExpiresAt: future, ID: inst.ID,
		}); err != nil {
			t.Fatalf("acquire/renew %d: %v", i, err)
		}
		got, err := q.GetOctoInstallation(ctx, inst.ID)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if !got.UpdatedAt.Time.Equal(baseline) {
			t.Fatalf("acquire/renew advanced updated_at (%v -> %v); the hub would see a phantom reconfigure and restart-loop",
				baseline, got.UpdatedAt.Time)
		}
	}

	// Release must also leave updated_at untouched.
	if err := q.ReleaseOctoWSLease(ctx, db.ReleaseOctoWSLeaseParams{
		ID: inst.ID, CurrentToken: token,
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	got, err := q.GetOctoInstallation(ctx, inst.ID)
	if err != nil {
		t.Fatalf("reload after release: %v", err)
	}
	if !got.UpdatedAt.Time.Equal(baseline) {
		t.Errorf("release advanced updated_at (%v -> %v)", baseline, got.UpdatedAt.Time)
	}
}

// TestOctoUserBinding_NoSteal verifies the identity-binding boundary in
// CreateOctoUserBinding: the same user re-binds idempotently (bound_at bumps),
// a different user CANNOT steal an already-bound uid (zero rows), and a
// non-member redeemer is rejected by the composite member FK.
func TestOctoUserBinding_NoSteal(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userA, agentID := fixture(t)
	inst := newInstallation(t, q, wsID, userA, agentID)
	ctx := context.Background()
	uid := "octo_uid_" + randToken()

	// userB is a second member of the same workspace.
	var userB pgtype.UUID
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (email, name) VALUES ($1, 'User B') RETURNING id`,
		"octo-b-"+randToken()[:8]+"@example.com").Scan(&userB); err != nil {
		t.Fatalf("create userB: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		wsID, userB); err != nil {
		t.Fatalf("add userB as member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userB)
	})

	// (1) Initial bind uid → userA.
	first, err := q.CreateOctoUserBinding(ctx, db.CreateOctoUserBindingParams{
		WorkspaceID: wsID, MulticaUserID: userA, InstallationID: inst.ID, OctoUid: uid,
	})
	if err != nil {
		t.Fatalf("initial bind to userA: %v", err)
	}

	// (2) Same user re-binds idempotently; bound_at must advance.
	again, err := q.CreateOctoUserBinding(ctx, db.CreateOctoUserBindingParams{
		WorkspaceID: wsID, MulticaUserID: userA, InstallationID: inst.ID, OctoUid: uid,
	})
	if err != nil {
		t.Fatalf("idempotent re-bind by userA: %v", err)
	}
	if again.ID != first.ID {
		t.Errorf("re-bind created a new row (%v) instead of updating (%v)", again.ID, first.ID)
	}
	if !again.BoundAt.Time.After(first.BoundAt.Time) {
		t.Errorf("re-bind did not bump bound_at: first=%v again=%v", first.BoundAt.Time, again.BoundAt.Time)
	}

	// (3) A DIFFERENT user cannot steal the already-bound uid → zero rows.
	if _, err := q.CreateOctoUserBinding(ctx, db.CreateOctoUserBindingParams{
		WorkspaceID: wsID, MulticaUserID: userB, InstallationID: inst.ID, OctoUid: uid,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("steal attempt by userB: err = %v, want pgx.ErrNoRows", err)
	}
	// The binding still points at userA.
	bound, err := q.GetOctoUserBindingByUID(ctx, db.GetOctoUserBindingByUIDParams{
		InstallationID: inst.ID, OctoUid: uid,
	})
	if err != nil {
		t.Fatalf("reload binding: %v", err)
	}
	if bound.MulticaUserID != userA {
		t.Errorf("binding was stolen: user = %v, want userA", bound.MulticaUserID)
	}

	// (4) A non-member redeemer is rejected by the composite member FK (23503).
	var outsider pgtype.UUID
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (email, name) VALUES ($1, 'Outsider') RETURNING id`,
		"octo-out-"+randToken()[:8]+"@example.com").Scan(&outsider); err != nil {
		t.Fatalf("create outsider: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, outsider)
	})
	_, err = q.CreateOctoUserBinding(ctx, db.CreateOctoUserBindingParams{
		WorkspaceID: wsID, MulticaUserID: outsider, InstallationID: inst.ID,
		OctoUid: "octo_uid_outsider_" + randToken(),
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Errorf("non-member bind: err = %v, want foreign_key_violation (23503)", err)
	}
}

// TestOctoOutboundMessage_TaskUniqueness covers the partial UNIQUE index
// (task_id) WHERE task_id IS NOT NULL that keeps task↔message 1:1: two rows for
// the same non-null task collide, while multiple NULL-task rows coexist. It also
// exercises GetOctoOutboundMessageByTask and UpdateOctoOutboundMessageStatus.
func TestOctoOutboundMessage_TaskUniqueness(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)
	ctx := context.Background()

	var sessionID pgtype.UUID
	if err := testPool.QueryRow(ctx,
		`INSERT INTO chat_session (workspace_id, agent_id, creator_id) VALUES ($1,$2,$3) RETURNING id`,
		wsID, agentID, userID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat_session: %v", err)
	}
	taskID := newTask(t, wsID, agentID, userID)

	// First row for the task.
	msg, err := q.CreateOctoOutboundMessage(ctx, db.CreateOctoOutboundMessageParams{
		ChatSessionID: sessionID,
		TaskID:        taskID,
		OctoChannelID: "ch_" + randToken(),
		OctoMessageID: "m_" + randToken(),
		Status:        "pending",
	})
	if err != nil {
		t.Fatalf("create first outbound: %v", err)
	}

	// Second row for the SAME task violates the partial unique index (23505).
	_, err = q.CreateOctoOutboundMessage(ctx, db.CreateOctoOutboundMessageParams{
		ChatSessionID: sessionID,
		TaskID:        taskID,
		OctoChannelID: "ch_" + randToken(),
		OctoMessageID: "m_" + randToken(),
		Status:        "pending",
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("duplicate task_id: err = %v, want unique_violation (23505)", err)
	}

	// Two NULL-task rows must coexist (the partial index excludes NULLs).
	for i := 0; i < 2; i++ {
		if _, err := q.CreateOctoOutboundMessage(ctx, db.CreateOctoOutboundMessageParams{
			ChatSessionID: sessionID,
			TaskID:        pgtype.UUID{}, // NULL task_id
			OctoChannelID: "ch_" + randToken(),
			OctoMessageID: "m_" + randToken(),
			Status:        "pending",
		}); err != nil {
			t.Fatalf("create NULL-task outbound #%d: %v", i, err)
		}
	}

	// GetByTask returns the single row for the task.
	got, err := q.GetOctoOutboundMessageByTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetByTask: %v", err)
	}
	if got.ID != msg.ID {
		t.Errorf("GetByTask returned %v, want %v", got.ID, msg.ID)
	}

	// UpdateStatus flips status and stamps last_edited_at.
	if err := q.UpdateOctoOutboundMessageStatus(ctx, db.UpdateOctoOutboundMessageStatusParams{
		ID: msg.ID, Status: "final",
	}); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	updated, err := q.GetOctoOutboundMessageByTask(ctx, taskID)
	if err != nil {
		t.Fatalf("reload after UpdateStatus: %v", err)
	}
	if updated.Status != "final" {
		t.Errorf("status = %q, want final", updated.Status)
	}
	if !updated.LastEditedAt.Valid {
		t.Errorf("last_edited_at not set after UpdateStatus")
	}
}

// TestOctoInboundDrop_AndPurges covers the drop-audit write path (including its
// nullable narg columns) plus the two cutoff-based purge queries.
func TestOctoInboundDrop_AndPurges(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)
	inst := newInstallation(t, q, wsID, userID, agentID)
	ctx := context.Background()

	// Drop with all routing fields populated.
	if err := q.RecordOctoInboundDrop(ctx, db.RecordOctoInboundDropParams{
		DropReason:     "unbound_user",
		InstallationID: inst.ID,
		OctoChannelID:  pgtype.Text{String: "ch_" + randToken(), Valid: true},
		OctoMessageID:  pgtype.Text{String: "m_" + randToken(), Valid: true},
	}); err != nil {
		t.Fatalf("record drop (full): %v", err)
	}
	// Drop with the nullable narg columns omitted (e.g. invalid_event with no IDs).
	if err := q.RecordOctoInboundDrop(ctx, db.RecordOctoInboundDropParams{
		DropReason:     "invalid_event",
		InstallationID: inst.ID,
	}); err != nil {
		t.Fatalf("record drop (nulls): %v", err)
	}
	audits, err := q.ListOctoInboundAuditByInstallation(ctx, db.ListOctoInboundAuditByInstallationParams{
		InstallationID: inst.ID, Limit: 10, Offset: 0,
	})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(audits) != 2 {
		t.Errorf("audit rows = %d, want 2", len(audits))
	}

	// PurgeOctoInboundDedup deletes rows older than the cutoff, keeps fresh ones.
	staleMsg := "msg_stale_" + randToken()
	freshMsg := "msg_fresh_" + randToken()
	if _, err := q.ClaimOctoInboundDedup(ctx, db.ClaimOctoInboundDedupParams{
		InstallationID: inst.ID, MessageID: staleMsg,
	}); err != nil {
		t.Fatalf("claim stale: %v", err)
	}
	if _, err := q.ClaimOctoInboundDedup(ctx, db.ClaimOctoInboundDedupParams{
		InstallationID: inst.ID, MessageID: freshMsg,
	}); err != nil {
		t.Fatalf("claim fresh: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE octo_inbound_dedup SET received_at = now() - INTERVAL '48 hours'
		 WHERE installation_id = $1 AND message_id = $2`, inst.ID, staleMsg); err != nil {
		t.Fatalf("age stale dedup: %v", err)
	}
	if err := q.PurgeOctoInboundDedup(ctx,
		pgtype.Timestamptz{Time: time.Now().Add(-24 * time.Hour), Valid: true}); err != nil {
		t.Fatalf("purge dedup: %v", err)
	}
	// Stale row gone (re-claimable), fresh row still blocks a replay.
	if _, err := q.ClaimOctoInboundDedup(ctx, db.ClaimOctoInboundDedupParams{
		InstallationID: inst.ID, MessageID: staleMsg,
	}); err != nil {
		t.Errorf("stale dedup row not purged (reclaim failed): %v", err)
	}
	if _, err := q.ClaimOctoInboundDedup(ctx, db.ClaimOctoInboundDedupParams{
		InstallationID: inst.ID, MessageID: freshMsg,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("fresh dedup row wrongly purged: err = %v, want pgx.ErrNoRows", err)
	}

	// PurgeExpiredOctoBindingTokens deletes lapsed tokens, keeps live ones.
	liveHash := "hash_live_" + randToken()
	expHash := "hash_exp_" + randToken()
	for _, h := range []string{liveHash, expHash} {
		if _, err := q.CreateOctoBindingToken(ctx, db.CreateOctoBindingTokenParams{
			TokenHash:      h,
			WorkspaceID:    wsID,
			InstallationID: inst.ID,
			OctoUid:        "uid_x",
			ExpiresAt:      pgtype.Timestamptz{Time: time.Now().Add(10 * time.Minute), Valid: true},
		}); err != nil {
			t.Fatalf("create token %s: %v", h, err)
		}
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE octo_binding_token SET expires_at = now() - INTERVAL '1 minute'
		 WHERE token_hash = $1`, expHash); err != nil {
		t.Fatalf("expire token: %v", err)
	}
	if err := q.PurgeExpiredOctoBindingTokens(ctx,
		pgtype.Timestamptz{Time: time.Now(), Valid: true}); err != nil {
		t.Fatalf("purge tokens: %v", err)
	}
	var remaining int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM octo_binding_token WHERE installation_id = $1`, inst.ID).Scan(&remaining); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if remaining != 1 {
		t.Errorf("tokens remaining = %d, want 1 (only the live one)", remaining)
	}
}

// newTask creates a minimal issue + agent_task_queue row and returns the task id.
func newTask(t *testing.T, wsID, agentID, userID pgtype.UUID) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var issueID pgtype.UUID
	if err := testPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		 VALUES ($1, 'Octo Task Issue', 'member', $2) RETURNING id`,
		wsID, userID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	var taskID pgtype.UUID
	// agent_task_queue.runtime_id is NOT NULL; reuse the agent's runtime
	// (the fixture wires every agent to one) so the insert satisfies the
	// constraint.
	var runtimeID pgtype.UUID
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load agent runtime_id: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id) VALUES ($1, $2, $3) RETURNING id`,
		agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("create agent_task_queue: %v", err)
	}
	return taskID
}

// randUUID returns a random pgtype.UUID for token-mismatch tests.
func randUUID() pgtype.UUID {
	var u pgtype.UUID
	_, _ = rand.Read(u.Bytes[:])
	u.Valid = true
	return u
}
