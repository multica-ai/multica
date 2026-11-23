package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func TestPlatformSettingsOverrideAndReset(t *testing.T) {
	ctx := t.Context()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	q := testHandler.Queries.WithTx(tx)
	h := &Handler{
		Queries: q,
		cfg: Config{
			DisableWorkspaceCreation:       true,
			WorkspaceCreationAllowedEmails: []string{"approved@example.test"},
		},
		FeatureFlags: WithPlatformFeatureFlags(q, nil),
	}
	router := chi.NewRouter()
	router.Patch("/{key}", h.UpdatePlatformSetting)
	update := func(key, value string, expected int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPatch, "/"+key, strings.NewReader(value))
		testutil.Call(t, router.ServeHTTP, req).Want(expected)
	}
	update(settingAllowWorkspaceCreation, `{"value":true}`, http.StatusNoContent)
	update(settingWorkspaceCreatorEmails, `{"value":[]}`, http.StatusNoContent)
	update(featureflags.PluginsV1, `{"value":true}`, http.StatusNoContent)
	update(settingAllowWorkspaceCreation, `{"value":"true"}`, http.StatusBadRequest)
	update("jwt_secret", `{"value":true}`, http.StatusNotFound)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	var public AppConfig
	testutil.Call(t, h.GetConfig, req).Want(http.StatusOK).JSON(&public)
	if public.WorkspaceCreationDisabled || !public.FeatureFlags[featureflags.PluginsV1] {
		t.Fatalf("public config did not reflect overrides: %+v", public)
	}
	settings, err := h.loadPlatformSettings(ctx)
	if err != nil || !h.canCreateWorkspace(settings, "another@example.test") {
		t.Fatalf("explicit empty allowlist did not override deployment: %v", err)
	}

	update(settingAllowWorkspaceCreation, `{"value":null}`, http.StatusNoContent)
	update(settingWorkspaceCreatorEmails, `{"value":null}`, http.StatusNoContent)
	update(featureflags.PluginsV1, `{"value":null}`, http.StatusNoContent)
	testutil.Call(t, h.GetConfig, req).Want(http.StatusOK).JSON(&public)
	if !public.WorkspaceCreationDisabled || public.FeatureFlags[featureflags.PluginsV1] {
		t.Fatalf("public config did not revert to deployment settings: %+v", public)
	}
	for _, key := range []string{settingAllowWorkspaceCreation, settingWorkspaceCreatorEmails, featureflags.PluginsV1} {
		raw, err := q.GetPlatformSetting(ctx, key)
		if err != nil || string(raw) != "null" {
			t.Fatalf("reset left an override for %s: %s, %v", key, raw, err)
		}
	}
}

func TestPlatformAdminGuardRequiresAllowlistedSession(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "http://localhost:3000")
	h := &Handler{Queries: testHandler.Queries}
	endpoint := h.RequirePlatformAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil)
	req.Header.Set("X-User-ID", testUserID)
	// The fixture already owns a workspace. That membership grants no
	// deployment-level administration privileges.
	testutil.Call(t, endpoint.ServeHTTP, req).Want(http.StatusForbidden)
	h.cfg.PlatformAdminEmails = []string{strings.ToUpper(handlerTestEmail)}
	testutil.Call(t, endpoint.ServeHTTP, req).Want(http.StatusNoContent)
	req.Header.Set("X-Actor-Source", "task_token")
	testutil.Call(t, endpoint.ServeHTTP, req).Want(http.StatusForbidden)
	req.Header.Set("X-Actor-Source", "cloud_pat")
	testutil.Call(t, endpoint.ServeHTTP, req).Want(http.StatusForbidden)
}

func TestPlatformFlagOverridePrecedesEnvironment(t *testing.T) {
	t.Setenv("FF_DOCS_V1", "true")
	provider := featureflag.NewChainProvider(platformFlagProvider{}, featureflag.NewEnvProvider("FF_"))
	flags := featureflag.NewService(provider)
	ctx := t.Context()
	if !flags.IsEnabled(context.WithValue(ctx, platformSettingsContextKey{}, platformSettings{}), featureflags.DocsV1, false) {
		t.Fatal("environment default was lost")
	}
	override := platformSettings{featureflags.DocsV1: json.RawMessage("false")}
	decision := flags.Decision(context.WithValue(ctx, platformSettingsContextKey{}, override), featureflags.DocsV1, true)
	if decision.Enabled || decision.Source != "platform" {
		t.Fatalf("explicit false did not override the environment: %+v", decision)
	}
}
