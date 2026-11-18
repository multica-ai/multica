package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Each workspace is private to this test; deleting it cannot touch suite fixtures.
func teardownGuardFixture(t *testing.T, provider string) (*testutil.Fixture, string, string) {
	t.Helper()
	ws := dbfx.Workspace(t, "Teardown guard", "teardown-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	f := testutil.New(testPool, ws, testUserID)
	rt := f.Runtime(t, "Teardown guard", testutil.Cols{"provider": provider, "runtime_mode": "local", "visibility": "public"})
	return f, ws, rt
}

func teardownGuardRequest(scope, ws, rt string, active []string) *http.Request {
	if scope == "workspace" {
		req := withURLParam(newRequest("DELETE", "/api/workspaces/"+ws, nil), "id", ws)
		req.Header.Set("X-Workspace-ID", ws)
		return req
	}
	method, path := "DELETE", "/api/runtimes/"+rt
	var body any
	if scope == "cascade" {
		method, path = "POST", path+"/unbind-agents-and-delete"
		body = map[string]any{"expected_active_agent_ids": active, "remote_deletion_verified": true}
	}
	if scope == "revoke" {
		method, path = "POST", "/api/runtimes/"+rt+"/revoke-workspace-access"
		body = map[string]any{"expected_nonowner_agent_ids": []string{}, "expected_task_ids": []string{}, "shared_with": []string{}}
	}
	req := withURLParam(newRequest(method, path, body), "runtimeId", rt)
	req.Header.Set("X-Workspace-ID", ws)
	return req
}

func teardownGuardHandler(h *Handler, scope string) http.HandlerFunc {
	switch scope {
	case "workspace":
		return h.DeleteWorkspace
	case "revoke":
		return h.RevokeRuntimeWorkspaceAccess
	case "cascade":
		return h.UnbindAgentsAndDeleteRuntime
	default:
		return h.DeleteAgentRuntime
	}
}

func teardownGuardEnrollment(t *testing.T, f *testutil.Fixture, ws, rt, agent, desired, phase string) {
	t.Helper()
	cols := testutil.Cols{"workspace_id": ws, "runtime_id": rt, "agent_id": agent, "desired_state": desired, "phase": phase}
	if phase == "ready" || phase == "deleted" {
		cols["applied_generation"] = 1
	}
	f.InsertNoID(t, "kagent_enrollment", cols, "agent_id=$1", agent)
}

func TestKagentTeardownGuardsRetainEveryEnrollmentState(t *testing.T) {
	states := []struct{ desired, phase string }{
		{"present", "pending"}, {"present", "ready"}, {"present", "blocked"},
		{"absent", "draining"}, {"absent", "deleting"}, {"absent", "blocked"}, {"absent", "deleted"},
	}
	for _, scope := range []string{"workspace", "runtime", "cascade", "revoke"} {
		for _, state := range states {
			t.Run(scope+"/"+state.desired+"/"+state.phase, func(t *testing.T) {
				f, ws, rt := teardownGuardFixture(t, "kagent")
				// Light DELETE must reach its transactional guard, past the active-agent check.
				over := testutil.Cols{}
				if scope == "runtime" {
					over["archived_at"] = testutil.Raw("now()")
				}
				a := f.Agent(t, "Enrolled", rt, over)
				task := f.Task(t, a, testutil.Cols{"runtime_id": rt})
				system := f.Agent(t, "System carrier", rt, testutil.Cols{"kind": "system"})
				teardownGuardEnrollment(t, f, ws, rt, a, state.desired, state.phase)
				response := testutil.Call(t, teardownGuardHandler(testHandler, scope), teardownGuardRequest(scope, ws, rt, []string{a})).Want(http.StatusConflict)
				if !strings.Contains(response.Text(), kagentTeardownBlockedMessage) {
					t.Fatalf("wrong refusal: %s", response.Text())
				}
				var runtimeID, status, desired, phase string
				f.QueryRow(t, `SELECT runtime_id FROM agent WHERE id=$1`, a).Scan(&runtimeID)
				if runtimeID != rt {
					t.Fatal("enrolled agent was unbound")
				}
				f.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, task).Scan(&status)
				if status != "queued" {
					t.Fatalf("task changed: %s", status)
				}
				f.QueryRow(t, `SELECT desired_state, phase FROM kagent_enrollment WHERE agent_id=$1`, a).Scan(&desired, &phase)
				if desired != state.desired || phase != state.phase {
					t.Fatalf("intent changed: %s/%s", desired, phase)
				}
				var intact bool
				f.QueryRow(t, `SELECT EXISTS(SELECT 1 FROM workspace WHERE id=$1) AND EXISTS(SELECT 1 FROM agent_runtime WHERE id=$2) AND EXISTS(SELECT 1 FROM agent WHERE id=$3)`, ws, rt, system).Scan(&intact)
				if !intact {
					t.Fatal("blocked teardown removed workspace, runtime or system agent")
				}
				var visibility string
				f.QueryRow(t, `SELECT visibility FROM agent_runtime WHERE id=$1`, rt).Scan(&visibility)
				if visibility != "public" {
					t.Fatalf("blocked teardown changed runtime access: %s", visibility)
				}

			})
		}
	}
}

