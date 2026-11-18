package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func fenceTestPool(t *testing.T, trace pgx.QueryTracer) *pgxpool.Pool {
	t.Helper()
	cfg := testPool.Config()
	cfg.ConnConfig.Tracer = trace
	pool, err := NewTeardownFencePool(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestTeardownFenceSessionOwnership(t *testing.T) {
	h := *testHandler
	h.TeardownFencePool = fenceTestPool(t, nil)
	ws := uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fence, err := h.acquireTeardownFence(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	defer fence.Close()
	if _, err = h.acquireTeardownFence(ctx, ws); !errors.Is(err, errTeardownFenceBusy) {
		t.Fatalf("session contention: %v", err)
	}
	other, err := h.acquireTeardownFence(ctx, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
	// Failed try-locks must have returned their connections without a lock.
	if got := h.TeardownFencePool.Stat().AcquiredConns(); got != 1 {
		t.Fatalf("held %d connections", got)
	}
	// The Qoder configuration key must remain independently acquirable.
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var locked bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,19))`, ws).Scan(&locked); err != nil || !locked {
		t.Fatalf("Qoder key collision: %v %v", locked, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// Cancellation can leave an aborted transaction. Close must rollback using
	// its own context, unlock, and be safe to call more than once.
	cancelled, cancelRequest := context.WithCancel(ctx)
	tx, err = fence.conn.Begin(cancelled)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(cancelled, `SELECT 1/0`); err == nil {
		t.Fatal("expected aborted transaction")
	}
	cancelRequest()
	fence.Close()
	fence.Close()
	next, err := h.acquireTeardownFence(ctx, ws)
	if err != nil {
		t.Fatalf("cancelled owner retained lock: %v", err)
	}
	next.Close()
	if got := h.TeardownFencePool.Stat().AcquiredConns(); got != 0 {
		t.Fatalf("close leaked %d connections", got)
	}
}

// If unlock cannot prove release, the underlying session must be discarded.
func TestTeardownFenceDiscardOnUncertainUnlock(t *testing.T) {
	for _, failure := range []string{"unlock-false", "connection-lost"} {
		t.Run(failure, func(t *testing.T) {
			h := *testHandler
			h.TeardownFencePool = fenceTestPool(t, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ws := uuid.NewString()
			fence, err := h.acquireTeardownFence(ctx, ws)
			if err != nil {
				t.Fatal(err)
			}
			defer fence.Close()
			pid := fence.conn.Conn().PgConn().PID()
			if failure == "unlock-false" {
				if _, err = fence.conn.Exec(ctx, `SELECT pg_advisory_unlock_all()`); err != nil {
					t.Fatal(err)
				}
			} else {
				var terminated bool
				if err = testPool.QueryRow(ctx, `SELECT pg_terminate_backend($1)`, pid).Scan(&terminated); err != nil || !terminated {
					t.Fatalf("terminate: %v %v", terminated, err)
				}
			}
			fence.Close()
			next, err := h.acquireTeardownFence(ctx, ws)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			if next.conn.Conn().PgConn().PID() == pid {
				t.Fatal("uncertain session returned to pool")
			}
		})
	}
}

func TestTeardownFenceUnavailableFailsClosed(t *testing.T) {
	_, ws, rt := teardownGuardFixture(t, "claude")
	h := *testHandler
	h.TeardownFencePool = nil
	testutil.Call(t, h.DeleteWorkspace, teardownGuardRequest("workspace", ws, rt, nil)).Want(http.StatusServiceUnavailable)
	if err := h.cleanupQoderJournal(context.Background(), ws); err == nil {
		t.Fatal("background cleanup silently fell back to application pool")
	}
}

// Two owners can occupy the entire fence pool without using either of the
// application's scarce connections. TaskService must still be able to commit.
func TestTeardownFenceSeparateCapacity(t *testing.T) {
	withVCSBox(t)
	f, ws, rt := teardownGuardFixture(t, "claude")
	a := f.Agent(t, "Task carrier", rt)
	task := f.Task(t, a, testutil.Cols{"runtime_id": rt, "status": "running"})
	installFenceQoder(t, f, ws, "https://example.invalid", `{"runs":[{"task_id":"`+task+`"}]}`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg := testPool.Config()
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.MinIdleConns = 0
	app, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	h := *testHandler
	h.TeardownFencePool = fenceTestPool(t, nil)
	h.TaskService = service.NewTaskService(db.New(app), app, h.Hub, h.Bus, h.DaemonHub)
	first, err := h.acquireTeardownFence(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := h.acquireTeardownFence(ctx, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if app.Stat().AcquiredConns() != 0 {
		t.Fatal("fence used application capacity")
	}
	if err = h.cleanupQoderJournalFenced(ctx, ws, first); err != nil {
		t.Fatalf("task service starved by fence owners: %v", err)
	}
	var failed bool
	f.QueryRow(t, `SELECT status='failed' FROM agent_task_queue WHERE id=$1`, task).Scan(&failed)
	if !failed {
		t.Fatal("cleanup did not transition task")
	}
	saturated, cancelAcquire := context.WithCancel(ctx)
	cancelAcquire()
	if _, err = h.acquireTeardownFence(saturated, uuid.NewString()); err == nil {
		t.Fatal("cancelled acquisition succeeded")
	}
	if h.TeardownFencePool.Stat().AcquiredConns() != 2 {
		t.Fatal("cancelled acquisition changed ownership")
	}
}

type fencePIDTrace struct{ pid chan uint32 }

func (tr *fencePIDTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, fenceTraceQueryKey{}, data.SQL)
}

type fenceTraceQueryKey struct{}

func (tr *fencePIDTrace) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, _ pgx.TraceQueryEndData) {
	sql, _ := ctx.Value(fenceTraceQueryKey{}).(string)
	if strings.Contains(sql, "pg_try_advisory_lock(") {
		select {
		case tr.pid <- conn.PgConn().PID():
		default:
		}
	}
}

// After session loss the already-issued remote request can still finish. Its
// old owner must be unable to commit journal cleanup or final deletion, even
// when a new creator obtains the released PostgreSQL lock.
func TestTeardownFenceConnectionLossCannotCommitCleanup(t *testing.T) {
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	withVCSBox(t)
	for _, scope := range []string{"workspace", "disconnect", "background"} {
		t.Run(scope, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once, releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Drain the body so the server notices mid-request client
				// aborts (background read starts only at body EOF).
				_, _ = io.Copy(io.Discard, r.Body)
				if r.Method == "POST" {
					once.Do(func() { close(entered) })
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"terminated"}`))
			}))
			defer remote.Close()
			defer unblock()
			f, ws, rt := teardownGuardFixture(t, "kagent")
			installFenceQoder(t, f, ws, remote.URL, `{"cleanup":["session-fixture"]}`)
			dbfx.Cleanup(t, `DELETE FROM agent WHERE workspace_id=$1`, ws)
			dbfx.Cleanup(t, `DELETE FROM kagent_enrollment WHERE workspace_id=$1`, ws)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			h := *testHandler
			trace := &fencePIDTrace{pid: make(chan uint32, 1)}
			h.TeardownFencePool = fenceTestPool(t, trace)
			done := make(chan struct{})
			response := httptest.NewRecorder()
			var cleanupErr error
			go func() {
				defer close(done)
				if scope == "background" {
					cleanupErr = h.cleanupQoderJournal(ctx, ws)
					return
				}
				_, req, _ := fenceCleanupRequest(scope, ws, rt)
				if scope == "workspace" {
					h.DeleteWorkspace(response, withRequestContext(req, ctx))
				} else {
					h.DisconnectQoderConnection(response, withRequestContext(req, ctx))
				}
			}()
			defer func() { unblock(); cancel(); <-done }()
			select {
			case <-entered:
			case <-done:
				t.Fatalf("cleanup missed gap: %d %s %v", response.Code, response.Body, cleanupErr)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var pid uint32
			select {
			case pid = <-trace.pid:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var terminated bool
			if err := testPool.QueryRow(ctx, `SELECT pg_terminate_backend($1)`, pid).Scan(&terminated); err != nil || !terminated {
				t.Fatalf("terminate: %v %v", terminated, err)
			}
			testutil.Call(t, testHandler.CreateAgent, withRequestContext(teardownGuardCreateRequest(ws, rt), ctx)).Want(http.StatusCreated)
			unblock()
			teardownGuardAwait(t, done)
			if scope == "background" {
				if cleanupErr == nil {
					t.Fatal("lost session committed cleanup")
				}
			} else if response.Code < 400 {
				t.Fatalf("lost session returned success: %d %s", response.Code, response.Body)
			}
			var retained bool
			f.QueryRow(t, `SELECT EXISTS(SELECT 1 FROM workspace WHERE id=$1) AND EXISTS(SELECT 1 FROM cloud_agent_connection WHERE workspace_id=$1 AND journal_encrypted<>'') AND EXISTS(SELECT 1 FROM kagent_enrollment WHERE workspace_id=$1)`, ws).Scan(&retained)
			if !retained {
				t.Fatal("old owner committed after losing the session fence")
			}
		})
	}
}

