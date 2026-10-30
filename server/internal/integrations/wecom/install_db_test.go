package wecom

// install_db_test.go — the scan-install guarantees that live in SQL, run against
// a real Postgres.
//
// install_test.go drives the service and worker against an in-memory store, so
// everything it proves is about the Go code's decisions. What the Go code relies
// on and cannot prove for itself is in the queries: the per-workspace advisory
// lock that serializes begin, the FOR UPDATE SKIP LOCKED claim, the lease
// predicate that makes a live claim unclaimable, and the lease_token match on
// every mutation that stops a worker whose lease expired from writing over the
// replica that now owns the session. A fake answers whatever the test tells it
// to; only the database can show those clauses do what the comments say.
//
// They skip when no migrated database is reachable (CI has one). Fixtures are
// disjoint from the other DB tests in this package.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	wcScanWS      = "5c0a1500-0000-4000-8000-000000000001"
	wcScanRuntime = "5c0a1500-0000-4000-8000-000000000002"
	wcScanUser    = "5c0a1500-0000-4000-8000-000000000003"
	// Agents are numbered from here: wcScanAgent(0), wcScanAgent(1), ...
	wcScanAgentBase = "5c0a1500-0000-4000-8000-0000000001%02x"
)

func wcScanAgent(n int) string { return fmt.Sprintf(wcScanAgentBase, n) }

// setupScanDB returns a pool with the fixture workspace and n agents seeded and
// every install session in that workspace removed.
//
// The claim query is not workspace-scoped — a worker takes the oldest due
// session anywhere — so a live session left in the table by something else would
// be claimed by these tests in place of their own. That is checked rather than
// cleaned up: deleting another test's rows would hide its leak.
func setupScanDB(t *testing.T, agents int) (context.Context, *pgxpool.Pool) {
	t.Helper()
	pool := reclaimTestDB(t)
	ctx := context.Background()
	var present bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.wecom_install_session') IS NOT NULL").Scan(&present); err != nil || !present {
		t.Skip("wecom_install_session not present (database not migrated)")
	}
	cleanScan := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM wecom_install_session WHERE workspace_id = $1`, wcScanWS)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE workspace_id = $1`, wcScanWS)
		_, _ = pool.Exec(ctx, `DELETE FROM agent WHERE workspace_id = $1`, wcScanWS)
		_, _ = pool.Exec(ctx, `DELETE FROM agent_runtime WHERE workspace_id = $1`, wcScanWS)
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wcScanWS)
	}
	cleanScan()
	t.Cleanup(cleanScan)

	var foreign int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wecom_install_session WHERE status IN ('creating', 'pending')`).Scan(&foreign); err != nil {
		t.Fatalf("count live sessions: %v", err)
	}
	if foreign != 0 {
		t.Fatalf("%d live install session(s) outside this test's workspace; the claim would take them instead of this test's own", foreign)
	}

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	exec(`INSERT INTO workspace (id, name, slug, description) VALUES ($1, 'wecom scan', 'wecom-scan', '')`, wcScanWS)
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider)
VALUES ($1, $2, 'wecom scan runtime', 'local', 'multica_daemon')`, wcScanRuntime, wcScanWS)
	for i := 0; i < agents; i++ {
		exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id)
VALUES ($1, $2, $3, 'local', $4)`, wcScanAgent(i), wcScanWS, fmt.Sprintf("wecom scan %d", i), wcScanRuntime)
	}
	return ctx, pool
}

func newDBInstallService(t *testing.T, pool *pgxpool.Pool, provider Provider, binder BotBinder, cfg InstallServiceConfig) *InstallService {
	t.Helper()
	cfg.Provider = provider
	cfg.Box = testBox(t)
	return NewInstallService(db.New(pool), pool, binder, cfg)
}

func scanBegin(agent int, key string) BeginInstallParams {
	return BeginInstallParams{
		WorkspaceID:    mustUUID(wcScanWS),
		AgentID:        mustUUID(wcScanAgent(agent)),
		InitiatorID:    mustUUID(wcScanUser),
		IdempotencyKey: key,
	}
}

func loadSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) db.WecomInstallSession {
	t.Helper()
	row, err := db.New(pool).GetWecomInstallSession(ctx, mustUUID(id))
	if err != nil {
		t.Fatalf("load session %s: %v", id, err)
	}
	return row
}

// makeDue moves a session's next poll to the database's now, so the next claim
// takes it without the test depending on the Go and Postgres clocks agreeing.
func makeDue(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE wecom_install_session SET poll_after = now() WHERE id = $1`, id); err != nil {
		t.Fatalf("make due: %v", err)
	}
}

