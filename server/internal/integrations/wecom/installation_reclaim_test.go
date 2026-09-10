package wecom

// installation_reclaim_test.go — DB-backed guards for the reclaim-then-upsert
// slot recovery (the fix for the "a disconnected bot can never be moved to
// another agent" review item). These need Postgres: they exercise the real
// ReclaimDeadChannelInstallationByAppID DELETE against channel_installation +
// workspace + agent rows, so they skip when no migrated database is reachable
// (they run in CI, which has one). Mirrors lark/channel_store_rebind_test.go.

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	wcRclWS       = "5c09e200-0000-4000-8000-000000000001"
	wcRclRuntime  = "5c09e200-0000-4000-8000-000000000003"
	wcRclAgentA   = "5c09e200-0000-4000-8000-00000000000a"
	wcRclAgentB   = "5c09e200-0000-4000-8000-00000000000b"
	wcRclAgentArc = "5c09e200-0000-4000-8000-00000000000c"
	wcRclInstall  = "5c09e200-0000-4000-8000-000000000005"

	wcRclBotRevoked  = "bot_rcl_revoked"
	wcRclBotLive     = "bot_rcl_live"
	wcRclBotArchived = "bot_rcl_archived"
	wcRclBotSame     = "bot_rcl_same"
)

func reclaimTestDB(t *testing.T) *pgxpool.Pool {
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
	var present bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.channel_installation') IS NOT NULL").Scan(&present); err != nil || !present {
		pool.Close()
		t.Skip("channel_installation not present (database not migrated)")
	}
	t.Cleanup(pool.Close)
	return pool
}

// newReclaimSvc builds the service with a probe that always succeeds. The
// probe is mandatory (see credential_probe.go) and these tests are about the
// reclaim, not the proof of control, so the fake stands in for "the caller does
// hold this bot's secret". The returned fake lets a test assert how many times
// WeCom would have been contacted.
func newReclaimSvc(t *testing.T, pool *pgxpool.Pool) (*InstallationService, *fakeProbe) {
	t.Helper()
	box, err := secretbox.New(make([]byte, secretbox.KeySize))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	probe := &fakeProbe{}
	svc, err := NewInstallationService(db.New(pool), pool, box, WithCredentialProbe(probe))
	if err != nil {
		t.Fatalf("NewInstallationService: %v", err)
	}
	return svc, probe
}

func seedReclaimOwners(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(q string, args ...any) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed reclaim owner: %v", err)
		}
	}
	exec(`INSERT INTO workspace (id, name, slug, description) VALUES ($1, 'wecom reclaim', 'wecom-reclaim', '') ON CONFLICT (id) DO NOTHING`, wcRclWS)
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider)
VALUES ($1, $2, 'wecom reclaim runtime', 'local', 'multica_daemon') ON CONFLICT (id) DO NOTHING`, wcRclRuntime, wcRclWS)
	for _, a := range []string{wcRclAgentA, wcRclAgentB} {
		exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id)
VALUES ($1, $2, $3, 'local', $4) ON CONFLICT (id) DO NOTHING`, a, wcRclWS, "wecom reclaim "+a, wcRclRuntime)
	}
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id, archived_at)
VALUES ($1, $2, $3, 'local', $4, now()) ON CONFLICT (id) DO NOTHING`, wcRclAgentArc, wcRclWS, "wecom reclaim archived", wcRclRuntime)
}

func cleanReclaim(ctx context.Context, pool *pgxpool.Pool) {
	apps := []string{wcRclBotRevoked, wcRclBotLive, wcRclBotArchived, wcRclBotSame}
	_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE config->>'app_id' = ANY($1)`, apps)
	_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wcRclWS)
}

func uuid(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %s: %v", s, err)
	}
	return u
}

