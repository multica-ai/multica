package lark

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/redis/go-redis/v9"
)

// installSessionStoreRedisDB is a dedicated logical DB so a flush here
// cannot take out another package's fixtures.
const installSessionStoreRedisDB = 12

// newInstallSessionRedis connects to REDIS_TEST_URL and flushes the test
// DB. Skips when the env var is unset or unreachable — same gating as the
// DATABASE_URL suites, so `go test ./...` still works on a stock laptop.
func newInstallSessionRedis(t *testing.T) redis.UniversalClient {
	t.Helper()
	url := os.Getenv("REDIS_TEST_URL")
	if url == "" {
		t.Skip("REDIS_TEST_URL not set")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse REDIS_TEST_URL: %v", err)
	}
	opts.DB = installSessionStoreRedisDB
	rdb := redis.NewClient(opts)
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("REDIS_TEST_URL unreachable: %v", err)
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flushdb: %v", err)
	}
	t.Cleanup(func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	})
	return rdb
}

func installSessionUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

// Both implementations must honour the same contract, so the behavioural
// suite runs against each. The Redis case is the one that matters in
// production; the memory case is what a single-instance dev deploy gets,
// and running them through identical assertions is what keeps the two
// from drifting.
func forEachInstallSessionStore(t *testing.T, fn func(t *testing.T, store InstallSessionStore)) {
	t.Helper()
	t.Run("memory", func(t *testing.T) {
		fn(t, NewMemoryInstallSessionStore())
	})
	t.Run("redis", func(t *testing.T) {
		fn(t, NewRedisInstallSessionStore(newInstallSessionRedis(t)))
	})
}

func TestInstallSessionStoreRoundTrip(t *testing.T) {
	forEachInstallSessionStore(t, func(t *testing.T, store InstallSessionStore) {
		ctx := context.Background()
		ws := installSessionUUID(t, "11111111-1111-1111-1111-111111111111")
		initiator := installSessionUUID(t, "44444444-4444-4444-4444-444444444444")
		expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

		if err := store.Create(ctx, InstallSessionState{
			ID:          "sess-1",
			WorkspaceID: ws,
			InitiatorID: initiator,
			Status:      RegistrationStatusPending,
			ExpiresAt:   expiresAt,
		}, time.Hour); err != nil {
			t.Fatalf("Create: %v", err)
		}

		got, err := store.Get(ctx, ws, "sess-1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status != RegistrationStatusPending {
			t.Errorf("Status: got %q want pending", got.Status)
		}
		// The initiator authorizes the status read, so losing it would
		// silently turn every non-admin's poll into a 404.
		if got.InitiatorID != initiator {
			t.Errorf("InitiatorID: got %v want %v", got.InitiatorID, initiator)
		}
		if !got.ExpiresAt.Equal(expiresAt) {
			t.Errorf("ExpiresAt: got %s want %s", got.ExpiresAt, expiresAt)
		}
	})
}

func TestInstallSessionStoreUnknownAndCrossWorkspaceAreIndistinguishable(t *testing.T) {
	forEachInstallSessionStore(t, func(t *testing.T, store InstallSessionStore) {
		ctx := context.Background()
		ws := installSessionUUID(t, "11111111-1111-1111-1111-111111111111")
		otherWs := installSessionUUID(t, "22222222-2222-2222-2222-222222222222")

		if err := store.Create(ctx, InstallSessionState{
			ID: "sess-2", WorkspaceID: ws, InitiatorID: ws,
			Status: RegistrationStatusPending, ExpiresAt: time.Now().Add(time.Hour),
		}, time.Hour); err != nil {
			t.Fatalf("Create: %v", err)
		}

		if _, err := store.Get(ctx, ws, "no-such-session"); !errors.Is(err, ErrInstallSessionNotFound) {
			t.Errorf("unknown id: want ErrInstallSessionNotFound, got %v", err)
		}
		if _, err := store.Get(ctx, otherWs, "sess-2"); !errors.Is(err, ErrInstallSessionNotFound) {
			t.Errorf("cross-workspace: want ErrInstallSessionNotFound, got %v", err)
		}
	})
}

