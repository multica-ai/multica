package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/runtimeapps"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

type submissionCommitBarrier struct {
	pool    *pgxpool.Pool
	ready   chan struct{}
	proceed chan struct{}
}

type submissionBarrierTx struct {
	pgx.Tx
	barrier *submissionCommitBarrier
}

func (b *submissionCommitBarrier) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := b.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return submissionBarrierTx{tx, b}, nil
}

func (tx submissionBarrierTx) Commit(ctx context.Context) error {
	close(tx.barrier.ready)
	select {
	case <-tx.barrier.proceed:
		return tx.Tx.Commit(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

type submissionBeginBarrier submissionCommitBarrier

func (b *submissionBeginBarrier) Begin(ctx context.Context) (pgx.Tx, error) {
	close(b.ready)
	select {
	case <-b.proceed:
		return b.pool.Begin(ctx)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type submissionHTTPResult struct {
	status int
	body   map[string]any
	err    error
}

func submissionAsync(serverURL, workspace, method, path string, body any) <-chan submissionHTTPResult {
	result := make(chan submissionHTTPResult, 1)
	go func() {
		status, response, err := submissionHTTP(serverURL, testToken, workspace, method, path, body)
		result <- submissionHTTPResult{status, response, err}
	}()
	return result
}

func submissionResult(t *testing.T, results <-chan submissionHTTPResult, want int) submissionHTTPResult {
	t.Helper()
	select {
	case result := <-results:
		if result.err != nil || result.status != want {
			t.Fatalf("HTTP result=%+v want status=%d", result, want)
		}
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("HTTP request did not finish")
		return submissionHTTPResult{}
	}
}

func TestChatSubmissionConcurrentParentDelete(t *testing.T) {
	for _, parent := range []string{"session", "workspace"} {
		t.Run(parent, func(t *testing.T) {
			fx := workspaceFixture(t)
			workspace := fx.Workspace(t, "Submission deletion", "delete-"+uuid.NewString())
			fx.Member(t, workspace, testUserID, "owner")
			runtime := fx.Runtime(t, "Offline synthetic runtime", testutil.Cols{"workspace_id": workspace, "owner_id": testUserID})
			agent := fx.Agent(t, "Submission deletion agent", runtime, testutil.Cols{"workspace_id": workspace})
			created := submissionStatus(t, testServer.URL, testToken, workspace, "POST", "/api/chat/sessions", map[string]any{"agent_id": agent}, http.StatusCreated)
			sessionID := created["id"].(string)
			cleanupSubmissionSession(t, testToken, workspace, sessionID)
			hub := realtime.NewHub()
			go hub.Run()
			router, h := NewRouterWithOptions(testPool, hub, events.New(), analytics.NoopClient{}, nil, RouterOptions{})
			server := httptest.NewServer(router)
			t.Cleanup(server.Close)
			barrier := &submissionCommitBarrier{testPool, make(chan struct{}), make(chan struct{})}
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(barrier.proceed) }) })
			h.TaskService.TxStarter = barrier
			body := map[string]any{"content": "delete race", "submission_id": uuid.NewString()}
			send := submissionAsync(server.URL, workspace, "POST", "/api/chat/sessions/"+sessionID+"/messages", body)
			select {
			case <-barrier.ready:
			case <-time.After(10 * time.Second):
				t.Fatal("send did not reach its pre-commit barrier")
			}
			deletePath := "/api/chat/sessions/" + sessionID
			if parent == "workspace" {
				deletePath = "/api/workspaces/" + workspace
			}
			deleted := submissionAsync(server.URL, workspace, "DELETE", deletePath, nil)
			deadline := time.Now().Add(5 * time.Second)
			blocked := false
			for time.Now().Before(deadline) {
				var waiting int
				fx.QueryRow(t, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'`).Scan(&waiting)
				if waiting > 0 {
					blocked = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !blocked {
				t.Fatal("parent delete did not wait for the send's session lock")
			}
			release.Do(func() { close(barrier.proceed) })
			submissionResult(t, send, http.StatusCreated)
			submissionResult(t, deleted, http.StatusNoContent)
			submissionCounts(t, sessionID, 0, 0, 0)
			submissionStatus(t, server.URL, testToken, workspace, "POST", "/api/chat/sessions/"+sessionID+"/messages", body, http.StatusNotFound)
		})
	}
}

func TestChatSubmissionDeleteWinsBeforeSendTransaction(t *testing.T) {
	for _, keyed := range []bool{false, true} {
		t.Run(fmt.Sprint(keyed), func(t *testing.T) {
			sessionID, _ := submissionSession(t)
			hub := realtime.NewHub()
			go hub.Run()
			router, h := NewRouterWithOptions(testPool, hub, events.New(), analytics.NoopClient{}, nil, RouterOptions{})
			server := httptest.NewServer(router)
			t.Cleanup(server.Close)
			barrier := &submissionBeginBarrier{testPool, make(chan struct{}), make(chan struct{})}
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(barrier.proceed) }) })
			h.TaskService.TxStarter = barrier
			body := map[string]any{"content": "send after deleted session"}
			wantStatus := http.StatusInternalServerError
			if keyed {
				body["submission_id"] = uuid.NewString()
				wantStatus = http.StatusNotFound
			}
			path := "/api/chat/sessions/" + sessionID
			send := submissionAsync(server.URL, testWorkspaceID, "POST", path+"/messages", body)
			select {
			case <-barrier.ready:
			case <-time.After(10 * time.Second):
				t.Fatal("send did not reach its pre-transaction barrier")
			}
			submissionStatus(t, server.URL, testToken, testWorkspaceID, "DELETE", path, nil, http.StatusNoContent)
			release.Do(func() { close(barrier.proceed) })
			submissionResult(t, send, wantStatus)
			submissionCounts(t, sessionID, 0, 0, 0)
		})
	}
}

