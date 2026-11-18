//go:build integration && !windows

package daemon

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Run with -tags=integration and an explicit disposable DATABASE_URL. There is
// no default database or skip path: unavailable PostgreSQL must fail this test.
func TestKagentDBDaemonRuntimeRegistration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Fatal("DATABASE_URL is required for Kagent database integration")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("required PostgreSQL: %v", err)
	}
	fx := testutil.New(pool, "", "")
	suffix := uuid.NewString()
	userID := fx.User(t, "Kagent daemon DB", "kagent-db-"+suffix+"@example.test")
	wsID := fx.Workspace(t, "Kagent daemon DB", "kagent-db-"+suffix)
	fx.UserID, fx.WorkspaceID = userID, wsID
	fx.Member(t, wsID, userID, "owner")
	hub := realtime.NewHub()
	// Registration only publishes to the event bus; no websocket loop needed.
	h := handler.New(db.New(pool), pool, hub, events.New(), service.NewEmailService(), nil, nil, nil, handler.Config{})
	r := chi.NewRouter()
	r.Post("/api/daemon/register", h.DaemonRegister)
	// Same route and handler as cmd/server/router.go. Identity is supplied at
	// the middleware boundary, leaving real HTTP decoding and database writes.
	client := NewClient("http://multica.test")
	client.client.Transport = kagentWiringTransport(func(req *http.Request) (*http.Response, error) {
		req.Header.Set("X-User-ID", userID)
		req.Header.Set("X-Workspace-ID", wsID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		resp := rec.Result()
		resp.Request = req
		return resp, nil
	})
	configureWiringKagent(t)
	d := &Daemon{cfg: Config{DaemonID: "kagent-db-" + suffix, DeviceName: "DB fixture", Agents: map[string]AgentEntry{"kagent": {Path: "/missing/must-not-execute", Command: "kagent"}}}, client: client, logger: slog.Default()}
	builtins, rejected, unavailable := d.detectBuiltinRuntimes(t.Context())
	if len(builtins) != 1 || len(rejected) != 0 || len(unavailable) != 0 || builtins[0]["type"] != "kagent" {
		t.Fatalf("detected=%v rejected=%v unavailable=%v", builtins, rejected, unavailable)
	}
	var firstID string
	for i := 0; i < 2; i++ {
		resp, err := d.registerBuiltinRuntimesForWorkspaceLocked(t.Context(), wsID, builtins)
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Runtimes) != 1 {
			t.Fatalf("registration=%+v", resp)
		}
		id := resp.Runtimes[0].ID
		if i == 0 {
			firstID = id
			fx.Cleanup(t, `DELETE FROM agent_runtime WHERE id=$1`, id)
		} else if id != firstID {
			t.Fatalf("re-registration changed identity: %s -> %s", firstID, id)
		}
		var provider, mode, status, owner, workspace, daemonID string
		fx.QueryRow(t, `SELECT provider,runtime_mode,status,owner_id,workspace_id,daemon_id FROM agent_runtime WHERE id=$1`, id).Scan(&provider, &mode, &status, &owner, &workspace, &daemonID)
		if provider != "kagent" || mode != "local" || status != "online" || owner != userID || workspace != wsID || daemonID != d.cfg.DaemonID {
			t.Fatalf("persisted runtime=%s/%s/%s owner=%s workspace=%s daemon=%s", provider, mode, status, owner, workspace, daemonID)
		}
	}
	t.Setenv("MULTICA_KAGENT_ENABLED", "")
	builtins, _, _ = d.detectBuiltinRuntimes(t.Context())
	if len(builtins) != 0 {
		t.Fatalf("disabled Kagent advertised: %v", builtins)
	}
}
