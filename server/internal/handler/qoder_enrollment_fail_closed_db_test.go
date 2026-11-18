package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Real creation must commit the agent and intent together before teardown can
// inspect either. A conflicting teardown gets a retryable response immediately.
func TestQoderEnrollmentCreationFirst(t *testing.T) {
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	withVCSBox(t)
	for _, scope := range []string{"workspace", "disconnect", "background"} {
		t.Run(scope, func(t *testing.T) {
			f, ws, rt := teardownGuardFixture(t, "kagent")
			installFenceQoder(t, f, ws, "https://example.invalid", "")
			dbfx.Cleanup(t, `DELETE FROM agent WHERE workspace_id=$1`, ws)
			dbfx.Cleanup(t, `DELETE FROM kagent_enrollment WHERE workspace_id=$1`, ws)
			creator := *testHandler
			entered, release := make(chan struct{}), make(chan struct{})
			creator.TxStarter = teardownGuardCommitBarrier{txStarter: testPool, entered: entered, release: release}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				defer close(done)
				creator.CreateAgent(response, withRequestContext(teardownGuardCreateRequest(ws, rt), ctx))
			}()
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer func() { unblock(); cancel(); <-done }()
			select {
			case <-entered:
			case <-done:
				t.Fatalf("creation failed: %d %s", response.Code, response.Body)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if scope == "background" {
				if err := testHandler.cleanupQoderJournal(ctx, ws); !errors.Is(err, errTeardownFenceBusy) {
					t.Fatalf("cleanup during creation: %v", err)
				}
			} else {
				handle, req, _ := fenceCleanupRequest(scope, ws, rt)
				testutil.Call(t, handle, withRequestContext(req, ctx)).Want(http.StatusConflict)
			}
			unblock()
			teardownGuardAwait(t, done)
			if response.Code != http.StatusCreated {
				t.Fatalf("create: %d %s", response.Code, response.Body)
			}
			var count int
			f.QueryRow(t, `SELECT count(*) FROM agent a JOIN kagent_enrollment e ON e.agent_id=a.id WHERE a.workspace_id=$1 AND e.workspace_id=$1 AND e.runtime_id=$2`, ws, rt).Scan(&count)
			if count != 1 {
				t.Fatalf("atomic creation: %d joined rows", count)
			}
			if scope == "background" {
				if err := testHandler.cleanupQoderJournal(ctx, ws); !errors.Is(err, service.ErrRuntimeKagentEnrollment) {
					t.Fatalf("committed intent ignored: %v", err)
				}
			} else {
				handle, req, _ := fenceCleanupRequest(scope, ws, rt)
				result := testutil.Call(t, handle, withRequestContext(req, ctx)).Want(http.StatusConflict)
				if !strings.Contains(result.Text(), kagentTeardownBlockedMessage) {
					t.Fatalf("intent ignored: %s", result.Text())
				}
			}
		})
	}
}

func installFenceQoder(t *testing.T, f *testutil.Fixture, ws, baseURL, journal string) {
	t.Helper()
	f.Runtime(t, "Qoder", testutil.Cols{"provider": "qoder_cloud", "runtime_mode": "cloud"})
	config, err := testHandler.sealVCSSecret(fmt.Sprintf(`{"base_url":%q,"qoder_token":"fixture"}`, baseURL))
	if err != nil {
		t.Fatal(err)
	}
	encrypted := ""
	if journal != "" {
		encrypted, err = testHandler.sealVCSSecret(journal)
		if err != nil {
			t.Fatal(err)
		}
	}
	token := f.Insert(t, "daemon_token", testutil.Cols{"workspace_id": ws, "daemon_id": "fence-" + ws, "token_hash": "fence-" + ws, "expires_at": testutil.Raw("now() + interval '1 hour'")})
	f.InsertNoID(t, "cloud_agent_connection", testutil.Cols{"workspace_id": ws, "provider": "qoder_cloud", "owner_id": testUserID, "token_id": token, "config_encrypted": config, "journal_encrypted": encrypted, "enabled": true}, "workspace_id=$1 AND provider='qoder_cloud'", ws)
}

func fenceCleanupRequest(scope, ws, rt string) (http.HandlerFunc, *http.Request, int) {
	if scope == "workspace" {
		return testHandler.DeleteWorkspace, teardownGuardRequest("workspace", ws, rt, nil), http.StatusNoContent
	}
	req := withURLParam(newRequest("DELETE", "/qoder", map[string]any{"expected_active_agent_ids": []string{}}), "id", ws)
	req.Header.Set("X-Workspace-ID", ws)
	return testHandler.DisconnectQoderConnection, req, http.StatusOK
}

