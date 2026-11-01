package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestEnrollmentProjectionDoesNotExposeCredentialHash(t *testing.T) {
	raw, err := json.Marshal(enrollmentProjection(db.DaemonEnrollment{
		DaemonID: "host-one", Name: "Linux worker", TokenHash: "must-stay-private", Generation: 2,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "must-stay-private") || strings.Contains(string(raw), "token_hash") {
		t.Fatal("administrator projections must not expose credential hashes")
	}
	if !strings.Contains(string(raw), "host-one") || !strings.Contains(string(raw), "generation") {
		t.Fatal("administrator projections must expose host identity and credential version")
	}
}

func TestDaemonEnrollmentLifecycle(t *testing.T) {
	ctx := context.Background()
	host := "enrollment-lifecycle"
	create := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/daemon-enrollments", map[string]any{"daemon_id": host, "name": "Dedicated host"}), "id", testWorkspaceID)
	var enrollment struct {
		ID         string `json:"id"`
		Token      string `json:"token"`
		Generation int64  `json:"generation"`
	}
	testutil.Call(t, testHandler.CreateDaemonEnrollment, create).Want(http.StatusCreated).JSON(&enrollment)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM daemon_enrollment WHERE id=$1`, enrollment.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM daemon_token WHERE workspace_id=$1 AND daemon_id=$2`, testWorkspaceID, host)
	})
	if !strings.HasPrefix(enrollment.Token, "mde_") || enrollment.Generation != 1 {
		t.Fatalf("invalid enrollment: %+v", enrollment)
	}
	runtimeID := dbfx.Runtime(t, "Enrolled runtime", testutil.Cols{"daemon_id": host, "runtime_mode": "local", "provider": "multica_daemon"})
	agentID := dbfx.Agent(t, "Enrolled agent", runtimeID, testutil.Cols{"runtime_mode": "local"})
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID})
	_, err := testHandler.Queries.CreateDaemonToken(ctx, db.CreateDaemonTokenParams{TokenHash: auth.HashToken("mds_derived"), WorkspaceID: parseUUID(testWorkspaceID), DaemonID: host, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, EnrollmentTokenHash: pgtype.Text{String: auth.HashToken(enrollment.Token), Valid: true}, OwnerID: parseUUID(testUserID)})
	if err != nil {
		t.Fatal(err)
	}
	otherOwner := dbfx.User(t, "Unrelated enrollment owner", "unrelated-enrollment-owner@example.com")
	otherRuntime := dbfx.Runtime(t, "Other owner on same host", testutil.Cols{"daemon_id": host, "owner_id": otherOwner, "status": "online"})
	otherAgent := dbfx.Agent(t, "Unrelated host agent", otherRuntime)
	otherTask := dbfx.Task(t, otherAgent, testutil.Cols{"runtime_id": otherRuntime, "status": "running"})
	_, err = testHandler.Queries.CreateDaemonToken(ctx, db.CreateDaemonTokenParams{TokenHash: auth.HashToken("mdt_unrelated-owner"), WorkspaceID: parseUUID(testWorkspaceID), DaemonID: host, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, OwnerID: parseUUID(otherOwner)})
	if err != nil {
		t.Fatal(err)
	}
	old := enrollment.Token
	rotate := enrollmentLifecycleRequest(http.MethodPost, enrollment.ID)
	testutil.Call(t, testHandler.RotateDaemonEnrollment, rotate).Want(http.StatusOK).JSON(&enrollment)
	if old == enrollment.Token || enrollment.Generation != 2 {
		t.Fatal("rotation must replace and version credential")
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_runtime WHERE id=$1`, runtimeID).Scan(&status)
	if status != "offline" {
		t.Fatalf("runtime status %s", status)
	}
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("task status %s", status)
	}
	dbfx.QueryRow(t, `SELECT status FROM agent_runtime WHERE id=$1`, otherRuntime).Scan(&status)
	if status != "online" {
		t.Fatalf("rotation changed another owner's runtime: %s", status)
	}
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, otherTask).Scan(&status)
	if status != "running" {
		t.Fatalf("rotation cancelled another owner's task: %s", status)
	}
	if _, err := testHandler.Queries.GetDaemonTokenByHash(ctx, auth.HashToken("mdt_unrelated-owner")); err != nil {
		t.Fatalf("rotation revoked another owner's daemon token: %v", err)
	}
	check := func(token string, want int) {
		req := newRequest(http.MethodGet, "/api/daemon/workspaces", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		next := middleware.DaemonAuth(testHandler.Queries, nil, nil, nil, testPool)(http.HandlerFunc(testHandler.ListDaemonWorkspaces))
		testutil.Call(t, next.ServeHTTP, req).Want(want)
	}
	check(old, http.StatusUnauthorized)
	check("mds_derived", http.StatusUnauthorized)
	check(enrollment.Token, http.StatusOK)
	for _, tc := range []struct {
		body map[string]any
		want int
	}{
		{map[string]any{"workspace_id": testWorkspaceID, "daemon_id": "another-host", "runtimes": []map[string]any{{"provider": "multica_daemon", "name": "Other host"}}}, http.StatusForbidden},
		{map[string]any{"workspace_id": "11111111-1111-1111-1111-111111111111", "daemon_id": host, "runtimes": []map[string]any{{"provider": "multica_daemon", "name": "Other workspace"}}}, http.StatusNotFound},
	} {
		req := newRequest(http.MethodPost, "/api/daemon/register", tc.body)
		req.Header.Set("Authorization", "Bearer "+enrollment.Token)
		req.Header.Set("X-User-ID", testUserID)
		next := middleware.DaemonAuth(testHandler.Queries, nil, nil, nil, testPool)(http.HandlerFunc(testHandler.DaemonRegister))
		testutil.Call(t, next.ServeHTTP, req).Want(tc.want)
	}
	revoke := enrollmentLifecycleRequest(http.MethodDelete, enrollment.ID)
	testutil.Call(t, testHandler.RevokeDaemonEnrollment, revoke).Want(http.StatusOK)
	check(enrollment.Token, http.StatusUnauthorized)
}

func TestDaemonEnrollmentAuthorizationFencesRotation(t *testing.T) {
	ctx := context.Background()
	e, err := testHandler.Queries.CreateDaemonEnrollment(ctx, db.CreateDaemonEnrollmentParams{WorkspaceID: parseUUID(testWorkspaceID), DaemonID: "enrollment-lock-fence", Name: "Fence", CreatorID: parseUUID(testUserID), TokenHash: auth.HashToken("mde_fence"), ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM daemon_enrollment WHERE id=$1`, uuidToString(e.ID))
	})
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := testHandler.Queries.WithTx(tx).LockLiveDaemonEnrollment(ctx, e.TokenHash); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, err = testHandler.Queries.RotateDaemonEnrollment(bounded, db.RotateDaemonEnrollmentParams{ID: e.ID, WorkspaceID: e.WorkspaceID, TokenHash: auth.HashToken("mde_replaced"), ExpiresAt: e.ExpiresAt})
	if err == nil {
		t.Fatal("rotation must wait for the authorization fence")
	}
	if bounded.Err() != context.DeadlineExceeded {
		t.Fatalf("unexpected rotation failure: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := testHandler.Queries.RotateDaemonEnrollment(ctx, db.RotateDaemonEnrollmentParams{ID: e.ID, WorkspaceID: e.WorkspaceID, TokenHash: auth.HashToken("mde_replaced"), ExpiresAt: e.ExpiresAt}); err != nil {
		t.Fatal(err)
	}
}

func enrollmentLifecycleRequest(method, id string) *http.Request {
	req := newRequest(method, "/enrollment", nil)
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("id", testWorkspaceID)
	ctx.URLParams.Add("enrollmentId", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
}
