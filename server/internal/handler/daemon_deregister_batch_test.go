package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The MUL-6788 batch rewrite collapsed the per-runtime GetAgentRuntime loop in
// DaemonDeregister into one GetAgentRuntimes read. These tests pin the two
// behaviours the review called out: the success path still processes existing
// ids and skips missing ones — resolving a differently-cased id to its row via
// the canonical-UUID map key — and a FAILED batch read must fail closed with
// 500 instead of reporting a successful deregister while every runtime silently
// stays online.

var errInjectedRuntimeBatchRead = errors.New("injected runtime batch read failure")

// runtimeBatchFailDBTX passes every statement through to the real pool EXCEPT
// the batched GetAgentRuntimes read, which it fails.
type runtimeBatchFailDBTX struct{ inner db.DBTX }

func (f runtimeBatchFailDBTX) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return f.inner.Exec(ctx, sql, args...)
}

func (f runtimeBatchFailDBTX) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "FROM agent_runtime") && strings.Contains(sql, "= ANY(") {
		return nil, errInjectedRuntimeBatchRead
	}
	return f.inner.Query(ctx, sql, args...)
}

func (f runtimeBatchFailDBTX) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f.inner.QueryRow(ctx, sql, args...)
}

// newRuntimeBatchReadFailureHandler is testHandler with the batched runtime
// read broken. It shares the same pool, hub and bus so everything else behaves
// normally.
func newRuntimeBatchReadFailureHandler(t *testing.T) *Handler {
	t.Helper()
	return New(
		db.New(runtimeBatchFailDBTX{inner: testPool}),
		testPool,
		testHandler.Hub,
		testHandler.Bus,
		testHandler.EmailService,
		nil,
		nil,
		analytics.NoopClient{},
		Config{},
	)
}

func runtimeStatus(t *testing.T, runtimeID string) string {
	t.Helper()
	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&status)
	return status
}

// TestDaemonDeregisterMixedRuntimeIDs covers the success path after batching:
// two distinct existing runtimes both go offline — one requested by its own
// (lowercase) id and the other requested ONLY by its upper-cased form, which
// proves the canonical-UUID map keying resolves a differently-cased id — while
// a well-formed but never-registered id is skipped. Using two separate
// runtimes (rather than one id in two cases) is deliberate: if the uppercase
// form wrongly fell through to "not found", a single-runtime test would still
// pass because the lowercase form already flipped that row offline.
func TestDaemonDeregisterMixedRuntimeIDs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	lower := dbfx.Runtime(t, "deregister mixed lower", testutil.Cols{"status": "online"})
	upperOnly := dbfx.Runtime(t, "deregister mixed upper", testutil.Cols{"status": "online"})
	// A well-formed UUID that is not in the table.
	missing := "00000000-0000-0000-0000-0000000009ff"

	req := newRequest("POST", "/api/daemon/deregister", map[string]any{
		"runtime_ids": []string{lower, missing, strings.ToUpper(upperOnly)},
	})
	testutil.Call(t, testHandler.DaemonDeregister, req).Want(http.StatusOK)

	if got := runtimeStatus(t, lower); got != "offline" {
		t.Errorf("lowercase-requested runtime status = %q, want offline", got)
	}
	if got := runtimeStatus(t, upperOnly); got != "offline" {
		t.Errorf("uppercase-only-requested runtime status = %q, want offline (canonical UUID map key must resolve it)", got)
	}
}

// TestDaemonDeregisterBatchReadErrorFailsClosed pins the core review fix: when
// the batch runtime lookup fails, the handler must return 500 and leave the
// runtime online — not swallow the error, skip every id, and report 200 ok
// while the runtime keeps attracting claims until the liveness sweep reaps it.
func TestDaemonDeregisterBatchReadErrorFailsClosed(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	h := newRuntimeBatchReadFailureHandler(t)

	online := dbfx.Runtime(t, "deregister batch-error online", testutil.Cols{"status": "online"})

	req := newRequest("POST", "/api/daemon/deregister", map[string]any{
		"runtime_ids": []string{online},
	})
	testutil.Call(t, h.DaemonDeregister, req).Want(http.StatusInternalServerError)

	if got := runtimeStatus(t, online); got != "online" {
		t.Errorf("runtime status = %q, want online (deregister must not have taken effect)", got)
	}
}