func TestTeardownFenceBackgroundRetainsUnknownTombstone(t *testing.T) {
	withVCSBox(t)
	f, ws, _ := teardownGuardFixture(t, "claude")
	installFenceQoder(t, f, ws, "https://example.invalid", `{"cleanup":["never-contact"]}`)
	teardownGuardEnrollment(t, f, ws, uuid.NewString(), uuid.NewString(), "absent", "deleted")
	if err := testHandler.cleanupQoderJournal(context.Background(), ws); !errors.Is(err, service.ErrRuntimeKagentEnrollment) {
		t.Fatalf("unknown tombstone ignored: %v", err)
	}
	var retained bool
	f.QueryRow(t, `SELECT enabled AND journal_encrypted<>'' FROM cloud_agent_connection WHERE workspace_id=$1`, ws).Scan(&retained)
	if !retained {
		t.Fatal("tombstone refusal mutated connection")
	}
}

func TestTeardownFenceMembershipRevocationFirst(t *testing.T) {
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	f, ws, rt := teardownGuardFixture(t, "kagent")
	// Revocation must not remove this runtime: its owner is another member.
	other := dbfx.User(t, "Fence other", "fence-other-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, ws, other, "owner")
	f.Exec(t, `UPDATE agent_runtime SET owner_id=$2 WHERE id=$1`, rt, other)
	var memberID string
	f.QueryRow(t, `SELECT id FROM member WHERE workspace_id=$1 AND user_id=$2`, ws, testUserID).Scan(&memberID)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	h := *testHandler
	entered, release := make(chan struct{}), make(chan struct{})
	h.TxStarter = teardownGuardCommitBarrier{txStarter: testPool, entered: entered, release: release}
	revokeDone := make(chan struct{})
	var revokeErr error
	go func() {
		defer close(revokeDone)
		_, revokeErr = h.revokeAndRemoveMember(ctx, parseUUID(ws), parseUUID(testUserID), parseUUID(memberID), parseUUID(testUserID))
	}()
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); cancel(); <-revokeDone }()
	select {
	case <-entered:
	case <-revokeDone:
		t.Fatalf("revocation missed barrier: %v", revokeErr)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	conn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	creator := *testHandler
	creator.TxStarter = conn
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		creator.CreateAgent(response, withRequestContext(teardownGuardCreateRequest(ws, rt), ctx))
	}()
	defer func() { unblock(); cancel(); <-done }()
	waitForBlockedWriter(t, ctx, int32(conn.Conn().PgConn().PID()))
	unblock()
	teardownGuardAwait(t, revokeDone)
	teardownGuardAwait(t, done)
	if revokeErr != nil {
		t.Fatal(revokeErr)
	}
	if response.Code != http.StatusForbidden {
		t.Fatalf("stale membership accepted: %d %s", response.Code, response.Body)
	}
	var empty bool
	f.QueryRow(t, `SELECT NOT EXISTS(SELECT 1 FROM agent WHERE workspace_id=$1) AND NOT EXISTS(SELECT 1 FROM kagent_enrollment WHERE workspace_id=$1)`, ws).Scan(&empty)
	if !empty {
		t.Fatal("revoked creator wrote agent or enrollment")
	}
}