// The expiry deadline and a poll result can terminate the same session
// concurrently. Whichever lands first is what the user was already shown,
// so a later write must be a silent no-op — not an error, and not an
// overwrite.
func TestInstallSessionStoreTerminalIsFirstWriterWins(t *testing.T) {
	forEachInstallSessionStore(t, func(t *testing.T, store InstallSessionStore) {
		ctx := context.Background()
		ws := installSessionUUID(t, "11111111-1111-1111-1111-111111111111")
		installationID := installSessionUUID(t, "33333333-3333-3333-3333-333333333333")

		if err := store.Create(ctx, InstallSessionState{
			ID: "sess-3", WorkspaceID: ws, InitiatorID: ws,
			Status: RegistrationStatusPending, ExpiresAt: time.Now().Add(time.Hour),
		}, time.Hour); err != nil {
			t.Fatalf("Create: %v", err)
		}

		if err := store.MarkTerminal(ctx, "sess-3", InstallSessionOutcome{
			Status:       RegistrationStatusError,
			ErrorReason:  RegistrationReasonAccessDenied,
			ErrorMessage: "user denied",
		}, 30*time.Minute); err != nil {
			t.Fatalf("first MarkTerminal: %v", err)
		}
		if err := store.MarkTerminal(ctx, "sess-3", InstallSessionOutcome{
			Status: RegistrationStatusSuccess, InstallationID: installationID,
		}, 30*time.Minute); err != nil {
			t.Fatalf("second MarkTerminal must be a no-op, not an error: %v", err)
		}

		got, err := store.Get(ctx, ws, "sess-3")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status != RegistrationStatusError {
			t.Fatalf("Status: got %q want error — a late success must not overwrite a recorded failure", got.Status)
		}
		if got.ErrorReason != RegistrationReasonAccessDenied {
			t.Errorf("ErrorReason: got %q want %q", got.ErrorReason, RegistrationReasonAccessDenied)
		}
	})
}

func TestInstallSessionStoreSuccessCarriesInstallationID(t *testing.T) {
	forEachInstallSessionStore(t, func(t *testing.T, store InstallSessionStore) {
		ctx := context.Background()
		ws := installSessionUUID(t, "11111111-1111-1111-1111-111111111111")
		installationID := installSessionUUID(t, "33333333-3333-3333-3333-333333333333")

		if err := store.Create(ctx, InstallSessionState{
			ID: "sess-4", WorkspaceID: ws, InitiatorID: ws,
			Status: RegistrationStatusPending, ExpiresAt: time.Now().Add(time.Hour),
		}, time.Hour); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := store.MarkTerminal(ctx, "sess-4", InstallSessionOutcome{
			Status: RegistrationStatusSuccess, InstallationID: installationID,
		}, 30*time.Minute); err != nil {
			t.Fatalf("MarkTerminal: %v", err)
		}

		got, err := store.Get(ctx, ws, "sess-4")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status != RegistrationStatusSuccess {
			t.Fatalf("Status: got %q want success", got.Status)
		}
		// The dialog closes on this id and invalidates the installations
		// cache, so dropping it would strand a successful bind.
		if got.InstallationID != installationID {
			t.Errorf("InstallationID: got %v want %v", got.InstallationID, installationID)
		}
	})
}

// Retention is the store's job now — there is no sweep to run. A record
// past its TTL must read as gone, not as stale-but-present.
func TestInstallSessionStoreRetentionExpires(t *testing.T) {
	forEachInstallSessionStore(t, func(t *testing.T, store InstallSessionStore) {
		ctx := context.Background()
		ws := installSessionUUID(t, "11111111-1111-1111-1111-111111111111")

		if err := store.Create(ctx, InstallSessionState{
			ID: "sess-5", WorkspaceID: ws, InitiatorID: ws,
			Status: RegistrationStatusPending, ExpiresAt: time.Now().Add(time.Hour),
			// 1s is the shortest retention both implementations can
			// express: Redis EXPIRE is second-granular, and ttlSeconds
			// floors at 1 so a sub-second TTL cannot round down to 0,
			// which Redis reads as "no expiry".
		}, time.Second); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := store.Get(ctx, ws, "sess-5"); err != nil {
			t.Fatalf("within retention: %v", err)
		}

		time.Sleep(1300 * time.Millisecond)

		if _, err := store.Get(ctx, ws, "sess-5"); !errors.Is(err, ErrInstallSessionNotFound) {
			t.Errorf("past retention: want ErrInstallSessionNotFound, got %v", err)
		}
	})
}

