package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	dingtalkintegration "github.com/multica-ai/multica/server/internal/integrations/dingtalk"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type dingtalkGroupRouteHandlerFixture struct {
	installationID string
	routeID        string
	defaultAgentID string
	targetAgentID  string
}

func newDingTalkGroupRouteHandlerFixture(t *testing.T) dingtalkGroupRouteHandlerFixture {
	t.Helper()
	var migrated bool
	if err := testPool.QueryRow(context.Background(), `SELECT to_regclass('public.dingtalk_group_route') IS NOT NULL`).Scan(&migrated); err != nil || !migrated {
		t.Skip("dingtalk group route table not present (database not migrated)")
	}

	previousInstall := testHandler.DingTalkInstall
	testHandler.DingTalkInstall = &dingtalkintegration.InstallService{}
	t.Cleanup(func() { testHandler.DingTalkInstall = previousInstall })

	defaultAgentID := createHandlerTestAgent(t, "DingTalk Route Default Agent", nil)
	targetAgentID := createHandlerTestAgent(t, "DingTalk Route Target Agent", nil)
	var installationID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO channel_installation (
			workspace_id, agent_id, channel_type, config, installer_user_id
		) VALUES ($1, $2, 'dingtalk', jsonb_build_object('app_id', $3::text), $4)
		RETURNING id
	`, testWorkspaceID, defaultAgentID, "handler-dingtalk-group-routes", testUserID).Scan(&installationID); err != nil {
		t.Fatalf("create DingTalk route installation: %v", err)
	}
	var routeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO dingtalk_group_route (
			workspace_id, installation_id, conversation_id, conversation_title, agent_id
		) VALUES ($1, $2, 'cid-handler-routes', 'Handler routes', $3)
		RETURNING id
	`, testWorkspaceID, installationID, defaultAgentID).Scan(&routeID); err != nil {
		t.Fatalf("create DingTalk group route: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_chat_session_binding WHERE installation_id = $1`, installationID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM dingtalk_group_route WHERE installation_id = $1`, installationID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, installationID)
	})
	return dingtalkGroupRouteHandlerFixture{
		installationID: installationID,
		routeID:        routeID,
		defaultAgentID: defaultAgentID,
		targetAgentID:  targetAgentID,
	}
}

func requestWithRouteParams(req *http.Request, workspaceID, routeID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", workspaceID)
	if routeID != "" {
		rctx.URLParams.Add("routeId", routeID)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func patchDingTalkGroupRoute(t *testing.T, routeID, agentID string) *httptest.ResponseRecorder {
	t.Helper()
	req := requestWithRouteParams(
		newRequest(
			http.MethodPatch,
			"/api/workspaces/"+testWorkspaceID+"/dingtalk/group-routes/"+routeID,
			UpdateDingTalkGroupRouteRequest{AgentID: agentID},
		),
		testWorkspaceID,
		routeID,
	)
	rec := httptest.NewRecorder()
	testHandler.UpdateDingTalkGroupRoute(rec, req)
	return rec
}

func seedDingTalkGroupRouteBinding(t *testing.T, fx dingtalkGroupRouteHandlerFixture) (string, int64) {
	t.Helper()
	sessionID := createHandlerTestChatSession(t, fx.defaultAgentID)
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO channel_chat_session_binding (
			chat_session_id, installation_id, channel_type,
			channel_chat_id, chat_type, config
		) VALUES ($1, $2, 'dingtalk', 'cid-handler-routes', 'group',
		          jsonb_build_object('agent_id', $3::text))
	`, sessionID, fx.installationID, fx.defaultAgentID); err != nil {
		t.Fatalf("seed DingTalk route chat binding: %v", err)
	}
	var revision int64
	if err := testPool.QueryRow(context.Background(), `
		SELECT revision FROM dingtalk_group_route WHERE id = $1
	`, fx.routeID).Scan(&revision); err != nil {
		t.Fatalf("read initial DingTalk route revision: %v", err)
	}
	return sessionID, revision
}

func assertDingTalkGroupRouteBindingUnchanged(
	t *testing.T,
	fx dingtalkGroupRouteHandlerFixture,
	sessionID string,
	revision int64,
) {
	t.Helper()
	var agentID string
	var durableRevision int64
	if err := testPool.QueryRow(context.Background(), `
		SELECT agent_id, revision FROM dingtalk_group_route WHERE id = $1
	`, fx.routeID).Scan(&agentID, &durableRevision); err != nil {
		t.Fatalf("read durable DingTalk route: %v", err)
	}
	if agentID != fx.defaultAgentID || durableRevision != revision {
		t.Fatalf(
			"durable route = agent %s revision %d, want agent %s revision %d",
			agentID,
			durableRevision,
			fx.defaultAgentID,
			revision,
		)
	}
	var durableSessionID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT chat_session_id
		FROM channel_chat_session_binding
		WHERE installation_id = $1
		  AND channel_type = 'dingtalk'
		  AND channel_chat_id = 'cid-handler-routes'
	`, fx.installationID).Scan(&durableSessionID); err != nil {
		t.Fatalf("read durable DingTalk route binding: %v", err)
	}
	if durableSessionID != sessionID {
		t.Fatalf("durable route binding = %s, want %s", durableSessionID, sessionID)
	}
}

func assertDingTalkRouteNotFoundWithoutMetadata(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PATCH revoked route status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	for _, secret := range []string{"Handler routes", "cid-handler-routes"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("PATCH revoked route leaked %q: %s", secret, rec.Body.String())
		}
	}
}

