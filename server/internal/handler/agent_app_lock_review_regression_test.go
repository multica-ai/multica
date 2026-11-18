package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/testutil"
)

type appLockReviewStarter struct {
	txStarter
	before     func(string, uint32)
	locked     func()
	failCommit bool
}

type appLockReviewTx struct {
	pgx.Tx
	starter appLockReviewStarter
}

type appLockReviewRow struct {
	pgx.Row
	locked func()
}

func (s appLockReviewStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &appLockReviewTx{Tx: tx, starter: s}, nil
}

func (tx *appLockReviewTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx.starter.before != nil {
		tx.starter.before(sql, tx.Conn().PgConn().PID())
	}
	row := tx.Tx.QueryRow(ctx, sql, args...)
	if strings.Contains(sql, "FROM agent WHERE") && strings.Contains(sql, "FOR UPDATE") && tx.starter.locked != nil {
		return appLockReviewRow{Row: row, locked: tx.starter.locked}
	}
	return row
}

func (tx *appLockReviewTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx.starter.before != nil {
		tx.starter.before(sql, tx.Conn().PgConn().PID())
	}
	return tx.Tx.Exec(ctx, sql, args...)
}

func (row appLockReviewRow) Scan(dest ...any) error {
	if err := row.Row.Scan(dest...); err != nil {
		return err
	}
	row.locked()
	return nil
}

func (tx *appLockReviewTx) Commit(ctx context.Context) error {
	if tx.starter.failCommit {
		if err := tx.Tx.Rollback(ctx); err != nil {
			return err
		}
		return errors.New("injected app lock review commit failure")
	}
	return tx.Tx.Commit(ctx)
}