// TestRedisInstallSessionStoreIsSharedAcrossClients is the MUL-7340
// regression at the storage layer: two store instances with SEPARATE
// Redis clients — standing in for two API replicas — must see one
// session. This is the property the old in-process map did not have, and
// the memory store still does not have; it is why the router must pick
// the Redis one whenever Redis exists.
func TestRedisInstallSessionStoreIsSharedAcrossClients(t *testing.T) {
	replicaA := NewRedisInstallSessionStore(newInstallSessionRedis(t))
	replicaB := NewRedisInstallSessionStore(newInstallSessionRedis(t))
	ctx := context.Background()
	ws := installSessionUUID(t, "11111111-1111-1111-1111-111111111111")

	if err := replicaA.Create(ctx, InstallSessionState{
		ID: "shared-1", WorkspaceID: ws, InitiatorID: ws,
		Status: RegistrationStatusPending, ExpiresAt: time.Now().Add(time.Hour),
	}, time.Hour); err != nil {
		t.Fatalf("replica A Create: %v", err)
	}

	got, err := replicaB.Get(ctx, ws, "shared-1")
	if err != nil {
		t.Fatalf("replica B must serve a session replica A created: %v", err)
	}
	if got.Status != RegistrationStatusPending {
		t.Errorf("Status: got %q want pending", got.Status)
	}

	// And a terminal outcome recorded on A is visible on B.
	if err := replicaA.MarkTerminal(ctx, "shared-1", InstallSessionOutcome{
		Status: RegistrationStatusError, ErrorReason: RegistrationReasonExpired,
	}, 30*time.Minute); err != nil {
		t.Fatalf("replica A MarkTerminal: %v", err)
	}
	got, err = replicaB.Get(ctx, ws, "shared-1")
	if err != nil {
		t.Fatalf("replica B Get after terminal: %v", err)
	}
	if got.Status != RegistrationStatusError || got.ErrorReason != RegistrationReasonExpired {
		t.Errorf("terminal outcome did not cross clients: %+v", got)
	}
}

// A Redis failure must NOT read as "session not found": that is a
// terminal state the dialog cannot recover from, so a transient blip
// would permanently kill a live scan. It has to surface as an error the
// caller can retry.
func TestRedisInstallSessionStoreFailureIsNotNotFound(t *testing.T) {
	rdb := newInstallSessionRedis(t)
	store := NewRedisInstallSessionStore(rdb)
	ctx := context.Background()
	ws := installSessionUUID(t, "11111111-1111-1111-1111-111111111111")

	if err := store.Create(ctx, InstallSessionState{
		ID: "sess-6", WorkspaceID: ws, InitiatorID: ws,
		Status: RegistrationStatusPending, ExpiresAt: time.Now().Add(time.Hour),
	}, time.Hour); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := rdb.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}

	_, err := store.Get(ctx, ws, "sess-6")
	if err == nil {
		t.Fatal("Get against a closed client must fail")
	}
	if errors.Is(err, ErrInstallSessionNotFound) {
		t.Fatal("a Redis failure must not be reported as ErrInstallSessionNotFound")
	}
}

// failScriptOnce fails the first script evaluation, then passes through.
// Script.Run tries EVALSHA and falls back to EVAL on NOSCRIPT, so both
// have to be wrapped to intercept the first attempt reliably.
type failScriptOnce struct {
	redis.UniversalClient
	// armed gates the injection so setup calls — Create also runs a
	// script — do not consume it.
	armed  bool
	failed bool
}