func claimParams(token string, ttl time.Duration) db.ClaimDueWecomInstallSessionParams {
	return db.ClaimDueWecomInstallSessionParams{
		LeaseToken:     pgtype.Text{String: token, Valid: true},
		LeaseExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(ttl), Valid: true},
	}
}

// The advisory lock is what makes begin's read-then-insert a single decision. A
// begin that arrives while another holds the workspace's lock must wait for it,
// or the two read the same "nothing pending, under the limit" and both insert.
func TestInstallDB_BeginWaitsForTheWorkspaceLock(t *testing.T) {
	ctx, pool := setupScanDB(t, 1)
	svc := newDBInstallService(t, pool, &fakeProvider{}, &fakeBotBinder{}, InstallServiceConfig{})

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder tx: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if err := db.New(pool).WithTx(holder).LockWecomInstallBeginWorkspace(ctx, mustUUID(wcScanWS)); err != nil {
		t.Fatalf("take the workspace lock: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.BeginInstall(ctx, scanBegin(0, "lock-wait"))
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("BeginInstall returned (err=%v) while another transaction held the workspace's begin lock; "+
			"concurrent begins are not serialized, so two of them can both pass the pending and rate checks", err)
	case <-time.After(500 * time.Millisecond):
	}

	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("release the lock: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("BeginInstall after the lock was released: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("BeginInstall still blocked 5s after the lock was released")
	}
}

// Many begins for one agent at once, each with its own key (two tabs, two
// clicks): they must all land on one session. The begins that find a live
// session resume it; a begin that tried to insert a second one instead would hit
// the one-live-session-per-agent index and come back as a 500.
func TestInstallDB_ConcurrentBeginsForOneAgentShareOneSession(t *testing.T) {
	ctx, pool := setupScanDB(t, 1)
	svc := newDBInstallService(t, pool, &fakeProvider{}, &fakeBotBinder{}, InstallServiceConfig{RatePerUser: 100, RatePerWorkspace: 100})

	const n = 8
	var wg sync.WaitGroup
	results := make([]BeginInstallResult, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = svc.BeginInstall(ctx, scanBegin(0, fmt.Sprintf("tab-%d", i)))
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("begin %d: %v", i, err)
		}
		if results[i].SessionID != results[0].SessionID {
			t.Fatalf("begin %d got session %s, begin 0 got %s: one agent has two live sessions", i, results[i].SessionID, results[0].SessionID)
		}
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wecom_install_session WHERE workspace_id = $1`, wcScanWS).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Fatalf("%d session rows for one agent, want 1", rows)
	}
}

// The rate cap is a count read under the same lock. Concurrent begins for
// different agents must not all read "under the limit" and all get in.
func TestInstallDB_ConcurrentBeginsRespectTheRateCap(t *testing.T) {
	const agents, limit = 6, 3
	ctx, pool := setupScanDB(t, agents)
	svc := newDBInstallService(t, pool, &fakeProvider{}, &fakeBotBinder{}, InstallServiceConfig{RatePerUser: limit, RatePerWorkspace: 100})

	var wg sync.WaitGroup
	errs := make([]error, agents)
	start := make(chan struct{})
	for i := 0; i < agents; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = svc.BeginInstall(ctx, scanBegin(i, fmt.Sprintf("agent-%d", i)))
		}(i)
	}
	close(start)
	wg.Wait()

	admitted := 0
	for i, err := range errs {
		switch {
		case err == nil:
			admitted++
		case errors.Is(err, ErrRateLimited):
		default:
			t.Fatalf("begin for agent %d: %v", i, err)
		}
	}
	if admitted != limit {
		t.Fatalf("%d of %d concurrent begins admitted with a per-user cap of %d", admitted, agents, limit)
	}
}

// A claim that is in flight holds its row locked until its statement commits.
// A second worker must step past it to the next row — not wait on it, and not
// take it too.
func TestInstallDB_ClaimSkipsARowAnotherWorkerIsClaiming(t *testing.T) {
	ctx, pool := setupScanDB(t, 2)
	svc := newDBInstallService(t, pool, &fakeProvider{}, &fakeBotBinder{}, InstallServiceConfig{})
	first, err := svc.BeginInstall(ctx, scanBegin(0, "skip-a"))
	if err != nil {
		t.Fatalf("begin a: %v", err)
	}
	second, err := svc.BeginInstall(ctx, scanBegin(1, "skip-b"))
	if err != nil {
		t.Fatalf("begin b: %v", err)
	}

	slow, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin slow tx: %v", err)
	}
	defer func() { _ = slow.Rollback(ctx) }()
	held, err := db.New(pool).WithTx(slow).ClaimDueWecomInstallSession(ctx, claimParams("slow", time.Minute))
	if err != nil {
		t.Fatalf("slow claim: %v", err)
	}

	type claimed struct {
		row db.WecomInstallSession
		err error
	}
	got := make(chan claimed, 1)
	go func() {
		row, err := db.New(pool).ClaimDueWecomInstallSession(ctx, claimParams("fast", time.Minute))
		got <- claimed{row, err}
	}()
	select {
	case c := <-got:
		if c.err != nil {
			t.Fatalf("second claim: %v", c.err)
		}
		if c.row.ID == held.ID {
			t.Fatalf("both workers claimed session %s", util.UUIDToString(held.ID))
		}
		want := map[string]bool{first.SessionID: true, second.SessionID: true}
		if !want[util.UUIDToString(c.row.ID)] {
			t.Fatalf("second claim took %s, which is neither fixture session", util.UUIDToString(c.row.ID))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the second claim blocked on the row the first claim holds instead of skipping it; " +
			"replicas would serialize on one hot session")
	}
}

// A live lease makes a session unclaimable; an expired one hands it to the next
// worker. And from then on the old worker's writes all miss: deferring, failing
// and completing each match on the lease token, so a worker that stalled past
// its lease cannot overwrite what the new owner is doing.
func TestInstallDB_LeaseFencesEveryWrite(t *testing.T) {
	ctx, pool := setupScanDB(t, 1)
	q := db.New(pool)
	svc := newDBInstallService(t, pool, &fakeProvider{}, &fakeBotBinder{}, InstallServiceConfig{})
	begun, err := svc.BeginInstall(ctx, scanBegin(0, "fence"))
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	id := mustUUID(begun.SessionID)

	if _, err := q.ClaimDueWecomInstallSession(ctx, claimParams("old", time.Minute)); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if _, err := q.ClaimDueWecomInstallSession(ctx, claimParams("thief", time.Minute)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("claim while the lease is live: err=%v, want ErrNoRows — two workers would drive one session", err)
	}

	// The old worker stalls past its lease; a second worker takes the session
	// and moves it to pending.
	if _, err := pool.Exec(ctx, `UPDATE wecom_install_session SET lease_expires_at = now() - interval '1 second' WHERE id = $1`, id); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	if _, err := q.ClaimDueWecomInstallSession(ctx, claimParams("new", time.Minute)); err != nil {
		t.Fatalf("claim after expiry: %v", err)
	}
	// The new owner is mid-step: still holding its lease, moving to pending.
	if _, err := pool.Exec(ctx, `UPDATE wecom_install_session SET status = 'pending' WHERE id = $1`, id); err != nil {
		t.Fatalf("advance to pending: %v", err)
	}

	stale := pgtype.Text{String: "old", Valid: true}
	if n, err := q.DeferClaimedWecomInstallSession(ctx, db.DeferClaimedWecomInstallSessionParams{
		ID: id, LeaseToken: stale, Status: InstallStatusCreating,
		PollAfter: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}); err != nil || n != 0 {
		t.Errorf("defer under the expired lease: rows=%d err=%v, want 0 rows", n, err)
	}
	if n, err := q.FailWecomInstallSession(ctx, db.FailWecomInstallSessionParams{
		ID: id, LeaseToken: stale,
		ErrorReason: pgtype.Text{String: InstallErrorInternalError, Valid: true},
	}); err != nil || n != 0 {
		t.Errorf("fail under the expired lease: rows=%d err=%v, want 0 rows", n, err)
	}
	if n, err := q.CompleteWecomInstallSession(ctx, db.CompleteWecomInstallSessionParams{
		ID: id, LeaseToken: stale, InstallationID: mustUUID(wcScanRuntime),
	}); err != nil || n != 0 {
		t.Errorf("complete under the expired lease: rows=%d err=%v, want 0 rows", n, err)
	}

	row := loadSession(t, ctx, pool, begun.SessionID)
	if row.Status != InstallStatusPending || row.LeaseToken.String != "new" || row.ErrorReason.Valid || row.InstallationID.Valid {
		t.Fatalf("the stale worker changed the session: status=%s lease=%q error_reason=%v installation_id=%v",
			row.Status, row.LeaseToken.String, row.ErrorReason, row.InstallationID.Valid)
	}

	// The current owner's writes still land.
	if n, err := q.CompleteWecomInstallSession(ctx, db.CompleteWecomInstallSessionParams{
		ID: id, LeaseToken: pgtype.Text{String: "new", Valid: true}, InstallationID: mustUUID(wcScanRuntime),
	}); err != nil || n != 1 {
		t.Fatalf("complete under the live lease: rows=%d err=%v, want 1", n, err)
	}
}

// Complete is fenced on status as well as lease: a session still creating has
// no scanned bot, so completing it would record a success that never happened.
func TestInstallDB_CompleteRequiresPending(t *testing.T) {
	ctx, pool := setupScanDB(t, 1)
	q := db.New(pool)
	svc := newDBInstallService(t, pool, &fakeProvider{}, &fakeBotBinder{}, InstallServiceConfig{})
	begun, err := svc.BeginInstall(ctx, scanBegin(0, "complete-creating"))
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := q.ClaimDueWecomInstallSession(ctx, claimParams("owner", time.Minute)); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if n, err := q.CompleteWecomInstallSession(ctx, db.CompleteWecomInstallSessionParams{
		ID: mustUUID(begun.SessionID), LeaseToken: pgtype.Text{String: "owner", Valid: true}, InstallationID: mustUUID(wcScanRuntime),
	}); err != nil || n != 0 {
		t.Fatalf("complete on a creating session: rows=%d err=%v, want 0", n, err)
	}
	if row := loadSession(t, ctx, pool, begun.SessionID); row.Status != InstallStatusCreating {
		t.Fatalf("status = %s, want creating", row.Status)
	}
}

// The whole walk on the real tables: begin, the worker fetches and seals the QR,
// the admin scans, the bot is bound through the same InstallationService the
// manual form uses, and the session settles with its secrets wiped. Then a
// settled session is never claimed again.
func TestInstallDB_WorkerWalksASessionToSuccess(t *testing.T) {
	ctx, pool := setupScanDB(t, 1)
	provider := &fakeProvider{gen: GenerateResult{Scode: "scode-walk", AuthURL: "https://example.test/qr?scode=scode-walk"}}
	binder, err := NewInstallationService(db.New(pool), pool, testBox(t), WithCredentialProbe(&fakeProbe{}))
	if err != nil {
		t.Fatalf("NewInstallationService: %v", err)
	}
	svc := newDBInstallService(t, pool, provider, binder, InstallServiceConfig{})
	w := NewInstallWorker(svc, InstallWorkerConfig{})

	begun, err := svc.BeginInstall(ctx, scanBegin(0, "walk"))
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	// creating → pending
	if worked, err := w.processOne(ctx); err != nil || !worked {
		t.Fatalf("generate step: worked=%v err=%v", worked, err)
	}
	row := loadSession(t, ctx, pool, begun.SessionID)
	if row.Status != InstallStatusPending || row.LeaseToken.Valid || !row.ExpiresAt.Valid {
		t.Fatalf("after generate: status=%s lease=%v expires_at=%v, want pending, lease released, expiry set",
			row.Status, row.LeaseToken.Valid, row.ExpiresAt.Valid)
	}
	if row.ScodeEncrypted.String == "" || row.ScodeEncrypted.String == "scode-walk" {
		t.Fatalf("scode stored as %q, want sealed ciphertext", row.ScodeEncrypted.String)
	}
	snap, err := svc.GetSession(ctx, mustUUID(wcScanWS), mustUUID(begun.SessionID), true)
	if err != nil || snap.QRCodeURL != provider.gen.AuthURL {
		t.Fatalf("status read: qr=%q err=%v, want the generated URL back", snap.QRCodeURL, err)
	}

	// Not scanned yet: the poll defers and the session stays pending.
	provider.query = QueryResult{Status: QueryStatusPending}
	makeDue(t, ctx, pool, begun.SessionID)
	if worked, err := w.processOne(ctx); err != nil || !worked {
		t.Fatalf("pending poll: worked=%v err=%v", worked, err)
	}
	if row := loadSession(t, ctx, pool, begun.SessionID); row.Status != InstallStatusPending || row.LeaseToken.Valid {
		t.Fatalf("after an unscanned poll: status=%s lease=%v, want pending with the lease released", row.Status, row.LeaseToken.Valid)
	}

	// Scanned.
	provider.query = QueryResult{Status: QueryStatusSuccess, BotInfo: &BotInfo{BotID: "bot_scan_walk", Secret: "s3cret"}}
	makeDue(t, ctx, pool, begun.SessionID)
	if worked, err := w.processOne(ctx); err != nil || !worked {
		t.Fatalf("success poll: worked=%v err=%v", worked, err)
	}
	row = loadSession(t, ctx, pool, begun.SessionID)
	if row.Status != InstallStatusSuccess || !row.InstallationID.Valid {
		t.Fatalf("after scan: status=%s installation_id=%v, want success with an installation", row.Status, row.InstallationID.Valid)
	}
	if row.ScodeEncrypted.Valid || row.QrCodeUrlEncrypted.Valid || row.LeaseToken.Valid {
		t.Fatalf("a settled session still carries scode=%v qr=%v lease=%v", row.ScodeEncrypted.Valid, row.QrCodeUrlEncrypted.Valid, row.LeaseToken.Valid)
	}
	var app, status string
	if err := pool.QueryRow(ctx, `SELECT config->>'app_id', status FROM channel_installation WHERE id = $1`, row.InstallationID).Scan(&app, &status); err != nil {
		t.Fatalf("read the bound installation: %v", err)
	}
	if app != "bot_scan_walk" || status != "active" {
		t.Fatalf("installation app_id=%q status=%q, want the scanned bot, active", app, status)
	}

	if worked, err := w.processOne(ctx); err != nil || worked {
		t.Fatalf("a settled session was claimed again: worked=%v err=%v", worked, err)
	}
}

// The reason a failed bind ends with comes from what the real InstallationService
// returns, not from a stub: a bot another agent already holds is a conflict,
// and a bot the probe could not reach is one that was created and not
// connected. If Upsert ever stopped returning the sentinels errors.Is can see,
// the first case would silently turn into the second.
func TestInstallDB_BindFailureReasonComesFromTheRealBind(t *testing.T) {
	ctx, pool := setupScanDB(t, 2)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// Agent 1 already holds bot_scan_held.
	exec(`INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES ($1, $2, 'wecom', '{"app_id":"bot_scan_held","bot_id":"bot_scan_held","app_secret_encrypted":""}', $3, 'active')`,
		wcScanWS, wcScanAgent(1), wcScanUser)

	for _, tc := range []struct {
		name  string
		bot   string
		probe error
		want  string
	}{
		{"bot held by another agent", "bot_scan_held", nil, InstallErrorInstallationConflict},
		{"probe cannot reach WeCom", "bot_scan_fresh", context.DeadlineExceeded, InstallErrorBindFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exec(`DELETE FROM wecom_install_session WHERE workspace_id = $1`, wcScanWS)
			provider := &fakeProvider{gen: GenerateResult{Scode: "scode-" + tc.bot, AuthURL: "https://example.test/qr"}}
			binder, err := NewInstallationService(db.New(pool), pool, testBox(t), WithCredentialProbe(&fakeProbe{err: tc.probe}))
			if err != nil {
				t.Fatalf("NewInstallationService: %v", err)
			}
			svc := newDBInstallService(t, pool, provider, binder, InstallServiceConfig{})
			w := NewInstallWorker(svc, InstallWorkerConfig{})

			begun, err := svc.BeginInstall(ctx, scanBegin(0, tc.bot))
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			if worked, err := w.processOne(ctx); err != nil || !worked {
				t.Fatalf("generate step: worked=%v err=%v", worked, err)
			}
			provider.query = QueryResult{Status: QueryStatusSuccess, BotInfo: &BotInfo{BotID: tc.bot, Secret: "s3cret"}}
			makeDue(t, ctx, pool, begun.SessionID)
			if worked, err := w.processOne(ctx); err != nil || !worked {
				t.Fatalf("success poll: worked=%v err=%v", worked, err)
			}
			row := loadSession(t, ctx, pool, begun.SessionID)
			if row.Status != InstallStatusError || row.ErrorReason.String != tc.want {
				t.Fatalf("status=%s reason=%q, want failed with %s", row.Status, row.ErrorReason.String, tc.want)
			}
		})
	}
}

// A QR nobody scanned in time ends as expired, with the ciphertext wiped.
func TestInstallDB_ExpiredSessionEndsAsExpired(t *testing.T) {
	ctx, pool := setupScanDB(t, 1)
	provider := &fakeProvider{gen: GenerateResult{Scode: "scode-exp", AuthURL: "https://example.test/qr?scode=scode-exp"}}
	svc := newDBInstallService(t, pool, provider, &fakeBotBinder{}, InstallServiceConfig{})
	w := NewInstallWorker(svc, InstallWorkerConfig{})
	begun, err := svc.BeginInstall(ctx, scanBegin(0, "expire"))
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := w.processOne(ctx); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE wecom_install_session SET expires_at = now() - interval '1 second', poll_after = now() WHERE id = $1`, mustUUID(begun.SessionID)); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if _, err := w.processOne(ctx); err != nil {
		t.Fatalf("expiry poll: %v", err)
	}
	row := loadSession(t, ctx, pool, begun.SessionID)
	if row.Status != InstallStatusError || row.ErrorReason.String != InstallErrorExpired {
		t.Fatalf("status=%s reason=%q, want error/expired", row.Status, row.ErrorReason.String)
	}
	if row.ScodeEncrypted.Valid || row.QrCodeUrlEncrypted.Valid {
		t.Fatal("an expired session kept its sealed scode or QR URL")
	}
	if provider.queryCall != 0 {
		t.Fatalf("query_result called %d times for an already-expired QR", provider.queryCall)
	}
}