// The remote server pauses after the stop transaction committed and before
// journal/final deletion transactions. The session fence must span this gap.
func TestQoderEnrollmentCleanupFirstAcrossTransactionGap(t *testing.T) {
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
			if scope == "background" {
				f.Exec(t, `UPDATE cloud_agent_connection SET enabled=false WHERE workspace_id=$1`, ws)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			response := httptest.NewRecorder()
			done := make(chan struct{})
			_, req, want := fenceCleanupRequest(scope, ws, rt)
			go func() {
				defer close(done)
				if scope == "background" {
					testHandler.cleanupInactiveQoder(ctx)
					return
				}
				handle, _, _ := fenceCleanupRequest(scope, ws, rt)
				handle(response, withRequestContext(req, ctx))
			}()
			defer func() { unblock(); cancel(); <-done }()
			select {
			case <-entered:
			case <-done:
				t.Fatalf("cleanup missed remote gap: %d %s", response.Code, response.Body)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			// Stop has committed, but the cleanup journal is still durable.
			var retained bool
			f.QueryRow(t, `SELECT NOT enabled AND journal_encrypted<>'' FROM cloud_agent_connection WHERE workspace_id=$1 AND provider='qoder_cloud'`, ws).Scan(&retained)
			if !retained {
				t.Fatal("stop or journal persistence missing at transaction gap")
			}
			result := testutil.Call(t, testHandler.CreateAgent, withRequestContext(teardownGuardCreateRequest(ws, rt), ctx)).Want(http.StatusConflict)
			if !strings.Contains(result.Text(), errTeardownFenceBusy.Error()) {
				t.Fatalf("wrong refusal: %s", result.Text())
			}
			// An unrelated workspace is never held behind this owner's remote call.
			_, otherWS, otherRT := teardownGuardFixture(t, "claude")
			dbfx.Cleanup(t, `DELETE FROM agent WHERE workspace_id=$1`, otherWS)
			testutil.Call(t, testHandler.CreateAgent, withRequestContext(teardownGuardCreateRequest(otherWS, otherRT), ctx)).Want(http.StatusCreated)
			unblock()
			teardownGuardAwait(t, done)
			if scope != "background" && response.Code != want {
				t.Fatalf("cleanup: %d %s", response.Code, response.Body)
			}
			var empty bool
			f.QueryRow(t, `SELECT NOT EXISTS(SELECT 1 FROM kagent_enrollment WHERE workspace_id=$1) AND NOT EXISTS(SELECT 1 FROM agent WHERE workspace_id=$1)`, ws).Scan(&empty)
			if !empty {
				t.Fatal("conflicting create wrote agent or intent")
			}
			if scope == "background" {
				f.QueryRow(t, `SELECT journal_encrypted='' FROM cloud_agent_connection WHERE workspace_id=$1 AND provider='qoder_cloud'`, ws).Scan(&empty)
				if !empty {
					t.Fatal("background cleanup did not finish")
				}
			}
		})
	}
}

func TestQoderEnrollmentOrdinaryCreation(t *testing.T) {
	_, ws, rt := teardownGuardFixture(t, "claude")
	var created AgentResponse
	testutil.Call(t, testHandler.CreateAgent, teardownGuardCreateRequest(ws, rt)).Want(http.StatusCreated).JSON(&created)
	dbfx.Cleanup(t, `DELETE FROM agent WHERE id=$1`, created.ID)
	if created.ID == "" {
		t.Fatal("ordinary creation returned no agent")
	}
}

// Pause on a real workspace row lock after stale preflight authorization, then
// change provider, membership, or runtime access in the winning transaction.
func TestQoderEnrollmentRechecksAuthorization(t *testing.T) {
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	for _, operation := range []string{"create", "update"} {
		for _, change := range []string{"provider", "membership", "access"} {
			t.Run(operation+"/"+change, func(t *testing.T) {
				f, ws, rt := teardownGuardFixture(t, "claude")
				dbfx.Cleanup(t, `DELETE FROM agent WHERE workspace_id=$1`, ws)
				var a string
				if operation == "update" {
					a = f.Agent(t, "Existing", rt)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				conn, err := testPool.Acquire(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Release()
				holder, err := testPool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer holder.Rollback(context.Background())
				if _, err = holder.Exec(ctx, `SELECT id FROM workspace WHERE id=$1 FOR UPDATE`, ws); err != nil {
					t.Fatal(err)
				}
				h := *testHandler
				h.TxStarter = conn
				response := httptest.NewRecorder()
				done := make(chan struct{})
				go func() {
					defer close(done)
					if operation == "create" {
						h.CreateAgent(response, withRequestContext(teardownGuardCreateRequest(ws, rt), ctx))
						return
					}
					req := withURLParam(newRequest("PATCH", "/agents/"+a, map[string]any{"runtime_id": rt, "name": "Updated"}), "id", a)
					req.Header.Set("X-Workspace-ID", ws)
					h.UpdateAgent(response, withRequestContext(req, ctx))
				}()
				defer func() { cancel(); _ = holder.Rollback(context.Background()); <-done }()
				waitForBlockedWriter(t, ctx, int32(conn.Conn().PgConn().PID()))
				switch change {
				case "provider":
					_, err = holder.Exec(ctx, `UPDATE agent_runtime SET provider='kagent' WHERE id=$1`, rt)
				case "membership":
					_, err = holder.Exec(ctx, `DELETE FROM member WHERE workspace_id=$1 AND user_id=$2`, ws, testUserID)
				case "access":
					_, err = holder.Exec(ctx, `UPDATE agent_runtime SET visibility='private',owner_id=NULL,shared_with='{}' WHERE id=$1`, rt)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err = holder.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				teardownGuardAwait(t, done)
				want := http.StatusForbidden
				if change == "provider" {
					want = http.StatusConflict
				}
				if response.Code != want {
					t.Fatalf("authorization: %d %s; want %d", response.Code, response.Body, want)
				}
				var empty bool
				f.QueryRow(t, `SELECT NOT EXISTS(SELECT 1 FROM kagent_enrollment WHERE workspace_id=$1) AND NOT EXISTS(SELECT 1 FROM agent WHERE workspace_id=$1 AND name IN ('Concurrent enrollment','Updated'))`, ws).Scan(&empty)
				if !empty {
					t.Fatal("stale preflight wrote agent or enrollment")
				}
			})
		}
	}
}