func (c *failScriptOnce) fail(ctx context.Context) *redis.Cmd {
	cmd := redis.NewCmd(ctx)
	cmd.SetErr(errors.New("injected Redis outage mid-script"))
	return cmd
}

func (c *failScriptOnce) EvalSha(ctx context.Context, sha string, keys []string, args ...any) *redis.Cmd {
	if c.armed && !c.failed {
		c.failed = true
		return c.fail(ctx)
	}
	return c.UniversalClient.EvalSha(ctx, sha, keys, args...)
}

func (c *failScriptOnce) Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
	if c.armed && !c.failed {
		c.failed = true
		return c.fail(ctx)
	}
	return c.UniversalClient.Eval(ctx, script, keys, args...)
}

// TestRedisInstallSessionStoreFailedRequestWritesNothing covers the case
// where the write never reaches Redis at all — a dropped connection
// before the script runs. Nothing is applied, and the retry moves status
// and retention together.
//
// Note what this does NOT claim: the injector fails BEFORE executing, so
// this says nothing about a script that dies partway through. Redis has
// no rollback, so Lua atomicity would not save us there either. The
// protection against the original two-key bug is structural — one key
// with one expiry — plus the replay behaviour covered by
// TestRedisInstallSessionStoreReplayAfterLostResponse.
func TestRedisInstallSessionStoreFailedRequestWritesNothing(t *testing.T) {
	rdb := newInstallSessionRedis(t)
	injector := &failScriptOnce{UniversalClient: rdb}
	store := NewRedisInstallSessionStore(injector)
	ctx := context.Background()
	ws := installSessionUUID(t, "11111111-1111-1111-1111-111111111111")
	id := "partial-write"

	if err := store.Create(ctx, InstallSessionState{
		ID: id, WorkspaceID: ws, InitiatorID: ws,
		Status: RegistrationStatusPending, ExpiresAt: time.Now().Add(time.Hour),
	}, 90*time.Minute); err != nil {
		t.Fatalf("Create: %v", err)
	}

	injector.armed = true
	outcome := InstallSessionOutcome{Status: RegistrationStatusSuccess}
	if err := store.MarkTerminal(ctx, id, outcome, time.Second); err == nil {
		t.Fatal("expected the injected failure to surface")
	}

	// Nothing applied: a request that never reached Redis must not have
	// left the status written under the original retention.
	state, err := store.Get(ctx, ws, id)
	if err != nil {
		t.Fatalf("Get after failed write: %v", err)
	}
	if state.Status != RegistrationStatusPending {
		t.Errorf("Status after a failed terminal write: got %q want pending", state.Status)
	}
	if ttl := rdb.TTL(ctx, installSessionKey(id)).Val(); ttl <= time.Hour {
		t.Errorf("failed write moved retention anyway: TTL=%s", ttl)
	}

	// Retry: status and retention move together.
	if err := store.MarkTerminal(ctx, id, outcome, time.Second); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if ttl := rdb.TTL(ctx, installSessionKey(id)).Val(); ttl > 2*time.Second {
		t.Errorf("retry reported success without moving retention: TTL=%s", ttl)
	}
	state, err = store.Get(ctx, ws, id)
	if err != nil {
		t.Fatalf("Get after retry: %v", err)
	}
	if state.Status != RegistrationStatusSuccess {
		t.Errorf("Status after retry: got %q want success", state.Status)
	}

	// And it never regresses to pending: the whole record expires at once.
	time.Sleep(1100 * time.Millisecond)
	if _, err := store.Get(ctx, ws, id); !errors.Is(err, ErrInstallSessionNotFound) {
		t.Errorf("past retention: want ErrInstallSessionNotFound, got %v", err)
	}
}

// A MarkTerminal for a session that is already gone reports
// ErrInstallSessionNotFound so the service's retry loop stops instead of
// burning its whole budget on something no retry can fix.
func TestInstallSessionStoreMarkTerminalOnMissingSessionIsNotFound(t *testing.T) {
	forEachInstallSessionStore(t, func(t *testing.T, store InstallSessionStore) {
		err := store.MarkTerminal(context.Background(), "never-existed",
			InstallSessionOutcome{Status: RegistrationStatusSuccess}, time.Minute)
		if !errors.Is(err, ErrInstallSessionNotFound) {
			t.Errorf("want ErrInstallSessionNotFound, got %v", err)
		}
	})
}

