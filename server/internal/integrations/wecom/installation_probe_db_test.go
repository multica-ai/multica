package wecom

// installation_probe_db_test.go — the guards on WHEN the credential probe is
// allowed to run.
//
// The probe is not a read. It subscribes, and WeCom allows exactly one live
// subscriber per bot, so subscribing displaces whoever is connected. Running it
// before knowing who owns the bot's routing slot means a request that is about
// to be REFUSED still knocks the rightful owner offline first: their supervisor
// reconnects, the caller repeats the request, and an install nobody is
// permitted to make becomes a denial of service against a live bot.
//
// So these assert a count of zero. Every conflict — another agent here, an
// archived agent, another workspace — must be decided from the row, with
// nothing sent to WeCom and nothing written locally.
//
// DB-backed because the decision reads channel_installation JOINed to workspace
// and agent under a transaction-level advisory lock; a fake queries layer would
// assert the mock, not the behaviour. They skip when no migrated database is
// reachable (CI has one). Fixtures are disjoint from installation_reclaim_test.go.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	wcPrbWS       = "5c09e201-0000-4000-8000-000000000001"
	wcPrbOtherWS  = "5c09e201-0000-4000-8000-000000000002"
	wcPrbRuntime  = "5c09e201-0000-4000-8000-000000000003"
	wcPrbOtherRT  = "5c09e201-0000-4000-8000-000000000004"
	wcPrbAgentA   = "5c09e201-0000-4000-8000-00000000000a"
	wcPrbAgentB   = "5c09e201-0000-4000-8000-00000000000b"
	wcPrbAgentArc = "5c09e201-0000-4000-8000-00000000000c"
	wcPrbAgentOth = "5c09e201-0000-4000-8000-00000000000d"
	wcPrbInstall  = "5c09e201-0000-4000-8000-000000000005"

	wcPrbBotActive   = "bot_prb_active"
	wcPrbBotArchived = "bot_prb_archived"
	wcPrbBotForeign  = "bot_prb_foreign"
	wcPrbBotRevoked  = "bot_prb_revoked"
	wcPrbBotFree     = "bot_prb_free"
)

func newProbeSvc(t *testing.T, pool *pgxpool.Pool, probe CredentialProbe) *InstallationService {
	t.Helper()
	box, err := secretbox.New(make([]byte, secretbox.KeySize))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	svc, err := NewInstallationService(db.New(pool), pool, box, WithCredentialProbe(probe))
	if err != nil {
		t.Fatalf("NewInstallationService: %v", err)
	}
	return svc
}

func seedProbeOwners(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(q string, args ...any) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed probe owner: %v", err)
		}
	}
	exec(`INSERT INTO workspace (id, name, slug, description) VALUES ($1, 'wecom probe', 'wecom-probe', '') ON CONFLICT (id) DO NOTHING`, wcPrbWS)
	exec(`INSERT INTO workspace (id, name, slug, description) VALUES ($1, 'wecom probe other', 'wecom-probe-other', '') ON CONFLICT (id) DO NOTHING`, wcPrbOtherWS)
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider)
VALUES ($1, $2, 'wecom probe runtime', 'local', 'multica_daemon') ON CONFLICT (id) DO NOTHING`, wcPrbRuntime, wcPrbWS)
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider)
VALUES ($1, $2, 'wecom probe other runtime', 'local', 'multica_daemon') ON CONFLICT (id) DO NOTHING`, wcPrbOtherRT, wcPrbOtherWS)
	for _, a := range []string{wcPrbAgentA, wcPrbAgentB} {
		exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id)
VALUES ($1, $2, $3, 'local', $4) ON CONFLICT (id) DO NOTHING`, a, wcPrbWS, "wecom probe "+a, wcPrbRuntime)
	}
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id, archived_at)
VALUES ($1, $2, $3, 'local', $4, now()) ON CONFLICT (id) DO NOTHING`, wcPrbAgentArc, wcPrbWS, "wecom probe archived", wcPrbRuntime)
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id)
VALUES ($1, $2, $3, 'local', $4) ON CONFLICT (id) DO NOTHING`, wcPrbAgentOth, wcPrbOtherWS, "wecom probe foreign", wcPrbOtherRT)
}

func cleanProbe(ctx context.Context, pool *pgxpool.Pool) {
	apps := []string{wcPrbBotActive, wcPrbBotArchived, wcPrbBotForeign, wcPrbBotRevoked, wcPrbBotFree}
	_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE config->>'app_id' = ANY($1)`, apps)
	_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = ANY($1)`, []string{wcPrbWS, wcPrbOtherWS})
}

func insertProbeInstall(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ws, agent, app, status string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES ($1, $2, 'wecom', jsonb_build_object('app_id', $3::text, 'bot_id', $3::text, 'app_secret_encrypted', ''), $4, $5)`,
		ws, agent, app, wcPrbInstall, status); err != nil {
		t.Fatalf("insert install app=%s status=%s: %v", app, status, err)
	}
}

