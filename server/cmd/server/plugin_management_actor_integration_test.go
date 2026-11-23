package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/featureflag"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

const consentPluginManifest = `{
  "manifest_version": 1, "key": "com.example.consent", "name": "Consent",
  "version": "1.0.0", "author": {"name": "example"}, "scopes": ["issues:read"],
  "config": {"label": {"type": "string", "label": "Label"}},
  "contributes": {"surfaces": [
    {"key": "panel", "type": "issue_panel", "name": "Panel", "entry": "ui.js"}
  ]}
}`

type pluginConsentFixture struct {
	db                  *dbfx.Fixture
	router              http.Handler
	h                   *handler.Handler
	pat, task, cloud    string
	packageID           string
	versionID           string
	installationID      string
	upload, contentType string
}

func newPluginConsentFixture(t *testing.T) *pluginConsentFixture {
	t.Helper()
	f := dbfx.New(testPool, testWorkspaceID, testUserID)
	f.WorkspaceID = f.Workspace(t, "Plugin consent", "plugin-consent-"+uuid.NewString())
	f.Member(t, f.WorkspaceID, f.UserID, "owner")
	runtimeID := f.Runtime(t, "Plugin consent")
	agentID := f.Agent(t, "Plugin consent", runtimeID)
	taskID := f.Task(t, agentID, dbfx.Cols{"runtime_id": runtimeID})
	task := "mat_" + uuid.NewString()
	f.InsertNoID(t, "task_token", dbfx.Cols{
		"token_hash": auth.HashToken(task), "task_id": taskID, "agent_id": agentID,
		"workspace_id": f.WorkspaceID, "user_id": f.UserID,
		"expires_at": dbfx.Raw("now() + interval '1 hour'"),
	}, "token_hash = $1", auth.HashToken(task))
	pat, err := auth.GeneratePATToken()
	if err != nil {
		t.Fatal(err)
	}
	f.Insert(t, "personal_access_token", dbfx.Cols{
		"user_id": f.UserID, "name": "plugin consent", "token_hash": auth.HashToken(pat), "token_prefix": pat[:12],
	})
	cloud := "mcn_" + uuid.NewString()
	fleet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Token string `json:"token"`
		}
		if r.URL.Path != "/api/v1/pat/verify" || json.NewDecoder(r.Body).Decode(&request) != nil || request.Token != cloud {
			http.Error(w, "invalid verification request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"valid": true, "owner_id": f.UserID})
	}))
	t.Cleanup(fleet.Close)
	t.Setenv("MULTICA_CLOUD_URL", fleet.URL)
	t.Setenv("MULTICA_PLUGIN_SECRET_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	t.Setenv("MULTICA_PLUGIN_SURFACE_ORIGIN", "https://plugin-content.example.test")
	flags := featureflag.NewStaticProvider()
	flags.Set(featureflags.PluginsV1, featureflag.Rule{Default: true})
	router, h := NewRouterWithOptions(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil,
		RouterOptions{FeatureFlags: featureflag.NewService(flags)})
	h.PluginService.LocalDir = t.TempDir()
	dir := filepath.Join(h.PluginService.LocalDir, "consent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{plugincontract.ManifestFilename: consentPluginManifest, "ui.js": "// panel"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	workspaceID, _ := util.ParseUUID(f.WorkspaceID)
	userID, _ := util.ParseUUID(f.UserID)
	t.Cleanup(func() {
		if err := h.Queries.DeleteWorkspacePluginData(context.Background(), workspaceID); err != nil {
			t.Errorf("clean up plugin data: %v", err)
		}
	})
	published, err := h.PluginService.PublishLocalBundle(context.Background(), workspaceID, userID, "consent")
	if err != nil {
		t.Fatalf("publish fixture: %v", err)
	}
	installed, err := h.PluginService.InstallPlugin(context.Background(), workspaceID, userID, published.Versions[0].ID, []string{"issues:read"})
	if err != nil {
		t.Fatalf("install fixture: %v", err)
	}
	if _, err := h.PluginService.IssueInstallToken(context.Background(), installed.ID); err != nil {
		t.Fatal(err)
	}
	// Both publishing paths carry a new version, so an unguarded publish can
	// write rather than merely conflict with the seeded version.
	manifest := strings.Replace(consentPluginManifest, "1.0.0", "2.0.0", 1)
	if err := os.WriteFile(filepath.Join(dir, plugincontract.ManifestFilename), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	for name, content := range map[string]string{plugincontract.ManifestFilename: manifest, "ui.js": "// panel v2"} {
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	var upload bytes.Buffer
	mw := multipart.NewWriter(&upload)
	part, err := mw.CreateFormFile("bundle", "plugin.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(archive.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &pluginConsentFixture{db: f, router: router, h: h, pat: pat, task: task, cloud: cloud,
		packageID: published.ID, versionID: published.Versions[0].ID, installationID: util.UUIDToString(installed.ID),
		upload: upload.String(), contentType: mw.FormDataContentType()}
}

func (f *pluginConsentFixture) request(method, path, token string, body any) *http.Request {
	req := dbfx.JSONRequest(method, path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Workspace-ID", f.db.WorkspaceID)
	// Auth must discard this client assertion for both machine auth branches.
	req.Header.Set("X-Actor-Source", "member")
	req.Header.Set("X-Multica-Plugin-Installation", f.installationID)
	if path == f.base()+"/packages" && method == http.MethodPost {
		req.Body = io.NopCloser(strings.NewReader(f.upload))
		req.Header.Set("Content-Type", f.contentType)
	}
	return req
}

func (f *pluginConsentFixture) base() string {
	return "/api/workspaces/" + f.db.WorkspaceID + "/plugins"
}

func (f *pluginConsentFixture) snapshot(t *testing.T) string {
	t.Helper()
	var state string
	f.db.QueryRow(t, `SELECT jsonb_build_object(
  'packages', (SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM plugin_package p WHERE workspace_id = $1),
  'versions', (SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM plugin_package_version v WHERE workspace_id = $1),
  'files', (SELECT jsonb_agg(to_jsonb(f) ORDER BY version_id, path) FROM plugin_package_file f
            WHERE version_id IN (SELECT id FROM plugin_package_version WHERE workspace_id = $1)),
  'installations', (SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM plugin_installation i WHERE workspace_id = $1),
  'secrets', (SELECT jsonb_agg(to_jsonb(s) ORDER BY installation_id, key) FROM plugin_secret s
              WHERE installation_id IN (SELECT id FROM plugin_installation WHERE workspace_id = $1))
)::text`, f.db.WorkspaceID).Scan(&state)
	return state
}

func TestPluginManagementRejectsMachineCredentials(t *testing.T) {
	f := newPluginConsentFixture(t)
	base := f.base()
	installation := base + "/" + f.installationID
	cases := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, base + "/packages", nil},
		{http.MethodPost, base + "/packages/local", map[string]any{"name": "consent"}},
		{http.MethodDelete, base + "/packages/" + f.packageID, nil},
		{http.MethodPost, base + "/preview", map[string]any{"version_id": f.versionID}},
		{http.MethodPost, base, map[string]any{"version_id": f.versionID, "granted_scopes": []string{"issues:read"}}},
		{http.MethodDelete, installation, nil},
		{http.MethodPost, installation + "/token", nil},
		{http.MethodDelete, installation + "/token", nil},
		{http.MethodPut, installation + "/mcp/toolbox/tools", map[string]any{"tools": []string{"read"}}},
		{http.MethodPut, installation + "/config", map[string]any{"values": map[string]any{"label": "changed"}}},
		{http.MethodPost, installation + "/enable", nil},
		{http.MethodPost, installation + "/disable", nil},
	}
	for _, role := range []string{"owner", "admin"} {
		f.db.Exec(t, `UPDATE member SET role = $1 WHERE workspace_id = $2 AND user_id = $3`, role, f.db.WorkspaceID, f.db.UserID)
		for name, token := range map[string]string{"task": f.task, "cloud": f.cloud} {
			for _, tc := range cases {
				t.Run(role+"/"+name+"/"+tc.method+tc.path, func(t *testing.T) {
					before := f.snapshot(t)
					dbfx.Call(t, f.router.ServeHTTP, f.request(tc.method, tc.path, token, tc.body)).Want(http.StatusForbidden)
					if f.snapshot(t) != before {
						t.Fatal("rejected management request changed plugin state")
					}
				})
			}
			for _, path := range []string{base, base + "/packages", installation + "/surfaces/panel/launch", "/api/plugin-bridge/v1/context"} {
				t.Run(role+"/"+name+"/read"+path, func(t *testing.T) {
					dbfx.Call(t, f.router.ServeHTTP, f.request(http.MethodGet, path, token, nil)).Want(http.StatusOK)
				})
			}
		}
	}
}

func TestPluginManagementPreservesHumanAuthorization(t *testing.T) {
	for _, credential := range []string{"jwt", "pat"} {
		t.Run(credential, func(t *testing.T) {
			f := newPluginConsentFixture(t)
			token := testToken
			if credential == "pat" {
				token = f.pat
			}
			base, installation := f.base(), f.base()+"/"+f.installationID
			do := func(method, path string, body any, status int) *dbfx.Response {
				return dbfx.Call(t, f.router.ServeHTTP, f.request(method, path, token, body)).Want(status)
			}
			outsiderID := f.db.User(t, "Outside plugin workspace", uuid.NewString()+"@example.test")
			outsiderToken, err := generateTestJWT(outsiderID, "outside@example.test", "Outside")
			if err != nil {
				t.Fatal(err)
			}
			before := f.snapshot(t)
			dbfx.Call(t, f.router.ServeHTTP, f.request(http.MethodPost, installation+"/token", outsiderToken, nil)).Want(http.StatusNotFound)
			if f.snapshot(t) != before {
				t.Fatal("user outside the workspace changed plugin state")
			}
			// The human gate supplements the role gate; it does not grant a
			// member or a user outside this workspace management authority.
			for _, role := range []string{"member", "admin"} {
				f.db.Exec(t, `UPDATE member SET role = $1 WHERE workspace_id = $2 AND user_id = $3`, role, f.db.WorkspaceID, f.db.UserID)
				want := http.StatusForbidden
				if role == "admin" {
					want = http.StatusOK
				}
				do(http.MethodPost, base+"/preview", map[string]any{"version_id": f.versionID}, want)
			}
			do(http.MethodPost, base+"/packages", nil, http.StatusCreated)
			// Local publication remains available for the same administrator.
			manifest := strings.Replace(consentPluginManifest, "1.0.0", "3.0.0", 1)
			if err := os.WriteFile(filepath.Join(f.h.PluginService.LocalDir, "consent", plugincontract.ManifestFilename), []byte(manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			do(http.MethodPost, base+"/packages/local", map[string]any{"name": "consent"}, http.StatusCreated)
			do(http.MethodPost, base, map[string]any{"version_id": f.versionID, "granted_scopes": []string{"issues:read"}}, http.StatusCreated)
			do(http.MethodPut, installation+"/config", map[string]any{"values": map[string]any{"label": "human"}}, http.StatusOK)
			// This fixture has no MCP hook: human approval reaches hook
			// validation, while the same request above is forbidden for machines.
			do(http.MethodPut, installation+"/mcp/toolbox/tools", map[string]any{"tools": []string{"read"}}, http.StatusNotFound)
			do(http.MethodPost, installation+"/disable", nil, http.StatusOK)
			do(http.MethodPost, installation+"/enable", nil, http.StatusOK)
			var credentials struct {
				Token string `json:"token"`
			}
			do(http.MethodPost, installation+"/token", nil, http.StatusOK).JSON(&credentials)
			dbfx.Call(t, f.router.ServeHTTP, f.request(http.MethodGet, "/v1/context", credentials.Token, nil)).Want(http.StatusOK)
			workspaceID, _ := util.ParseUUID(f.db.WorkspaceID)
			installed, err := f.h.PluginService.InstallationForWorkspace(context.Background(), workspaceID, f.installationID)
			if err != nil {
				t.Fatal(err)
			}
			callback, err := f.h.PluginService.Callbacks.Issue(context.Background(), service.HookInvocation{
				Installation: installed, Trigger: plugincontract.TriggerManual,
				Actor: service.HookActor{Type: "plugin", ID: installed.ID},
			})
			if err != nil {
				t.Fatal(err)
			}
			dbfx.Call(t, f.router.ServeHTTP, f.request(http.MethodGet, "/v1/context", callback, nil)).Want(http.StatusOK)
			do(http.MethodDelete, installation+"/token", nil, http.StatusNoContent)
			do(http.MethodDelete, installation, nil, http.StatusNoContent)
			do(http.MethodDelete, base+"/packages/"+f.packageID, nil, http.StatusNoContent)
		})
	}
}