func TestDingTalkGroupRoutePatchRejectsDisabledDeployment(t *testing.T) {
	req := requestWithRouteParams(
		httptest.NewRequest(http.MethodPatch, "/api/workspaces/disabled/dingtalk/group-routes/disabled", nil),
		"disabled",
		"disabled",
	)
	rec := httptest.NewRecorder()
	(&Handler{}).UpdateDingTalkGroupRoute(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled PATCH status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
}

func TestDingTalkGroupRoutePatchRejectsRevokedInstallation(t *testing.T) {
	fx := newDingTalkGroupRouteHandlerFixture(t)
	sessionID, revision := seedDingTalkGroupRouteBinding(t, fx)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE channel_installation SET status = 'revoked' WHERE id = $1
	`, fx.installationID); err != nil {
		t.Fatalf("revoke DingTalk installation before PATCH: %v", err)
	}

	rec := patchDingTalkGroupRoute(t, fx.routeID, fx.targetAgentID)
	assertDingTalkRouteNotFoundWithoutMetadata(t, rec)
	assertDingTalkGroupRouteBindingUnchanged(t, fx, sessionID, revision)
}

func TestDingTalkGroupRoutePatchSerializesWithRevoke(t *testing.T) {
	fx := newDingTalkGroupRouteHandlerFixture(t)
	sessionID, revision := seedDingTalkGroupRouteBinding(t, fx)
	ctx := context.Background()

	revokeTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin controlled DingTalk revoke: %v", err)
	}
	t.Cleanup(func() { _ = revokeTx.Rollback(context.Background()) })
	var revokePID int32
	if err := revokeTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&revokePID); err != nil {
		t.Fatalf("read controlled revoke backend pid: %v", err)
	}
	if _, err := revokeTx.Exec(ctx, `
		UPDATE channel_installation SET status = 'revoked' WHERE id = $1
	`, fx.installationID); err != nil {
		t.Fatalf("lock installation for controlled revoke: %v", err)
	}

	patchDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		patchDone <- patchDingTalkGroupRoute(t, fx.routeID, fx.targetAgentID)
	}()

	deadline := time.Now().Add(5 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		if err := testPool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity a
				WHERE $1 = ANY(pg_blocking_pids(a.pid))
			)
		`, revokePID).Scan(&blocked); err != nil {
			_ = revokeTx.Rollback(ctx)
			t.Fatalf("inspect controlled revoke lock wait: %v", err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		_ = revokeTx.Rollback(ctx)
		rec := <-patchDone
		t.Fatalf("PATCH never waited for revoke; status = %d: %s", rec.Code, rec.Body.String())
	}
	if err := revokeTx.Commit(ctx); err != nil {
		t.Fatalf("commit controlled DingTalk revoke: %v", err)
	}

	select {
	case rec := <-patchDone:
		assertDingTalkRouteNotFoundWithoutMetadata(t, rec)
	case <-time.After(5 * time.Second):
		t.Fatal("PATCH did not resume after controlled revoke committed")
	}
	assertDingTalkGroupRouteBindingUnchanged(t, fx, sessionID, revision)
}

func TestDingTalkGroupRoutePatchDiagnosesConcurrentAgentArchive(t *testing.T) {
	fx := newDingTalkGroupRouteHandlerFixture(t)
	sessionID, revision := seedDingTalkGroupRouteBinding(t, fx)
	ctx := context.Background()

	archiveTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin controlled Agent archive: %v", err)
	}
	t.Cleanup(func() { _ = archiveTx.Rollback(context.Background()) })
	var archivePID int32
	if err := archiveTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&archivePID); err != nil {
		t.Fatalf("read controlled Agent archive backend pid: %v", err)
	}
	if _, err := archiveTx.Exec(ctx, `
		UPDATE agent SET archived_at = now(), archived_by = $2 WHERE id = $1
	`, fx.targetAgentID, testUserID); err != nil {
		t.Fatalf("lock target Agent for controlled archive: %v", err)
	}

	patchDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		patchDone <- patchDingTalkGroupRoute(t, fx.routeID, fx.targetAgentID)
	}()

	deadline := time.Now().Add(5 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		if err := testPool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity a
				WHERE $1 = ANY(pg_blocking_pids(a.pid))
			)
		`, archivePID).Scan(&blocked); err != nil {
			_ = archiveTx.Rollback(ctx)
			t.Fatalf("inspect controlled Agent archive lock wait: %v", err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		_ = archiveTx.Rollback(ctx)
		rec := <-patchDone
		t.Fatalf("PATCH never waited for Agent archive; status = %d: %s", rec.Code, rec.Body.String())
	}
	if err := archiveTx.Commit(ctx); err != nil {
		t.Fatalf("commit controlled Agent archive: %v", err)
	}

	select {
	case rec := <-patchDone:
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "archived") {
			t.Fatalf("PATCH after Agent archive status = %d, want archived 409: %s", rec.Code, rec.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PATCH did not resume after controlled Agent archive committed")
	}
	assertDingTalkGroupRouteBindingUnchanged(t, fx, sessionID, revision)
}

func TestListDingTalkInstallationsAdvertisesGroupRoutingCapability(t *testing.T) {
	t.Run("disabled deployment", func(t *testing.T) {
		rec := httptest.NewRecorder()
		(&Handler{}).ListDingTalkInstallations(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("disabled installations status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			GroupRoutingSupported bool `json:"group_routing_supported"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode disabled installations response: %v", err)
		}
		if body.GroupRoutingSupported {
			t.Fatal("disabled deployment advertised group routing")
		}
	})

	t.Run("configured deployment", func(t *testing.T) {
		_ = newDingTalkGroupRouteHandlerFixture(t)
		box, err := secretbox.New(make([]byte, secretbox.KeySize))
		if err != nil {
			t.Fatalf("create DingTalk test secret box: %v", err)
		}
		service, err := dingtalkintegration.NewInstallService(db.New(testPool), testPool, box, nil)
		if err != nil {
			t.Fatalf("create DingTalk install service: %v", err)
		}
		testHandler.DingTalkInstall = service

		req := requestWithRouteParams(
			httptest.NewRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/dingtalk/installations", nil),
			testWorkspaceID,
			"",
		)
		rec := httptest.NewRecorder()
		testHandler.ListDingTalkInstallations(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("configured installations status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			GroupRoutingSupported bool `json:"group_routing_supported"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode configured installations response: %v", err)
		}
		if !body.GroupRoutingSupported {
			t.Fatal("configured deployment did not advertise group routing")
		}
	})
}