type submissionMissingAgentPool struct{ pool *pgxpool.Pool }
type submissionMissingAgentTx struct{ pgx.Tx }

func (p submissionMissingAgentPool) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return submissionMissingAgentTx{tx}, nil
}

func (tx submissionMissingAgentTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	// Inject an absent agent read without deleting the real session with it.
	if strings.HasPrefix(sql, "-- name: GetAgentForClaimUpdate :one") {
		return tx.Tx.QueryRow(ctx, "SELECT 1 WHERE false")
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func TestChatSubmissionMissingAgentIsNotMissingSession(t *testing.T) {
	sessionID, _ := submissionSession(t)
	hub := realtime.NewHub()
	go hub.Run()
	router, h := NewRouterWithOptions(testPool, hub, events.New(), analytics.NoopClient{}, nil, RouterOptions{})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	h.TaskService.TxStarter = submissionMissingAgentPool{testPool}
	body := map[string]any{"content": "agent disappears after preflight", "submission_id": uuid.NewString()}
	submissionStatus(t, server.URL, testToken, testWorkspaceID, "POST", "/api/chat/sessions/"+sessionID+"/messages", body, http.StatusInternalServerError)
	if got := workspaceFixture(t).Count(t, `SELECT count(*) FROM chat_session WHERE id=$1`, sessionID); got != 1 {
		t.Fatalf("session count=%d, want 1", got)
	}
	submissionCounts(t, sessionID, 0, 0, 0)
}

type submissionOverlay struct{ calls atomic.Int32 }

func (o *submissionOverlay) BuildTaskOverlay(context.Context, pgtype.UUID, db.Agent) (runtimeapps.MCPOverlayResult, error) {
	o.calls.Add(1)
	return runtimeapps.MCPOverlayResult{}, errors.New("synthetic offline overlay failure")
}

type submissionFailingWakeup struct{ calls atomic.Int32 }

func (w *submissionFailingWakeup) NotifyTaskAvailable(string, string) {
	w.calls.Add(1)
	panic("synthetic post-commit wakeup failure")
}

func TestChatSubmissionRecoversPostCommitNotificationFailure(t *testing.T) {
	sessionID, _ := submissionSession(t)
	hub := realtime.NewHub()
	go hub.Run()
	router, h := NewRouterWithOptions(testPool, hub, events.New(), analytics.NoopClient{}, nil, RouterOptions{})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	overlay := &submissionOverlay{}
	wakeup := &submissionFailingWakeup{}
	flags := featureflag.NewStaticProvider()
	flags.Set(featureflags.ComposioMCPApps, featureflag.Rule{Default: true})
	h.TaskService.FeatureFlags = featureflag.NewService(flags)
	h.TaskService.Composio = overlay
	h.TaskService.Wakeup = wakeup
	body := map[string]any{"content": "committed before notification", "submission_id": uuid.NewString()}
	path := "/api/chat/sessions/" + sessionID + "/messages"
	status, _, err := submissionHTTP(server.URL, testToken, testWorkspaceID, "POST", path, body)
	if status != http.StatusInternalServerError {
		t.Fatalf("injected wakeup failure status=%d error=%v", status, err)
	}
	submissionCounts(t, sessionID, 1, 1, 1)
	var raw []byte
	workspaceFixture(t).QueryRow(t, `SELECT response FROM chat_submission_receipt WHERE chat_session_id=$1`, sessionID).Scan(&raw)
	var accepted map[string]any
	if err := json.Unmarshal(raw, &accepted); err != nil {
		t.Fatal(err)
	}
	replay := submissionStatus(t, server.URL, testToken, testWorkspaceID, "POST", path, body, http.StatusCreated)
	if !reflect.DeepEqual(accepted, replay) || overlay.calls.Load() != 1 || wakeup.calls.Load() != 1 {
		t.Fatalf("post-commit replay=%v accepted=%v overlay=%d wakeup=%d", replay, accepted, overlay.calls.Load(), wakeup.calls.Load())
	}
}

func TestChatSubmissionConcurrentPayloadConflict(t *testing.T) {
	sessionID, _ := submissionSession(t)
	id := uuid.NewString()
	start := make(chan struct{})
	results := make(chan submissionHTTPResult, 2)
	for _, content := range []string{"first payload", "second payload"} {
		go func(content string) {
			<-start
			status, body, err := submissionHTTP(testServer.URL, testToken, testWorkspaceID, "POST", "/api/chat/sessions/"+sessionID+"/messages", map[string]any{"content": content, "submission_id": id})
			results <- submissionHTTPResult{status, body, err}
		}(content)
	}
	close(start)
	statuses := map[int]int{}
	for i := 0; i < 2; i++ {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatal(result.err)
			}
			statuses[result.status]++
		case <-time.After(10 * time.Second):
			t.Fatal("parallel conflicting send did not complete")
		}
	}
	if statuses[http.StatusCreated] != 1 || statuses[http.StatusConflict] != 1 {
		t.Fatalf("parallel conflicting statuses=%v", statuses)
	}
	submissionCounts(t, sessionID, 1, 1, 1)
}
