package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestOctoCleanupJob_Spec validates the static job configuration so a typo in
// cadence/timeouts is caught without a database.
func TestOctoCleanupJob_Spec(t *testing.T) {
	spec := OctoCleanupJob(nil)
	if spec.Name != JobNameOctoCleanup {
		t.Errorf("Name = %q, want %q", spec.Name, JobNameOctoCleanup)
	}
	if spec.Handler == nil {
		t.Error("Handler is nil")
	}
	if spec.Cadence <= 0 || spec.RunTimeout <= 0 {
		t.Errorf("non-positive cadence/timeout: cadence=%v runTimeout=%v", spec.Cadence, spec.RunTimeout)
	}
}

// TestOctoCleanupHandler_PurgesExpiredAndStale inserts an expired binding token
// and an aged dedup row, runs the handler, and asserts both are deleted while a
// fresh token and a recent dedup row survive.
func TestOctoCleanupHandler_PurgesExpiredAndStale(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()

	wsID, _, instID := octoCleanupFixture(t, pool)
	now := time.Now()

	// Expired token (purged) + fresh token (survives).
	expiredHash := "hash_expired_" + randHex(t, pool)
	freshHash := "hash_fresh_" + randHex(t, pool)
	insertBindingToken(t, pool, expiredHash, wsID, instID, "uid_exp", now.Add(-time.Minute))
	insertBindingToken(t, pool, freshHash, wsID, instID, "uid_fresh", now.Add(10*time.Minute))

	// Aged dedup row (purged) + recent dedup row (survives).
	oldMsg := "msg_old_" + randHex(t, pool)
	newMsg := "msg_new_" + randHex(t, pool)
	insertDedup(t, pool, instID, oldMsg, now.Add(-48*time.Hour))
	insertDedup(t, pool, instID, newMsg, now.Add(-time.Minute))

	if _, err := makeOctoCleanupHandler(pool)(ctx, HandlerInput{PlanTime: now}); err != nil {
		t.Fatalf("handler: %v", err)
	}

	if n := scalarInt(t, pool, `SELECT count(*) FROM octo_binding_token WHERE installation_id=$1`, instID); n != 1 {
		t.Errorf("binding tokens after purge = %d, want 1 (only the fresh one)", n)
	}
	if scalarInt(t, pool, `SELECT count(*) FROM octo_binding_token WHERE token_hash=$1`, freshHash) != 1 {
		t.Error("fresh token was wrongly purged")
	}
	if n := scalarInt(t, pool, `SELECT count(*) FROM octo_inbound_dedup WHERE installation_id=$1`, instID); n != 1 {
		t.Errorf("dedup rows after purge = %d, want 1 (only the recent one)", n)
	}
	if scalarInt(t, pool, `SELECT count(*) FROM octo_inbound_dedup WHERE installation_id=$1 AND message_id=$2`, instID, newMsg) != 1 {
		t.Error("recent dedup row was wrongly purged")
	}

	// Sanity: the handler also goes through the generated queries path.
	_ = db.New(pool)
}

// octoCleanupFixture creates a workspace + user + membership + runtime + agent +
// installation the binding-token/dedup rows can reference, with cleanup.
func octoCleanupFixture(t *testing.T, pool *pgxpool.Pool) (wsID, userID, instID pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug) VALUES ('Octo Cleanup WS','octo-cleanup-'||substr(md5(random()::text),1,8)) RETURNING id`).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id=$1`, wsID) })
	if err := pool.QueryRow(ctx,
		`INSERT INTO "user" (email, name) VALUES ('octo-cleanup-'||substr(md5(random()::text),1,8)||'@example.com','U') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, userID) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1,$2,'owner')`, wsID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	var runtimeID, agentID pgtype.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider) VALUES ($1,'rt','local','octo_test') RETURNING id`, wsID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id) VALUES ($1,'a','local',$2) RETURNING id`, wsID, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO octo_installation (workspace_id, agent_id, bot_token_encrypted, robot_id, api_url, installer_user_id)
		 VALUES ($1,$2,'\x00','robot_cleanup_'||substr(md5(random()::text),1,8),'https://im.example/api',$3) RETURNING id`,
		wsID, agentID, userID).Scan(&instID); err != nil {
		t.Fatalf("create installation: %v", err)
	}
	return wsID, userID, instID
}

func insertBindingToken(t *testing.T, pool *pgxpool.Pool, hash string, wsID, instID pgtype.UUID, uid string, expiresAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO octo_binding_token (token_hash, workspace_id, installation_id, octo_uid, expires_at) VALUES ($1,$2,$3,$4,$5)`,
		hash, wsID, instID, uid, expiresAt); err != nil {
		t.Fatalf("insert binding token: %v", err)
	}
}

func insertDedup(t *testing.T, pool *pgxpool.Pool, instID pgtype.UUID, msgID string, receivedAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO octo_inbound_dedup (installation_id, message_id, received_at, claim_token) VALUES ($1,$2,$3,gen_random_uuid())`,
		instID, msgID, receivedAt); err != nil {
		t.Fatalf("insert dedup: %v", err)
	}
}

func scalarInt(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("scalarInt: %v", err)
	}
	return n
}

func randHex(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(context.Background(), `SELECT substr(md5(random()::text),1,12)`).Scan(&s); err != nil {
		t.Fatalf("randHex: %v", err)
	}
	return s
}
