package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type delayedRegisterDBTX struct {
	db.DBTX
	started chan struct{}
	release chan struct{}
}

type delayedProfileRegisterTxStarter struct {
	started chan struct{}
	release chan struct{}
}

func (s delayedProfileRegisterTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := testPool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return delayedProfileRegisterTx{Tx: tx, started: s.started, release: s.release}, nil
}

type delayedProfileRegisterTx struct {
	pgx.Tx
	started chan struct{}
	release chan struct{}
}

func (tx delayedProfileRegisterTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "-- name: UpsertAgentRuntimeWithProfile :one") {
		close(tx.started)
		<-tx.release
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func (d delayedRegisterDBTX) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return d.DBTX.Exec(ctx, sql, args...)
}

func (d delayedRegisterDBTX) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return d.DBTX.Query(ctx, sql, args...)
}

func (d delayedRegisterDBTX) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "-- name: UpsertAgentRuntime :one") {
		close(d.started)
		<-d.release
	}
	return d.DBTX.QueryRow(ctx, sql, args...)
}

func TestDaemonRegisterLateOldOwnerCannotOverwriteNewOwner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	daemonID := "register-generation-race-test"
	runtimeID := dbfx.Runtime(t, "delayed register", testutil.Cols{
		"daemon_id": daemonID, "runtime_mode": "local", "provider": "codex", "status": "online",
	})
	gate := delayedRegisterDBTX{DBTX: testPool, started: make(chan struct{}), release: make(chan struct{})}
	h := New(db.New(gate), testPool, testHandler.Hub, testHandler.Bus, testHandler.EmailService,
		nil, nil, analytics.NoopClient{}, Config{})
	register := func(handler *Handler, generation string) *httptest.ResponseRecorder {
		req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
			"workspace_id": testWorkspaceID, "daemon_id": daemonID,
			"runtimes": []map[string]any{{"name": "codex", "type": "codex", "status": "online", "owner_generation": generation}},
		}, testWorkspaceID, daemonID)
		w := httptest.NewRecorder()
		handler.DaemonRegister(w, req)
		return w
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- register(h, "g00000000000000000001:owner-A") }()
	select {
	case <-gate.started:
	case <-time.After(5 * time.Second):
		close(gate.release)
		t.Fatal("old owner register did not reach the delayed upsert")
	}
	if w := register(testHandler, "g00000000000000000002:owner-B"); w.Code != http.StatusOK {
		close(gate.release)
		t.Fatalf("new owner register status=%d: %s", w.Code, w.Body.String())
	}
	close(gate.release)
	select {
	case w := <-done:
		if w.Code != http.StatusOK && w.Code != http.StatusConflict {
			t.Fatalf("old owner register status=%d: %s", w.Code, w.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("delayed register did not finish")
	}
	var status, generation string
	dbfx.QueryRow(t, `SELECT status, metadata->>'owner_generation' FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&status, &generation)
	if status != "online" || generation != "g00000000000000000002:owner-B" {
		t.Fatalf("late register left status=%q generation=%q, want online owner-B", status, generation)
	}
	if w := register(testHandler, "g00000000000000000002:owner-B"); w.Code != http.StatusOK {
		t.Fatalf("same owner re-register status=%d: %s", w.Code, w.Body.String())
	}
	if w := register(testHandler, "g00000000000000000003:owner-C"); w.Code != http.StatusOK {
		t.Fatalf("legitimate takeover status=%d: %s", w.Code, w.Body.String())
	}
	dbfx.QueryRow(t, `SELECT metadata->>'owner_generation' FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&generation)
	if generation != "g00000000000000000003:owner-C" {
		t.Fatalf("takeover generation = %q", generation)
	}
}

func TestDaemonRegisterLateFailedProfileCannotOfflineNewOwner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	profileID := insertRuntimeProfileFixture(t, t.Context(), "delayed failure", "codex", "company-codex")
	daemonID := "register-failure-race-test"
	runtimeID := insertProfileRuntimeFixture(t, t.Context(), profileID, "delayed failure", "codex")
	dbfx.Exec(t, `UPDATE agent_runtime SET daemon_id = $1 WHERE id = $2`, daemonID, runtimeID)
	gate := delayedProfileRegisterTxStarter{started: make(chan struct{}), release: make(chan struct{})}
	h := New(db.New(testPool), testPool, testHandler.Hub, testHandler.Bus, testHandler.EmailService,
		nil, nil, analytics.NoopClient{}, Config{})
	h.TxStarter = gate
	register := func(handler *Handler, failure bool, generation string) *httptest.ResponseRecorder {
		body := map[string]any{"workspace_id": testWorkspaceID, "daemon_id": daemonID}
		if failure {
			body["failed_profiles"] = []map[string]any{{"profile_id": profileID, "reason": "command unavailable", "owner_generation": generation}}
		} else {
			body["runtimes"] = []map[string]any{{"name": "codex", "type": "codex", "profile_id": profileID, "status": "online", "owner_generation": generation}}
		}
		w := httptest.NewRecorder()
		handler.DaemonRegister(w, newDaemonTokenRequest("POST", "/api/daemon/register", body, testWorkspaceID, daemonID))
		return w
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- register(h, true, "g00000000000000000001:failure") }()
	select {
	case <-gate.started:
	case <-time.After(5 * time.Second):
		close(gate.release)
		t.Fatal("failed profile did not reach delayed upsert")
	}
	if w := register(testHandler, false, "g00000000000000000002:online"); w.Code != http.StatusOK {
		close(gate.release)
		t.Fatalf("runnable owner register status=%d: %s", w.Code, w.Body.String())
	}
	close(gate.release)
	select {
	case w := <-done:
		if w.Code != http.StatusOK {
			t.Fatalf("late failed profile status=%d: %s", w.Code, w.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("delayed failed profile did not finish")
	}
	var status, generation string
	dbfx.QueryRow(t, `SELECT status, metadata->>'owner_generation' FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&status, &generation)
	if status != "online" || generation != "g00000000000000000002:online" {
		t.Fatalf("late failure left status=%q generation=%q, want online under new owner", status, generation)
	}
}