func TestDingTalkGroupRouteHandlersValidationAndErrors(t *testing.T) {
	fx := newDingTalkGroupRouteHandlerFixture(t)

	t.Run("GET returns workspace routes", func(t *testing.T) {
		req := requestWithRouteParams(newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/dingtalk/group-routes", nil), testWorkspaceID, "")
		rec := httptest.NewRecorder()
		testHandler.ListDingTalkGroupRoutes(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET routes status = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Routes []DingTalkGroupRouteResponse `json:"routes"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Routes) != 1 || body.Routes[0].ID != fx.routeID || body.Routes[0].AgentID != fx.defaultAgentID {
			t.Fatalf("GET routes body = %+v", body)
		}
	})

	t.Run("GET validates workspace id", func(t *testing.T) {
		req := requestWithRouteParams(newRequest(http.MethodGet, "/api/workspaces/not-a-uuid/dingtalk/group-routes", nil), "not-a-uuid", "")
		rec := httptest.NewRecorder()
		testHandler.ListDingTalkGroupRoutes(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid workspace status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET reports query errors", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/dingtalk/group-routes", nil).WithContext(ctx)
		req = requestWithRouteParams(req, testWorkspaceID, "")
		rec := httptest.NewRecorder()
		testHandler.ListDingTalkGroupRoutes(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("cancelled GET status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	patch := func(t *testing.T, workspaceID, routeID string, body any) *httptest.ResponseRecorder {
		t.Helper()
		req := requestWithRouteParams(newRequest(http.MethodPatch, "/api/workspaces/"+workspaceID+"/dingtalk/group-routes/"+routeID, body), workspaceID, routeID)
		rec := httptest.NewRecorder()
		testHandler.UpdateDingTalkGroupRoute(rec, req)
		return rec
	}

	t.Run("PATCH requires a user", func(t *testing.T) {
		raw, _ := json.Marshal(UpdateDingTalkGroupRouteRequest{AgentID: fx.targetAgentID})
		req := requestWithRouteParams(httptest.NewRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID+"/dingtalk/group-routes/"+fx.routeID, bytes.NewReader(raw)), testWorkspaceID, fx.routeID)
		rec := httptest.NewRecorder()
		testHandler.UpdateDingTalkGroupRoute(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous PATCH status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	for _, tc := range []struct {
		name        string
		workspaceID string
		routeID     string
		body        any
		want        int
	}{
		{name: "invalid workspace id", workspaceID: "bad-workspace", routeID: fx.routeID, body: UpdateDingTalkGroupRouteRequest{AgentID: fx.targetAgentID}, want: http.StatusBadRequest},
		{name: "invalid route id", workspaceID: testWorkspaceID, routeID: "bad-route", body: UpdateDingTalkGroupRouteRequest{AgentID: fx.targetAgentID}, want: http.StatusBadRequest},
		{name: "invalid agent id", workspaceID: testWorkspaceID, routeID: fx.routeID, body: UpdateDingTalkGroupRouteRequest{AgentID: "bad-agent"}, want: http.StatusBadRequest},
		{name: "route not found", workspaceID: testWorkspaceID, routeID: "d2770000-0000-4000-8000-000000000002", body: UpdateDingTalkGroupRouteRequest{AgentID: fx.targetAgentID}, want: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := patch(t, tc.workspaceID, tc.routeID, tc.body)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	t.Run("PATCH rejects existing agent outside workspace", func(t *testing.T) {
		ctx := context.Background()
		var foreignWorkspaceID string
		suffix := fmt.Sprintf("%d", time.Now().UnixNano())
		if err := testPool.QueryRow(ctx, `
			INSERT INTO workspace (name, slug, description, issue_prefix)
			VALUES ($1, $2, 'DingTalk cross-workspace target test', 'DTX')
			RETURNING id
		`, "DingTalk Foreign Workspace "+suffix, "dingtalk-foreign-"+suffix).Scan(&foreignWorkspaceID); err != nil {
			t.Fatalf("create foreign workspace: %v", err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
		})
		var foreignAgentID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent (
				workspace_id, name, description, runtime_mode, runtime_config,
				visibility, max_concurrent_tasks, owner_id
			) VALUES ($1, $2, '', 'cloud', '{}'::jsonb, 'workspace', 1, $3)
			RETURNING id
		`, foreignWorkspaceID, "DingTalk Foreign Agent "+suffix, testUserID).Scan(&foreignAgentID); err != nil {
			t.Fatalf("create foreign Agent: %v", err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, foreignAgentID)
		})

		rec := patch(t, testWorkspaceID, fx.routeID, UpdateDingTalkGroupRouteRequest{AgentID: foreignAgentID})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("cross-workspace target status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
		var durableAgentID string
		if err := testPool.QueryRow(ctx, `SELECT agent_id FROM dingtalk_group_route WHERE id = $1`, fx.routeID).Scan(&durableAgentID); err != nil {
			t.Fatal(err)
		}
		if durableAgentID != fx.defaultAgentID {
			t.Fatalf("cross-workspace PATCH changed route to %s, want %s", durableAgentID, fx.defaultAgentID)
		}
	})

	t.Run("PATCH rejects malformed JSON", func(t *testing.T) {
		req := requestWithRouteParams(newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID+"/dingtalk/group-routes/"+fx.routeID, nil), testWorkspaceID, fx.routeID)
		req.Body = http.NoBody
		rec := httptest.NewRecorder()
		testHandler.UpdateDingTalkGroupRoute(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("empty PATCH status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("PATCH rejects archived target", func(t *testing.T) {
		if _, err := testPool.Exec(context.Background(), `UPDATE agent SET archived_at = now() WHERE id = $1`, fx.targetAgentID); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `UPDATE agent SET archived_at = NULL, archived_by = NULL WHERE id = $1`, fx.targetAgentID)
		})
		rec := patch(t, testWorkspaceID, fx.routeID, UpdateDingTalkGroupRouteRequest{AgentID: fx.targetAgentID})
		if rec.Code != http.StatusConflict {
			t.Fatalf("archived target status = %d: %s", rec.Code, rec.Body.String())
		}
		if _, err := testPool.Exec(context.Background(), `UPDATE agent SET archived_at = NULL, archived_by = NULL WHERE id = $1`, fx.targetAgentID); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("PATCH rejects non-user target", func(t *testing.T) {
		if _, err := testPool.Exec(context.Background(), `
			UPDATE agent
			SET kind = 'system', system_key = $2
			WHERE id = $1
		`, fx.targetAgentID, "dingtalk_route_non_user_"+fx.targetAgentID); err != nil {
			t.Fatal(err)
		}
		defer func() {
			_, _ = testPool.Exec(context.Background(), `
				UPDATE agent SET kind = 'user', system_key = NULL WHERE id = $1
			`, fx.targetAgentID)
		}()

		rec := patch(t, testWorkspaceID, fx.routeID, UpdateDingTalkGroupRouteRequest{AgentID: fx.targetAgentID})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "only user agents") {
			t.Fatalf("non-user target status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
		var durableAgentID string
		if err := testPool.QueryRow(context.Background(), `SELECT agent_id FROM dingtalk_group_route WHERE id = $1`, fx.routeID).Scan(&durableAgentID); err != nil {
			t.Fatal(err)
		}
		if durableAgentID != fx.defaultAgentID {
			t.Fatalf("non-user PATCH changed route to %s, want %s", durableAgentID, fx.defaultAgentID)
		}
	})

	t.Run("PATCH reports query errors", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID+"/dingtalk/group-routes/"+fx.routeID, UpdateDingTalkGroupRouteRequest{AgentID: fx.targetAgentID}).WithContext(ctx)
		req = requestWithRouteParams(req, testWorkspaceID, fx.routeID)
		rec := httptest.NewRecorder()
		testHandler.UpdateDingTalkGroupRoute(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("cancelled PATCH status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("PATCH reassigns route", func(t *testing.T) {
		rec := patch(t, testWorkspaceID, fx.routeID, UpdateDingTalkGroupRouteRequest{AgentID: fx.targetAgentID})
		if rec.Code != http.StatusOK {
			t.Fatalf("successful PATCH status = %d: %s", rec.Code, rec.Body.String())
		}
		var body DingTalkGroupRouteResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.ID != fx.routeID || body.AgentID != fx.targetAgentID {
			t.Fatalf("successful PATCH body = %+v", body)
		}
	})
}

func TestDingTalkGroupRouteAuthorizationWiring(t *testing.T) {
	fx := newDingTalkGroupRouteHandlerFixture(t)
	ctx := context.Background()

	createUser := func(label string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, "DingTalk "+label, fmt.Sprintf("dingtalk-route-%s@multica.test", label)).Scan(&id); err != nil {
			t.Fatalf("create %s user: %v", label, err)
		}
		return id
	}
	memberID := createUser("member")
	outsiderID := createUser("outsider")
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, memberID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, memberID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id IN ($1, $2)`, memberID, outsiderID)
	})

	router := chi.NewRouter()
	router.Route("/api/workspaces/{id}", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMemberFromURL(testHandler.Queries, "id"))
			r.Get("/dingtalk/group-routes", testHandler.ListDingTalkGroupRoutes)
		})
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceRoleFromURL(testHandler.Queries, "id", "owner", "admin"))
			r.Patch("/dingtalk/group-routes/{routeId}", testHandler.UpdateDingTalkGroupRoute)
		})
	})

	exercise := func(method, path, userID string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		if userID != "" {
			req.Header.Set("X-User-ID", userID)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	base := "/api/workspaces/" + testWorkspaceID + "/dingtalk/group-routes"
	if rec := exercise(http.MethodGet, base, memberID, nil); rec.Code != http.StatusOK {
		t.Errorf("member GET status = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := testPool.Exec(ctx, `UPDATE channel_installation SET status = 'revoked' WHERE id = $1`, fx.installationID); err != nil {
		t.Fatalf("revoke DingTalk installation before member GET: %v", err)
	}
	revokedGET := exercise(http.MethodGet, base, memberID, nil)
	if revokedGET.Code != http.StatusOK {
		t.Fatalf("member GET after revoke status = %d: %s", revokedGET.Code, revokedGET.Body.String())
	}
	var revokedBody struct {
		Routes []DingTalkGroupRouteResponse `json:"routes"`
	}
	if err := json.Unmarshal(revokedGET.Body.Bytes(), &revokedBody); err != nil {
		t.Fatalf("decode member GET after revoke: %v", err)
	}
	if len(revokedBody.Routes) != 0 || strings.Contains(revokedGET.Body.String(), "Handler routes") || strings.Contains(revokedGET.Body.String(), "cid-handler-routes") {
		t.Fatalf("member GET leaked revoked route metadata: %s", revokedGET.Body.String())
	}
	if _, err := testPool.Exec(ctx, `UPDATE channel_installation SET status = 'active' WHERE id = $1`, fx.installationID); err != nil {
		t.Fatalf("restore DingTalk installation after member GET regression: %v", err)
	}
	if rec := exercise(http.MethodGet, base, outsiderID, nil); rec.Code != http.StatusNotFound {
		t.Errorf("outsider GET status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := exercise(http.MethodGet, base, "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous GET status = %d: %s", rec.Code, rec.Body.String())
	}
	patchBody := UpdateDingTalkGroupRouteRequest{AgentID: fx.targetAgentID}
	if rec := exercise(http.MethodPatch, base+"/"+fx.routeID, memberID, patchBody); rec.Code != http.StatusForbidden {
		t.Errorf("member PATCH status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := exercise(http.MethodPatch, base+"/"+fx.routeID, testUserID, patchBody); rec.Code != http.StatusOK {
		t.Errorf("owner PATCH status = %d: %s", rec.Code, rec.Body.String())
	}
}