func probeParams(ws, agent, app string) InstallationParams {
	return InstallationParams{
		WorkspaceID:     mustUUID(ws),
		AgentID:         mustUUID(agent),
		InstallerUserID: mustUUID(wcPrbInstall),
		BotID:           app,
		Secret:          "the-callers-guess",
	}
}

func setupProbe(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	pool := reclaimTestDB(t)
	ctx := context.Background()
	cleanProbe(ctx, pool)
	seedProbeOwners(t, ctx, pool)
	t.Cleanup(func() { cleanProbe(ctx, pool) })
	return ctx, pool
}

// installationRowID reads the primary key currently holding a bot's slot, so a
// test can prove the victim's row is the SAME row afterwards — not a
// replacement written by the reclaim.
func installationRowID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, app string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM channel_installation WHERE config->>'app_id' = $1`, app).Scan(&id); err != nil {
		t.Fatalf("read installation id for %s: %v", app, err)
	}
	return id
}

// The one this PR is for. A live owner holds the bot; somebody else installs it.
// The request must be refused having sent NOTHING to WeCom: one subscribe is
// enough to displace the rightful owner's connection, and a request that is
// refused anyway has no business producing an external side effect.
func TestUpsert_DoesNotProbeWhenAnActiveOwnerHoldsTheBot(t *testing.T) {
	ctx, pool := setupProbe(t)
	insertProbeInstall(t, ctx, pool, wcPrbWS, wcPrbAgentA, wcPrbBotActive, "active")
	victim := installationRowID(t, ctx, pool, wcPrbBotActive)

	// The correct secret, deliberately: the point is that even a caller who
	// WOULD pass the probe must not reach it, because they still lose on
	// ownership and the subscribe would have kicked the live bot for nothing.
	probe := &fakeProbe{}
	svc := newProbeSvc(t, pool, probe)

	_, err := svc.Upsert(ctx, probeParams(wcPrbWS, wcPrbAgentB, wcPrbBotActive))
	if !errors.Is(err, ErrBotOwnedBySameWorkspace) {
		t.Fatalf("Upsert = %v, want ErrBotOwnedBySameWorkspace", err)
	}
	if n := probe.callCount(); n != 0 {
		t.Fatalf("the probe subscribed %d time(s) on a refused install — WeCom allows one live subscriber per bot, so this disconnects the rightful owner and a repeat is a denial of service against a bot the caller may not touch", n)
	}
	if got := installationRowID(t, ctx, pool, wcPrbBotActive); got != victim {
		t.Fatalf("the live owner's installation row changed (%s → %s); a refused install must not touch it", victim, got)
	}
}

// Same rule for an archived owner: archive is reversible, the bot stays owned,
// and the row still names a bot that may be connected.
func TestUpsert_DoesNotProbeWhenAnArchivedOwnerHoldsTheBot(t *testing.T) {
	ctx, pool := setupProbe(t)
	insertProbeInstall(t, ctx, pool, wcPrbWS, wcPrbAgentArc, wcPrbBotArchived, "active")

	probe := &fakeProbe{}
	svc := newProbeSvc(t, pool, probe)

	_, err := svc.Upsert(ctx, probeParams(wcPrbWS, wcPrbAgentB, wcPrbBotArchived))
	if !errors.Is(err, ErrBotOwnedByArchivedAgent) {
		t.Fatalf("Upsert = %v, want ErrBotOwnedByArchivedAgent", err)
	}
	if n := probe.callCount(); n != 0 {
		t.Fatalf("the probe subscribed %d time(s) against an archived owner's bot; a refused install must reach WeCom zero times", n)
	}
}

// And for an owner in a workspace the caller cannot see — the case where the
// caller has no way to even know what they just disconnected.
func TestUpsert_DoesNotProbeWhenAnotherWorkspaceHoldsTheBot(t *testing.T) {
	ctx, pool := setupProbe(t)
	insertProbeInstall(t, ctx, pool, wcPrbOtherWS, wcPrbAgentOth, wcPrbBotForeign, "active")
	victim := installationRowID(t, ctx, pool, wcPrbBotForeign)

	probe := &fakeProbe{}
	svc := newProbeSvc(t, pool, probe)

	_, err := svc.Upsert(ctx, probeParams(wcPrbWS, wcPrbAgentB, wcPrbBotForeign))
	if !errors.Is(err, ErrBotOwnedByAnotherWorkspace) {
		t.Fatalf("Upsert = %v, want ErrBotOwnedByAnotherWorkspace", err)
	}
	if n := probe.callCount(); n != 0 {
		t.Fatalf("the probe subscribed %d time(s) against a bot owned by another workspace; a refused install must reach WeCom zero times", n)
	}
	if got := installationRowID(t, ctx, pool, wcPrbBotForeign); got != victim {
		t.Fatalf("the other workspace's installation row changed (%s → %s)", victim, got)
	}
}

// The probe still runs where it is meant to: a revoked owner's row is what
// ReclaimDeadChannelInstallationByAppID hard-deletes, bindings and all, so
// taking it over is exactly the act that needs proof of control. A rejected
// pair leaves the row standing.
func TestUpsert_ProbesAndRefusesBeforeReclaimingARevokedOwner(t *testing.T) {
	ctx, pool := setupProbe(t)
	insertProbeInstall(t, ctx, pool, wcPrbWS, wcPrbAgentA, wcPrbBotRevoked, "revoked")
	victim := installationRowID(t, ctx, pool, wcPrbBotRevoked)

	probe := &fakeProbe{err: ErrCredentialsRejected}
	svc := newProbeSvc(t, pool, probe)

	_, err := svc.Upsert(ctx, probeParams(wcPrbWS, wcPrbAgentB, wcPrbBotRevoked))
	if !errors.Is(err, ErrCredentialsRejected) {
		t.Fatalf("Upsert = %v, want ErrCredentialsRejected", err)
	}
	if n := probe.callCount(); n != 1 {
		t.Fatalf("probe called %d time(s), want 1 — a revoked row is the case proof of control exists for", n)
	}
	if got := installationRowID(t, ctx, pool, wcPrbBotRevoked); got != victim {
		t.Fatalf("the revoked owner's row was destroyed on a rejected install (%s → %s)", victim, got)
	}
}

// A free slot: probe once, then install.
func TestUpsert_ProbesOnceOnAFreeSlot(t *testing.T) {
	ctx, pool := setupProbe(t)

	probe := &fakeProbe{}
	svc := newProbeSvc(t, pool, probe)

	got, err := svc.Upsert(ctx, probeParams(wcPrbWS, wcPrbAgentA, wcPrbBotFree))
	if err != nil {
		t.Fatalf("Upsert on a free slot = %v, want success", err)
	}
	if got.Status != InstallationActive {
		t.Fatalf("status = %q, want active", got.Status)
	}
	if n := probe.callCount(); n != 1 {
		t.Fatalf("probe called %d time(s), want 1", n)
	}
}

// The serialization boundary. Two installs race for the same bot: the winner is
// inside the probe, holding the slot's advisory lock, when the loser arrives.
// The loser must WAIT for the lock rather than read a stale "nobody owns this"
// and subscribe — a second subscribe would displace the winner's connection at
// WeCom, and the loser is going to be refused anyway. A pre-read outside the
// transaction leaves exactly this window open.
func TestUpsert_ConcurrentInstallDoesNotSubscribeBehindTheWinner(t *testing.T) {
	ctx, pool := setupProbe(t)

	winnerProbe := &fakeProbe{entered: make(chan struct{}), release: make(chan struct{})}
	loserProbe := &fakeProbe{}
	winner := newProbeSvc(t, pool, winnerProbe)
	loser := newProbeSvc(t, pool, loserProbe)

	var wg sync.WaitGroup
	winnerErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := winner.Upsert(ctx, probeParams(wcPrbWS, wcPrbAgentA, wcPrbBotFree))
		winnerErr <- err
	}()

	select {
	case <-winnerProbe.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first install never reached the probe")
	}

	loserErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := loser.Upsert(ctx, probeParams(wcPrbWS, wcPrbAgentB, wcPrbBotFree))
		loserErr <- err
	}()

	// Give the second install every chance to overtake. It must be parked on
	// the advisory lock, not talking to WeCom.
	time.Sleep(500 * time.Millisecond)
	if n := loserProbe.callCount(); n != 0 {
		t.Errorf("the racing install subscribed %d time(s) while another install held the bot's slot — that displaces the connection the winner is about to establish", n)
	}

	close(winnerProbe.release)

	if err := <-winnerErr; err != nil {
		t.Fatalf("first install = %v, want success", err)
	}
	select {
	case err := <-loserErr:
		if !errors.Is(err, ErrBotOwnedBySameWorkspace) {
			t.Fatalf("racing install = %v, want ErrBotOwnedBySameWorkspace", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the racing install never returned")
	}
	wg.Wait()

	if n := loserProbe.callCount(); n != 0 {
		t.Fatalf("the racing install subscribed %d time(s) in total; a refused install must reach WeCom zero times", n)
	}
	if n := winnerProbe.callCount(); n != 1 {
		t.Fatalf("the winning install probed %d time(s), want 1", n)
	}
}