// Trace the real PostgreSQL COMMIT completion, rather than replacing the
// transaction starter: teardown must always begin on its pinned session.
type fenceCommitGapTrace struct {
	commits int
	entered chan struct{}
	release chan struct{}
}

func (tr *fenceCommitGapTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, fenceTraceQueryKey{}, data.SQL)
}
func (tr *fenceCommitGapTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	sql, _ := ctx.Value(fenceTraceQueryKey{}).(string)
	if strings.EqualFold(sql, "commit") && data.Err == nil {
		tr.commits++
		if tr.commits == 2 {
			close(tr.entered)
			select {
			case <-tr.release:
			case <-ctx.Done():
			}
		}
	}
}

func TestTeardownFenceHoldsAfterJournalCommit(t *testing.T) {
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	withVCSBox(t)
	for _, scope := range []string{"workspace", "disconnect"} {
		for _, loseSession := range []bool{false, true} {
			name := scope + "/owned"
			if loseSession {
				name = scope + "/session-lost"
			}
			t.Run(name, func(t *testing.T) {
				remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"status":"terminated"}`))
				}))
				defer remote.Close()
				f, ws, rt := teardownGuardFixture(t, "kagent")
				installFenceQoder(t, f, ws, remote.URL, `{"cleanup":["session-fixture"]}`)
				dbfx.Cleanup(t, `DELETE FROM agent WHERE workspace_id=$1`, ws)
				dbfx.Cleanup(t, `DELETE FROM kagent_enrollment WHERE workspace_id=$1`, ws)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				trace := &fenceCommitGapTrace{entered: make(chan struct{}), release: make(chan struct{})}
				h := *testHandler
				h.TeardownFencePool = fenceTestPool(t, trace)
				response := httptest.NewRecorder()
				done := make(chan struct{})
				_, req, want := fenceCleanupRequest(scope, ws, rt)
				go func() {
					defer close(done)
					if scope == "workspace" {
						h.DeleteWorkspace(response, withRequestContext(req, ctx))
					} else {
						h.DisconnectQoderConnection(response, withRequestContext(req, ctx))
					}
				}()
				var once sync.Once
				unblock := func() { once.Do(func() { close(trace.release) }) }
				defer func() { unblock(); cancel(); <-done }()
				select {
				case <-trace.entered:
				case <-done:
					t.Fatalf("missed journal commit: %d %s", response.Code, response.Body)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				var clean bool
				f.QueryRow(t, `SELECT journal_encrypted='' FROM cloud_agent_connection WHERE workspace_id=$1`, ws).Scan(&clean)
				if !clean {
					t.Fatal("barrier preceded journal commit")
				}
				testutil.Call(t, testHandler.CreateAgent, withRequestContext(teardownGuardCreateRequest(ws, rt), ctx)).Want(http.StatusConflict)
				if loseSession {
					// Locate only the holder of this fixture's precise advisory lock.
					var pid uint32
					err := testPool.QueryRow(ctx, `WITH k AS (SELECT `+teardownFenceKey+` AS id) SELECT l.pid FROM pg_locks l,k WHERE l.locktype='advisory' AND l.granted AND l.classid=((k.id>>32)&4294967295)::oid AND l.objid=(k.id&4294967295)::oid AND l.objsubid=1`, ws).Scan(&pid)
					if err != nil {
						t.Fatal(err)
					}
					var terminated bool
					if err = testPool.QueryRow(ctx, `SELECT pg_terminate_backend($1)`, pid).Scan(&terminated); err != nil || !terminated {
						t.Fatalf("terminate: %v %v", terminated, err)
					}
					testutil.Call(t, testHandler.CreateAgent, withRequestContext(teardownGuardCreateRequest(ws, rt), ctx)).Want(http.StatusCreated)
				}
				unblock()
				teardownGuardAwait(t, done)
				if loseSession {
					if response.Code < 400 {
						t.Fatalf("lost session deleted local resources: %d %s", response.Code, response.Body)
					}
					var retained bool
					f.QueryRow(t, `SELECT EXISTS(SELECT 1 FROM workspace WHERE id=$1) AND EXISTS(SELECT 1 FROM cloud_agent_connection WHERE workspace_id=$1) AND EXISTS(SELECT 1 FROM kagent_enrollment WHERE workspace_id=$1)`, ws).Scan(&retained)
					if !retained {
						t.Fatal("final deletion committed using a replacement connection")
					}
				} else if response.Code != want {
					t.Fatalf("cleanup did not finish: %d %s", response.Code, response.Body)
				}
			})
		}
	}
}

func TestTeardownFenceCleanupFailureAndCancellationReleaseOwnership(t *testing.T) {
	withVCSBox(t)
	for _, scope := range []string{"workspace", "disconnect", "background"} {
		for _, failure := range []string{"remote-error", "cancelled"} {
			t.Run(scope+"/"+failure, func(t *testing.T) {
				entered := make(chan struct{})
				var once sync.Once
				remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Drain the body so the server starts its background
					// read; otherwise it never notices a mid-request client
					// abort and this handler leaks past remote.Close().
					_, _ = io.Copy(io.Discard, r.Body)
					once.Do(func() { close(entered) })
					if failure == "cancelled" {
						<-r.Context().Done()
						return
					}
					w.WriteHeader(http.StatusServiceUnavailable)
				}))
				defer remote.Close()
				f, ws, rt := teardownGuardFixture(t, "claude")
				installFenceQoder(t, f, ws, remote.URL, `{"cleanup":["session-fixture"]}`)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				response := httptest.NewRecorder()
				done := make(chan struct{})
				var cleanupErr error
				go func() {
					defer close(done)
					if scope == "background" {
						cleanupErr = testHandler.cleanupQoderJournal(ctx, ws)
						return
					}
					handle, req, _ := fenceCleanupRequest(scope, ws, rt)
					handle(response, withRequestContext(req, ctx))
				}()
				defer func() { cancel(); <-done }()
				select {
				case <-entered:
				case <-done:
					t.Fatalf("cleanup missed remote call: %d %s %v", response.Code, response.Body, cleanupErr)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if failure == "cancelled" {
					cancel()
				}
				teardownGuardAwait(t, done)
				if scope == "background" {
					if cleanupErr == nil {
						t.Fatal("failed cleanup returned success")
					}
				} else if response.Code < 400 {
					t.Fatalf("cleanup reported success: %d %s", response.Code, response.Body)
				}
				checkCtx, checkCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer checkCancel()
				next, err := testHandler.acquireTeardownFence(checkCtx, ws)
				if err != nil {
					t.Fatalf("failed owner retained lock: %v", err)
				}
				next.Close()
				var retained bool
				f.QueryRow(t, `SELECT EXISTS(SELECT 1 FROM workspace WHERE id=$1) AND EXISTS(SELECT 1 FROM cloud_agent_connection WHERE workspace_id=$1 AND journal_encrypted<>'')`, ws).Scan(&retained)
				if !retained {
					t.Fatal("failed cleanup discarded durable journal")
				}
			})
		}
	}
}

func TestTeardownFenceAcquisitionErrorDiscardsSession(t *testing.T) {
	h := *testHandler
	h.TeardownFencePool = fenceTestPool(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// A PostgreSQL error after pool acquisition follows the same uncertain-
	// ownership path as an interrupted lock query.
	if _, err := h.acquireTeardownFence(ctx, "invalid-uuid"); err == nil {
		t.Fatal("invalid lock query succeeded")
	}
	if h.TeardownFencePool.Stat().AcquiredConns() != 0 {
		t.Fatal("failed acquire retained a connection")
	}
	next, err := h.acquireTeardownFence(ctx, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
}

func TestTeardownFenceMembershipRefusalsReturnConflict(t *testing.T) {
	for _, route := range []string{"remove", "leave"} {
		t.Run(route, func(t *testing.T) {
			fx := membershipKagentFixture(t)
			f := testutil.New(testPool, fx.WorkspaceID, fx.TargetUserID)
			teardownGuardEnrollment(t, f, fx.WorkspaceID, fx.RuntimeID, fx.AgentID, "present", "pending")
			handle := testHandler.DeleteMember
			if route == "leave" {
				handle = testHandler.LeaveWorkspace
			}
			req := membershipKagentRequest(fx, route)
			if route == "remove" {
				chi.RouteContext(req.Context()).URLParams.Add("id", fx.WorkspaceID)
			}
			result := testutil.Call(t, handle, req).Want(http.StatusConflict)
			if !strings.Contains(result.Text(), kagentTeardownBlockedMessage) {
				t.Fatalf("wrong conflict: %s", result.Text())
			}
			membershipKagentAssertIntact(t, fx)
		})
	}
}

func TestTeardownFenceBlocksAgentWritesForEveryProvider(t *testing.T) {
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	for _, provider := range []string{"claude", "kagent"} {
		for _, operation := range []string{"create", "update"} {
			t.Run(provider+"/"+operation, func(t *testing.T) {
				f, ws, rt := teardownGuardFixture(t, provider)
				dbfx.Cleanup(t, `DELETE FROM agent WHERE workspace_id=$1`, ws)
				dbfx.Cleanup(t, `DELETE FROM kagent_enrollment WHERE workspace_id=$1`, ws)
				var existing AgentResponse
				if operation == "update" {
					testutil.Call(t, testHandler.CreateAgent, teardownGuardCreateRequest(ws, rt)).Want(http.StatusCreated).JSON(&existing)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				fence, err := testHandler.acquireTeardownFence(ctx, ws)
				if err != nil {
					t.Fatal(err)
				}
				defer fence.Close()
				req := withRequestContext(teardownGuardCreateRequest(ws, rt), ctx)
				handle := testHandler.CreateAgent
				want := http.StatusCreated
				if operation == "update" {
					req = withRequestContext(withURLParam(newRequest("PATCH", "/agents/"+existing.ID, map[string]any{"name": "Updated"}), "id", existing.ID), ctx)
					req.Header.Set("X-Workspace-ID", ws)
					handle = testHandler.UpdateAgent
					want = http.StatusOK
				}
				result := testutil.Call(t, handle, req).Want(http.StatusConflict)
				if !strings.Contains(result.Text(), errTeardownFenceBusy.Error()) {
					t.Fatalf("wrong contention refusal: %s", result.Text())
				}
				var untouched bool
				if operation == "create" {
					f.QueryRow(t, `SELECT NOT EXISTS(SELECT 1 FROM agent WHERE workspace_id=$1) AND NOT EXISTS(SELECT 1 FROM kagent_enrollment WHERE workspace_id=$1)`, ws).Scan(&untouched)
				} else {
					f.QueryRow(t, `SELECT name='Concurrent enrollment' AND NOT EXISTS(SELECT 1 FROM kagent_enrollment WHERE agent_id=agent.id AND generation<>1) FROM agent WHERE id=$1`, existing.ID).Scan(&untouched)
				}
				if !untouched {
					t.Fatal("conflicting write changed configuration or lifecycle")
				}
				fence.Close()
				// Make a fresh request body for the retry.
				if operation == "create" {
					req = withRequestContext(teardownGuardCreateRequest(ws, rt), ctx)
				} else {
					req = withRequestContext(withURLParam(newRequest("PATCH", "/agents/"+existing.ID, map[string]any{"name": "Updated"}), "id", existing.ID), ctx)
					req.Header.Set("X-Workspace-ID", ws)
				}
				testutil.Call(t, handle, req).Want(want)
				if operation == "update" && provider == "kagent" {
					var committed bool
					f.QueryRow(t, `SELECT a.name='Updated' AND e.generation=2 FROM agent a JOIN kagent_enrollment e ON e.agent_id=a.id WHERE a.id=$1`, existing.ID).Scan(&committed)
					if !committed {
						t.Fatal("configuration and lifecycle update did not commit together")
					}
				}
			})
		}
	}
}