func insertWecomInstall(t *testing.T, ctx context.Context, pool *pgxpool.Pool, app, agent, status string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES ($1, $2, 'wecom', jsonb_build_object('app_id', $3::text, 'bot_id', $3::text, 'app_secret_encrypted', ''), $4, $5)`,
		wcRclWS, agent, app, wcRclInstall, status); err != nil {
		t.Fatalf("insert install app=%s status=%s: %v", app, status, err)
	}
}

func params(app, agent string) InstallationParams {
	return InstallationParams{
		WorkspaceID:     mustUUID(wcRclWS),
		AgentID:         mustUUID(agent),
		InstallerUserID: mustUUID(wcRclInstall),
		BotID:           app,
		Secret:          "s3cr3t",
	}
}

func mustUUID(s string) pgtype.UUID {
	u, _ := util.ParseUUID(s)
	return u
}

func setupReclaim(t *testing.T) (context.Context, *pgxpool.Pool, *InstallationService, *fakeProbe) {
	t.Helper()
	pool := reclaimTestDB(t)
	ctx := context.Background()
	cleanReclaim(ctx, pool)
	seedReclaimOwners(t, ctx, pool)
	t.Cleanup(func() { cleanReclaim(ctx, pool) })
	svc, probe := newReclaimSvc(t, pool)
	return ctx, pool, svc, probe
}

// A revoked owner on a DIFFERENT agent must be reclaimed so the bot can move.
func TestUpsert_TakesOverRevokedOwnerOnDifferentAgent(t *testing.T) {
	ctx, pool, svc, probe := setupReclaim(t)
	insertWecomInstall(t, ctx, pool, wcRclBotRevoked, wcRclAgentA, "revoked")

	got, err := svc.Upsert(ctx, params(wcRclBotRevoked, wcRclAgentB))
	if err != nil {
		t.Fatalf("takeover of a revoked owner should succeed, got: %v", err)
	}
	if got.AgentID != uuid(t, wcRclAgentB) {
		t.Errorf("new owner agent = %v, want agent B", got.AgentID)
	}
	// The dead row for agent A must be gone (slot freed, not double-owned).
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM channel_installation WHERE config->>'app_id' = $1`, wcRclBotRevoked).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("expected exactly one row for the bot after takeover, got %d", n)
	}
	// Taking over a revoked row hard-deletes it and every binding beneath, so
	// it is exactly the act that has to be preceded by proof of control.
	if n := probe.callCount(); n != 1 {
		t.Errorf("probe called %d time(s) before reclaiming a revoked owner, want 1", n)
	}
}

// A LIVE (active) owner on another agent must still be refused with an accurate
// same-workspace conflict — reclaim must not steal a live slot.
func TestUpsert_RefusesLiveOwnerOnDifferentAgent(t *testing.T) {
	ctx, pool, svc, probe := setupReclaim(t)
	insertWecomInstall(t, ctx, pool, wcRclBotLive, wcRclAgentA, "active")

	_, err := svc.Upsert(ctx, params(wcRclBotLive, wcRclAgentB))
	if !errors.Is(err, ErrBotOwnedBySameWorkspace) {
		t.Fatalf("live owner should be refused with ErrBotOwnedBySameWorkspace, got: %v", err)
	}
	if n := probe.callCount(); n != 0 {
		t.Errorf("probe called %d time(s) on a refused install; a live owner is decided from the row, without touching WeCom", n)
	}
}

// An ARCHIVED agent is still a live owner (not dead) — reclaim leaves it, and
// the conflict is reported as the archived-agent case.
func TestUpsert_RefusesArchivedOwner(t *testing.T) {
	ctx, pool, svc, probe := setupReclaim(t)
	insertWecomInstall(t, ctx, pool, wcRclBotArchived, wcRclAgentArc, "active")

	_, err := svc.Upsert(ctx, params(wcRclBotArchived, wcRclAgentB))
	if !errors.Is(err, ErrBotOwnedByArchivedAgent) {
		t.Fatalf("archived owner should be refused with ErrBotOwnedByArchivedAgent, got: %v", err)
	}
	if n := probe.callCount(); n != 0 {
		t.Errorf("probe called %d time(s) on a refused install; an archived owner is decided from the row, without touching WeCom", n)
	}
}

// Reconnecting the same bot to the SAME agent is an in-place refresh, never a
// conflict (it hits the (workspace, agent, channel_type) ON CONFLICT).
func TestUpsert_SameAgentReconnectInPlace(t *testing.T) {
	ctx, pool, svc, probe := setupReclaim(t)
	insertWecomInstall(t, ctx, pool, wcRclBotSame, wcRclAgentA, "revoked")

	got, err := svc.Upsert(ctx, params(wcRclBotSame, wcRclAgentA))
	if err != nil {
		t.Fatalf("same-agent reconnect should succeed, got: %v", err)
	}
	if got.Status != InstallationActive {
		t.Errorf("status after reconnect = %q, want active", got.Status)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM channel_installation WHERE config->>'app_id' = $1`, wcRclBotSame).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("expected one row after in-place reconnect, got %d", n)
	}
	// The caller's own row: still probed, because this is the secret-rotation
	// path and the only connection a subscribe can displace is their own.
	if n := probe.callCount(); n != 1 {
		t.Errorf("probe called %d time(s) on a same-agent reconnect, want 1", n)
	}
}