// loseScriptReply lets the script run on the server, then hands the
// caller an error — the realistic failure the two-key version could not
// survive: the write landed, but the client does not know it.
type loseScriptReply struct {
	redis.UniversalClient
	armed  bool
	failed bool
}

func (c *loseScriptReply) intercept(ctx context.Context, cmd *redis.Cmd) *redis.Cmd {
	if c.armed && !c.failed && cmd.Err() == nil {
		c.failed = true
		lost := redis.NewCmd(ctx)
		lost.SetErr(errors.New("injected lost response after successful server execution"))
		return lost
	}
	return cmd
}

func (c *loseScriptReply) EvalSha(ctx context.Context, sha string, keys []string, args ...any) *redis.Cmd {
	return c.intercept(ctx, c.UniversalClient.EvalSha(ctx, sha, keys, args...))
}

func (c *loseScriptReply) Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
	return c.intercept(ctx, c.UniversalClient.Eval(ctx, script, keys, args...))
}

// TestRedisInstallSessionStoreReplayAfterLostResponse is the case the
// earlier two-key store actually lost (raised in re-review of #8376): the
// server ran the write, the client never saw the reply, and the retry
// therefore replays a write that already landed.
//
// Under SETNX-plus-EXPIRE the replay reported "someone else won" and
// skipped the EXPIRE, stranding the terminal status under the base key's
// original retention. Here the replay must keep the first outcome AND
// re-assert retention, so the record expires as one piece and never reads
// `pending` again.
func TestRedisInstallSessionStoreReplayAfterLostResponse(t *testing.T) {
	rdb := newInstallSessionRedis(t)
	client := &loseScriptReply{UniversalClient: rdb}
	store := NewRedisInstallSessionStore(client)
	ctx := context.Background()
	ws := installSessionUUID(t, "11111111-1111-1111-1111-111111111111")
	id := "lost-response"

	if err := store.Create(ctx, InstallSessionState{
		ID: id, WorkspaceID: ws, InitiatorID: ws,
		Status: RegistrationStatusPending, ExpiresAt: time.Now().Add(time.Hour),
	}, 90*time.Minute); err != nil {
		t.Fatalf("Create: %v", err)
	}

	client.armed = true
	if err := store.MarkTerminal(ctx, id, InstallSessionOutcome{
		Status: RegistrationStatusSuccess,
	}, 30*time.Minute); err == nil {
		t.Fatal("expected the lost reply to surface as an error")
	}

	// The server did run it, so the outcome is already recorded.
	state, err := store.Get(ctx, ws, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if state.Status != RegistrationStatusSuccess {
		t.Fatalf("server did not retain the first write: got %q", state.Status)
	}

	// The caller retries, not knowing that. A different outcome must not
	// replace the recorded one, and retention must still move.
	if err := store.MarkTerminal(ctx, id, InstallSessionOutcome{
		Status: RegistrationStatusError, ErrorReason: RegistrationReasonExpired,
	}, time.Second); err != nil {
		t.Fatalf("replay: %v", err)
	}
	state, err = store.Get(ctx, ws, id)
	if err != nil {
		t.Fatalf("Get after replay: %v", err)
	}
	if state.Status != RegistrationStatusSuccess {
		t.Fatalf("replay replaced the first terminal outcome: got %q", state.Status)
	}
	if ttl := rdb.TTL(ctx, installSessionKey(id)).Val(); ttl > 2*time.Second || ttl < 0 {
		t.Fatalf("replay did not re-assert retention: TTL=%s", ttl)
	}

	time.Sleep(1100 * time.Millisecond)
	if _, err := store.Get(ctx, ws, id); !errors.Is(err, ErrInstallSessionNotFound) {
		t.Errorf("whole record must expire at once, never back to pending: %v", err)
	}
}
