package octo_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

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
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping octo DB tests: cannot connect: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping octo DB tests: database not reachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}
	testPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

func randToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// fixture creates a throwaway workspace + user + member + agent and returns
// their IDs, registering cleanup that cascades everything away.
func fixture(t *testing.T, q *db.Queries) (workspaceID, userID, agentID pgtype.UUID) {
	t.Helper()
	ctx := context.Background()

	var wsID, uID, aID pgtype.UUID
	slug := "octo-test-" + randToken()[:8]
	email := "octo-test-" + randToken()[:8] + "@example.com"

	// workspace
	err := testPool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug) VALUES ($1, $2) RETURNING id`,
		"Octo Test WS", slug).Scan(&wsID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	// user
	err = testPool.QueryRow(ctx,
		`INSERT INTO "user" (email, name) VALUES ($1, $2) RETURNING id`,
		email, "Octo Tester").Scan(&uID)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	// member
	_, err = testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		wsID, uID)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	// agent requires runtime_mode and a NOT NULL runtime_id (migration 004),
	// so create an agent_runtime first.
	var runtimeID pgtype.UUID
	err = testPool.QueryRow(ctx,
		`INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider)
		 VALUES ($1, 'Octo Runtime', 'local', 'octo_test') RETURNING id`,
		wsID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create agent_runtime: %v", err)
	}
	err = testPool.QueryRow(ctx,
		`INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		 VALUES ($1, $2, 'local', $3) RETURNING id`,
		wsID, "Octo Agent", runtimeID).Scan(&aID)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	t.Cleanup(func() {
		// Deleting the workspace cascades to member/agent/installation/etc.
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, uID)
	})
	return wsID, uID, aID
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
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t, q)

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
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t, q)

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
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t, q)
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
	if err == nil {
		t.Errorf("second concurrent claim should return no rows, got success")
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
	if err == nil {
		t.Errorf("claim after terminal mark should return no rows")
	}
}

func TestOctoInboundDedup_ReleaseAllowsReclaim(t *testing.T) {
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t, q)
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

func TestOctoBindingToken_ConsumeOnce(t *testing.T) {
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t, q)
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
	if _, err := q.ConsumeOctoBindingToken(ctx, hash); err == nil {
		t.Errorf("second consume should fail (already consumed)")
	}
}

func TestOctoBindingToken_TTLCapRejected(t *testing.T) {
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t, q)
	inst := newInstallation(t, q, wsID, userID, agentID)
	ctx := context.Background()

	// expires_at beyond the 15-minute DB CHECK cap must be rejected.
	_, err := q.CreateOctoBindingToken(ctx, db.CreateOctoBindingTokenParams{
		TokenHash:      "hash_" + randToken(),
		WorkspaceID:    wsID,
		InstallationID: inst.ID,
		OctoUid:        "uid_x",
		ExpiresAt:      pgtype.Timestamptz{Time: time.Now().Add(30 * time.Minute), Valid: true},
	})
	if err == nil {
		t.Errorf("expected CHECK violation for >15min TTL, got nil")
	}
}

func TestOctoChatSessionBinding_BothDirections(t *testing.T) {
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t, q)
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

// randUUID returns a random pgtype.UUID for token-mismatch tests.
func randUUID() pgtype.UUID {
	var u pgtype.UUID
	_, _ = rand.Read(u.Bytes[:])
	u.Valid = true
	return u
}