// Purge removes settled sessions past retention and creating rows nobody ever
// claimed, and leaves everything still live or still within retention.
func TestInstallDB_PurgeKeepsLiveAndRecentSessions(t *testing.T) {
	ctx, pool := setupScanDB(t, 0)
	insert := func(status, age string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO wecom_install_session (request_key_hash, workspace_id, agent_id, initiator_user_id, status, created_at, updated_at)
VALUES (gen_random_uuid()::text, $1, gen_random_uuid(), $2, $3, now() - $4::interval, now() - $4::interval)
RETURNING id::text`, wcScanWS, wcScanUser, status, age).Scan(&id); err != nil {
			t.Fatalf("insert %s/%s: %v", status, age, err)
		}
		return id
	}
	oldSuccess := insert("success", "1 hour")
	oldError := insert("error", "1 hour")
	oldCreating := insert("creating", "10 minutes")
	recentSuccess := insert("success", "1 minute")
	recentCreating := insert("creating", "5 seconds")
	oldPending := insert("pending", "1 hour")

	svc := newDBInstallService(t, pool, &fakeProvider{}, &fakeBotBinder{}, InstallServiceConfig{})
	w := NewInstallWorker(svc, InstallWorkerConfig{})
	if err := w.purge(ctx); err != nil {
		t.Fatalf("purge: %v", err)
	}

	exists := func(id string) bool {
		var ok bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM wecom_install_session WHERE id = $1)`, id).Scan(&ok); err != nil {
			t.Fatalf("exists: %v", err)
		}
		return ok
	}
	for name, id := range map[string]string{"old success": oldSuccess, "old error": oldError, "old creating": oldCreating} {
		if exists(id) {
			t.Errorf("%s session survived the purge", name)
		}
	}
	for name, id := range map[string]string{"recent success": recentSuccess, "recent creating": recentCreating, "old pending": oldPending} {
		if !exists(id) {
			t.Errorf("%s session was purged", name)
		}
	}
}

// The status column only takes the four values the code and the frontend
// switch on.
func TestInstallDB_StatusIsConstrained(t *testing.T) {
	ctx, pool := setupScanDB(t, 0)
	_, err := pool.Exec(ctx, `
INSERT INTO wecom_install_session (request_key_hash, workspace_id, agent_id, initiator_user_id, status)
VALUES ('x', $1, gen_random_uuid(), $2, 'scanned')`, wcScanWS, wcScanUser)
	if err == nil {
		t.Fatal("a status outside creating/pending/success/error was accepted")
	}
}
