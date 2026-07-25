package handler

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestLocalLoginDisabledIsNotFound(t *testing.T) {
	h := &Handler{cfg: Config{LocalMode: false}}
	recorder := httptest.NewRecorder()
	h.LocalLogin(recorder, httptest.NewRequest(http.MethodPost, "/auth/local", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("LocalLogin() status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestLocalSetupAndLoginBootstrapOneIdentity(t *testing.T) {
	if testPool == nil {
		t.Skip("database fixture is unavailable")
	}
	ctx := context.Background()
	cleanup := func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM lifeos_local_credential`)
		_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, localWorkspaceSlug)
		_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, localUserEmail)
	}
	cleanup()
	t.Cleanup(cleanup)

	t.Setenv("JWT_SECRET", "local-mode-test-secret")
	h := &Handler{
		Queries: db.New(testPool),
		DB:      testPool,
		cfg: Config{
			LocalMode:            true,
			LocalAutomationToken: "test-automation-token-12345678901234567890",
		},
	}

	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodPost,
			map[bool]string{true: "/auth/local/setup", false: "/auth/local"}[i == 0],
			strings.NewReader(`{"username":"xingyao","password":"Strong!LifeOS2026"}`),
		)
		request.Header.Set("Content-Type", "application/json")
		if i == 0 {
			h.LocalSetup(recorder, request)
		} else {
			h.LocalLogin(recorder, request)
		}
		if recorder.Code != http.StatusOK {
			t.Fatalf("local auth status = %d, want 200: %s", recorder.Code, recorder.Body.String())
		}

		var response LocalLoginResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode LocalLogin response: %v", err)
		}
		if response.User.Email != localUserEmail || response.User.OnboardedAt == nil {
			t.Fatalf("unexpected local user: %+v", response.User)
		}
		if response.Workspace.Slug != localWorkspaceSlug {
			t.Fatalf("workspace slug = %q, want %q", response.Workspace.Slug, localWorkspaceSlug)
		}

		cookies := recorder.Result().Cookies()
		seenAuth := false
		seenCSRF := false
		for _, cookie := range cookies {
			seenAuth = seenAuth || cookie.Name == auth.AuthCookieName
			seenCSRF = seenCSRF || cookie.Name == auth.CSRFCookieName
		}
		if !seenAuth || !seenCSRF {
			t.Fatalf("local login cookies: auth=%v csrf=%v", seenAuth, seenCSRF)
		}
	}

	var users, workspaces, memberships int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM "user" WHERE email = $1`, localUserEmail).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workspace WHERE slug = $1`, localWorkspaceSlug).Scan(&workspaces); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM member m
		JOIN "user" u ON u.id = m.user_id
		JOIN workspace w ON w.id = m.workspace_id
		WHERE u.email = $1 AND w.slug = $2
	`, localUserEmail, localWorkspaceSlug).Scan(&memberships); err != nil {
		t.Fatal(err)
	}
	if users != 1 || workspaces != 1 || memberships != 1 {
		t.Fatalf("local bootstrap counts: users=%d workspaces=%d memberships=%d", users, workspaces, memberships)
	}

	var userID, workspaceID, runtimeID, agentID pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT id FROM "user" WHERE email = $1`, localUserEmail).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM workspace WHERE slug = $1`, localWorkspaceSlug).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime(workspace_id, name, runtime_mode, provider, status, owner_id)
		VALUES ($1, 'local automation test runtime', 'local', 'codex', 'online', $2)
		RETURNING id
	`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent(workspace_id, name, runtime_mode, status, owner_id, runtime_id)
		VALUES ($1, 'AI 星耀', 'local', 'idle', $2, $3)
		RETURNING id
	`, workspaceID, userID, runtimeID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}

	automationRecorder := httptest.NewRecorder()
	automationRequest := httptest.NewRequest(
		http.MethodPost,
		"/auth/local/automation",
		strings.NewReader(`{"agent_name":"AI 星耀"}`),
	)
	automationRequest.Header.Set("X-LifeOS-Automation-Token", "test-automation-token-12345678901234567890")
	h.LocalAutomationLogin(automationRecorder, automationRequest)
	if automationRecorder.Code != http.StatusOK {
		t.Fatalf("local agent automation status = %d, want 200: %s", automationRecorder.Code, automationRecorder.Body.String())
	}
	var automationResponse LocalLoginResponse
	if err := json.Unmarshal(automationRecorder.Body.Bytes(), &automationResponse); err != nil {
		t.Fatalf("decode local agent automation response: %v", err)
	}
	if automationResponse.Actor == nil ||
		automationResponse.Actor.Type != "agent" ||
		automationResponse.Actor.Name != "AI 星耀" ||
		automationResponse.Actor.ID != util.UUIDToString(agentID) {
		t.Fatalf("unexpected local automation actor: %+v", automationResponse.Actor)
	}
	if len(automationRecorder.Result().Cookies()) != 0 {
		t.Fatal("machine automation login must not set browser cookies")
	}

	token, err := jwt.Parse(automationResponse.Token, func(token *jwt.Token) (any, error) {
		return auth.JWTSecret(), nil
	})
	if err != nil || !token.Valid {
		t.Fatalf("parse local automation token: %v", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok ||
		claims["actor_source"] != auth.LocalAgentActorSource ||
		claims["agent_id"] != util.UUIDToString(agentID) ||
		claims["actor_workspace"] != util.UUIDToString(workspaceID) {
		t.Fatalf("unexpected local automation claims: %+v", claims)
	}

	var gotActorType, gotActorID, gotActorSource, gotWorkspaceID string
	authenticated := middleware.Auth(db.New(testPool), nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotActorType, gotActorID = h.resolveActor(r, r.Header.Get("X-User-ID"), r.Header.Get("X-Workspace-ID"))
		gotActorSource = r.Header.Get("X-Actor-Source")
		gotWorkspaceID = r.Header.Get("X-Workspace-ID")
		w.WriteHeader(http.StatusOK)
	}))
	authenticatedRequest := httptest.NewRequest(http.MethodGet, "/api/issues", nil)
	authenticatedRequest.Header.Set("Authorization", "Bearer "+automationResponse.Token)
	authenticatedRequest.Header.Set("X-Workspace-ID", "00000000-0000-0000-0000-000000000000")
	authenticatedRecorder := httptest.NewRecorder()
	authenticated.ServeHTTP(authenticatedRecorder, authenticatedRequest)
	if authenticatedRecorder.Code != http.StatusOK {
		t.Fatalf("local automation auth status = %d: %s", authenticatedRecorder.Code, authenticatedRecorder.Body.String())
	}
	if gotActorType != "agent" || gotActorID != util.UUIDToString(agentID) ||
		gotActorSource != auth.LocalAgentActorSource ||
		gotWorkspaceID != util.UUIDToString(workspaceID) {
		t.Fatalf(
			"local automation actor headers: type=%q id=%q source=%q workspace=%q",
			gotActorType, gotActorID, gotActorSource, gotWorkspaceID,
		)
	}
}