func TestKagentTeardownGuardsRetainOrphanTombstonesRegardlessOfProvider(t *testing.T) {
	for _, scope := range []string{"workspace", "runtime", "cascade", "revoke"} {
		t.Run(scope, func(t *testing.T) {
			f, ws, rt := teardownGuardFixture(t, "claude")
			a := uuid.NewString() // No agent exists to join against.
			teardownGuardEnrollment(t, f, ws, rt, a, "absent", "deleted")
			response := testutil.Call(t, teardownGuardHandler(testHandler, scope), teardownGuardRequest(scope, ws, rt, nil)).Want(http.StatusConflict)
			if !strings.Contains(response.Text(), kagentTeardownBlockedMessage) {
				t.Fatalf("wrong refusal: %s", response.Text())
			}
			var retained bool
			f.QueryRow(t, `SELECT EXISTS(SELECT 1 FROM kagent_enrollment WHERE agent_id=$1)`, a).Scan(&retained)
			if !retained {
				t.Fatal("tombstone deleted")
			}
		})
	}
}

func TestKagentTeardownGuardsAllowUnenrolledTeardown(t *testing.T) {
	for _, scope := range []string{"workspace", "runtime", "cascade"} {
		for _, provider := range []string{"claude", "kagent"} {
			t.Run(scope+"/"+provider, func(t *testing.T) {
				f, ws, rt := teardownGuardFixture(t, provider)
				// Another runtime's enrollment in another workspace must not block this one.
				other, otherWS, otherRT := teardownGuardFixture(t, "kagent")
				teardownGuardEnrollment(t, other, otherWS, otherRT, uuid.NewString(), "absent", "deleted")
				a := f.Agent(t, "Unenrolled", rt, testutil.Cols{"archived_at": testutil.Raw("now()")})
				task := f.Task(t, a, testutil.Cols{"runtime_id": rt, "status": "completed", "completed_at": testutil.Raw("now()")})
				want := http.StatusOK
				if scope == "workspace" {
					want = http.StatusNoContent
				}
				testutil.Call(t, teardownGuardHandler(testHandler, scope), teardownGuardRequest(scope, ws, rt, nil)).Want(want)
				var exists bool
				f.QueryRow(t, `SELECT EXISTS(SELECT 1 FROM agent_runtime WHERE id=$1)`, rt).Scan(&exists)
				if exists {
					t.Fatal("runtime retained without enrollment")
				}
				if scope == "workspace" {
					f.QueryRow(t, `SELECT EXISTS(SELECT 1 FROM workspace WHERE id=$1)`, ws).Scan(&exists)
					if exists {
						t.Fatal("workspace retained without enrollment")
					}
				} else {
					var unbound bool
					f.QueryRow(t, `SELECT runtime_id IS NULL FROM agent WHERE id=$1`, a).Scan(&unbound)
					if !unbound {
						t.Fatal("ordinary agent was not unbound")
					}
					f.QueryRow(t, `SELECT runtime_id IS NULL FROM agent_task_queue WHERE id=$1`, task).Scan(&unbound)
					if !unbound {
						t.Fatal("task history was not unbound")
					}
				}
			})
		}
	}
}

// Pause immediately before a real transaction commits, keeping all its locks.
// Use separate Handler copies and pinned connections so no other request is paused.
type teardownGuardCommitBarrier struct {
	txStarter
	entered chan struct{}
	release chan struct{}
}

func (b teardownGuardCommitBarrier) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := b.txStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return teardownGuardBarrierTx{Tx: tx, entered: b.entered, release: b.release}, nil
}

type teardownGuardBarrierTx struct {
	pgx.Tx
	entered chan struct{}
	release chan struct{}
}

func (tx teardownGuardBarrierTx) Commit(ctx context.Context) error {
	close(tx.entered)
	select {
	case <-tx.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return tx.Tx.Commit(ctx)
}

func teardownGuardAwait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("handler did not reach the transaction barrier or finish")
	}
}