// The metadata endpoint holds the agent lock while the apps endpoint starts.
// PostgreSQL must report the latter waiting on that lock before any app write.
// Exercise successful commits and either endpoint's rollback with real handlers.
func TestAgentAppLockReviewConcurrentEndpoints(t *testing.T) {
	for _, failure := range []string{"none", "agent", "apps"} {
		t.Run(failure, func(t *testing.T) {
			f := newAppsFixture(t)
			if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "read"}}); w.Code != http.StatusOK {
				t.Fatalf("seed apps: %d %s", w.Code, w.Body.String())
			}
			agentHandler, appsHandler := *f.h, *f.h
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			acquired, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			agentHandler.TxStarter = appLockReviewStarter{txStarter: f.h.TxStarter, failCommit: failure == "agent", locked: func() {
				close(acquired)
				select {
				case <-release:
				case <-ctx.Done():
				}
			}}
			firstSQL, pid := make(chan string, 1), make(chan uint32, 1)
			var first sync.Once
			appsHandler.TxStarter = appLockReviewStarter{txStarter: f.h.TxStarter, failCommit: failure == "apps", before: func(sql string, p uint32) {
				first.Do(func() { firstSQL <- sql; pid <- p })
			}}
			agentReq := withURLParam(newRequest("PUT", "/api/agents/"+f.agentID, map[string]any{
				"instructions": "lock review saved", "composio_toolkit_allowlist": []string{},
			}), "id", f.agentID)
			appsReq := withURLParam(appsReqAs(t, testUserID, "PUT", "/api/agents/"+f.agentID+"/apps", map[string]any{
				"apps": []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "write"}},
			}), "id", f.agentID)
			agentCtx, agentCancel := context.WithTimeout(agentReq.Context(), 10*time.Second)
			defer agentCancel()
			appsCtx, appsCancel := context.WithTimeout(appsReq.Context(), 10*time.Second)
			defer appsCancel()
			agentReq, appsReq = agentReq.WithContext(agentCtx), appsReq.WithContext(appsCtx)
			agentDone, appsDone := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
			go func() { w := httptest.NewRecorder(); agentHandler.UpdateAgent(w, agentReq); agentDone <- w }()
			select {
			case <-acquired:
			case <-ctx.Done():
				t.Fatal("agent endpoint never acquired lock")
			}
			go func() { w := httptest.NewRecorder(); appsHandler.UpdateAgentApps(w, appsReq); appsDone <- w }()
			select {
			case sql := <-firstSQL:
				if !strings.Contains(sql, "FROM agent WHERE") || !strings.Contains(sql, "FOR UPDATE") || !strings.Contains(sql, "workspace_id") {
					t.Fatalf("apps endpoint must lock scoped agent first: %s", sql)
				}
			case <-ctx.Done():
				t.Fatal("apps endpoint never attempted transaction")
			}
			appPID := <-pid
			for {
				var blocked bool
				if err := testPool.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, appPID).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				select {
				case <-time.After(10 * time.Millisecond):
				case <-ctx.Done():
					t.Fatal("apps endpoint did not wait for agent lock")
				}
			}
			close(release)
			for name, done := range map[string]chan *httptest.ResponseRecorder{"agent": agentDone, "apps": appsDone} {
				select {
				case w := <-done:
					want := http.StatusOK
					if failure == name {
						want = http.StatusInternalServerError
					}
					if w.Code != want {
						t.Fatalf("%s: %d %s, want %d", name, w.Code, w.Body.String(), want)
					}
				case <-ctx.Done():
					t.Fatalf("%s endpoint did not finish", name)
				}
			}
			var instructions string
			var slugs []string
			dbfx.QueryRow(t, `SELECT instructions,composio_toolkit_allowlist FROM agent WHERE id=$1`, f.agentID).Scan(&instructions, &slugs)
			if (instructions == "lock review saved") != (failure != "agent") {
				t.Fatalf("incorrect metadata rollback: %q", instructions)
			}
			rows, err := f.h.Queries.ListAgentApps(t.Context(), parseUUID(f.agentID))
			if err != nil {
				t.Fatal(err)
			}
			if failure == "apps" {
				if len(rows) != 0 || len(slugs) != 0 {
					t.Fatalf("app rollback was partial: %+v %v", rows, slugs)
				}
			} else if len(rows) != 1 || rows[0].Level != "write" || len(slugs) != 1 || slugs[0] != "gmail" {
				t.Fatalf("app commit was partial: %+v %v", rows, slugs)
			}
		})
	}
}

func TestAgentAppLockReviewRejectsChangedScopeAndOwner(t *testing.T) {
	for _, endpoint := range []string{"agent", "apps"} {
		for _, change := range []string{"workspace", "owner"} {
			t.Run(endpoint+"/"+change, func(t *testing.T) {
				f := newAppsFixture(t)
				otherWorkspace := dbfx.Workspace(t, "Lock review other", "lock-review-"+f.agentID)
				// A regular member initially owns the agent; an ownership change
				// must revoke management even after the initial authorization.
				dbfx.Exec(t, `UPDATE agent SET owner_id=$2 WHERE id=$1`, f.agentID, f.member)
				h := *f.h
				h.TxStarter = hookedTxStarter{base: f.h.TxStarter, hook: func() error {
					sql := `UPDATE agent SET owner_id=$2 WHERE id=$1`
					value := testUserID
					if change == "workspace" {
						sql = `UPDATE agent SET workspace_id=$2 WHERE id=$1`
						value = otherWorkspace
					}
					_, err := testPool.Exec(t.Context(), sql, f.agentID, value)
					return err
				}}
				body := map[string]any{"name": "unauthorized edit"}
				handler := h.UpdateAgent
				if endpoint == "apps" {
					body = map[string]any{"apps": []any{}}
					handler = h.UpdateAgentApps
				}
				req := withURLParam(appsReqAs(t, f.member, "PUT", "/api/agents/"+f.agentID, body), "id", f.agentID)
				status := http.StatusForbidden
				if change == "workspace" {
					status = http.StatusNotFound
				}
				testutil.Call(t, handler, req).Want(status)
			})
		}
	}
}