func TestLocalCredentialValidation(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		valid    bool
	}{
		{name: "strong", username: "xingyao", password: "Strong!LifeOS2026", valid: true},
		{name: "short", username: "xingyao", password: "Aa1!short", valid: false},
		{name: "weak classes", username: "xingyao", password: "alllowercasepassword", valid: false},
		{name: "contains username", username: "xingyao", password: "Xingyao!2026Strong", valid: false},
		{name: "invalid username", username: "星耀", password: "Strong!LifeOS2026", valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, message := validateLocalCredential(tt.username, tt.password)
			if (message == "") != tt.valid {
				t.Fatalf("validateLocalCredential() message = %q, valid = %v", message, tt.valid)
			}
		})
	}
}

func TestDeriveLocalPasswordIsSaltedAndDeterministic(t *testing.T) {
	one := deriveLocalPassword("Strong!LifeOS2026", []byte("01234567890123456789012345678901"), 1000)
	two := deriveLocalPassword("Strong!LifeOS2026", []byte("01234567890123456789012345678901"), 1000)
	other := deriveLocalPassword("Strong!LifeOS2026", []byte("11234567890123456789012345678901"), 1000)
	if subtle.ConstantTimeCompare(one, two) != 1 {
		t.Fatal("same credential must derive the same digest")
	}
	if subtle.ConstantTimeCompare(one, other) == 1 {
		t.Fatal("different salts must derive different digests")
	}
}