func teardownGuardCreateRequest(ws, rt string) *http.Request {
	req := newRequest("POST", "/api/agents", map[string]any{"name": "Concurrent enrollment", "runtime_id": rt, "runtime_config": map[string]string{"mode": "native-text"}})
	req.Header.Set("X-Workspace-ID", ws)
	return req
}

func TestKagentTeardownGuardsSerializeWithEnrollmentCreation(t *testing.T) {
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	// The workspace scope is canonically covered by TestTeardownFence*: workspace
	// deletion now begins on the dedicated TeardownFencePool connection and
	// acquires the per-workspace session advisory fence before its checks, so
	// enrollment contention yields an immediate 409 workspace_teardown_busy
	// instead of waiting on a workspace row lock. The runtime and cascade scopes
	// still serialize on the runtime row lock, which this barrier technique
	// remains valid for.
	for _, scope := range []string{"runtime", "cascade"} {
		for _, first := range []string{"enrollment", "teardown"} {
			t.Run(scope+"/"+first+"-first", func(t *testing.T) {
				_, ws, rt := teardownGuardFixture(t, "kagent")
				dbfx.Cleanup(t, `DELETE FROM agent WHERE workspace_id=$1`, ws)
				dbfx.Cleanup(t, `DELETE FROM kagent_enrollment WHERE workspace_id=$1`, ws)
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				creatorConn, err := testPool.Acquire(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer creatorConn.Release()
				deleterConn, err := testPool.Acquire(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer deleterConn.Release()
				creator, deleter := *testHandler, *testHandler
				creator.TxStarter, deleter.TxStarter = creatorConn, deleterConn
				entered, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()
				barrier := teardownGuardCommitBarrier{entered: entered, release: release}
				if first == "enrollment" {
					barrier.txStarter = creatorConn
					creator.TxStarter = barrier
				} else {
					barrier.txStarter = deleterConn
					deleter.TxStarter = barrier
				}
				created, deleted := httptest.NewRecorder(), httptest.NewRecorder()
				createDone, deleteDone := make(chan struct{}), make(chan struct{})
				create := func() {
					defer close(createDone)
					creator.CreateAgent(created, withRequestContext(teardownGuardCreateRequest(ws, rt), ctx))
				}
				remove := func() {
					defer close(deleteDone)
					req := teardownGuardRequest(scope, ws, rt, nil).WithContext(ctx)
					if scope == "workspace" {
						req = withURLParam(req, "id", ws)
					} else {
						req = withURLParam(req, "runtimeId", rt)
					}
					teardownGuardHandler(&deleter, scope)(deleted, req)
				}
				// Cleanup is registered after fixture cleanup, and runs before it. Always
				// release barriers and join requests before returning their connections.
				defer func() { unblock(); cancel(); <-createDone; <-deleteDone }()
				if first == "enrollment" {
					go create()
					// If creation fails before Commit, finish both channels before reporting.
					select {
					case <-entered:
					case <-createDone:
						close(deleteDone)
						t.Fatalf("create failed before barrier: %s", created.Body.String())
					case <-ctx.Done():
						close(deleteDone)
						t.Fatal(ctx.Err())
					}
					go remove()
					waitForBlockedWriter(t, ctx, int32(deleterConn.Conn().PgConn().PID()))
				} else {
					go remove()
					select {
					case <-entered:
					case <-deleteDone:
						close(createDone)
						t.Fatalf("delete failed before barrier: %s", deleted.Body.String())
					case <-ctx.Done():
						close(createDone)
						t.Fatal(ctx.Err())
					}
					go create()
					waitForBlockedWriter(t, ctx, int32(creatorConn.Conn().PgConn().PID()))
				}
				unblock()
				teardownGuardAwait(t, createDone)
				teardownGuardAwait(t, deleteDone)
				var count int
				dbfx.QueryRow(t, `SELECT count(*) FROM kagent_enrollment WHERE workspace_id=$1 AND runtime_id=$2`, ws, rt).Scan(&count)
				if first == "enrollment" {
					if created.Code != http.StatusCreated || deleted.Code != http.StatusConflict || !strings.Contains(deleted.Body.String(), kagentTeardownBlockedMessage) || count != 1 {
						t.Fatalf("create=%d %s delete=%d %s enrollments=%d", created.Code, created.Body.String(), deleted.Code, deleted.Body.String(), count)
					}
				} else if (deleted.Code != http.StatusOK && deleted.Code != http.StatusNoContent) || created.Code < 400 || count != 0 {
					t.Fatalf("delete=%d %s create=%d %s enrollments=%d", deleted.Code, deleted.Body.String(), created.Code, created.Body.String(), count)
				}
			})
		}
	}
}
