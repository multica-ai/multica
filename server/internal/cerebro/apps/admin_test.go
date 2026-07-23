package apps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type successfulAppRuntime struct{ deployments []RuntimeDeploymentRequest }

func (r *successfulAppRuntime) Deploy(_ context.Context, deployment RuntimeDeploymentRequest) error {
	r.deployments = append(r.deployments, deployment)
	return nil
}
func (*successfulAppRuntime) Invoke(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
	return nil, nil
}
func (*successfulAppRuntime) Lifecycle(context.Context, string, string) error { return nil }

func TestMiniAppLifecyclePermissionsAndCascadeDeletion(t *testing.T) {
	for _, capability := range []string{"apps.create", "apps.manage", "apps.delete"} {
		if !isKnownAppCapability(capability) {
			t.Errorf("lifecycle permission %q is not recognized", capability)
		}
	}
	if isKnownAppCapability("use_apps") {
		t.Fatal("legacy app permission is still accepted")
	}
	raw, err := os.ReadFile("../../../migrations/9135_cerebro_mini_apps.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	schema := string(raw)
	for _, table := range []string{"cerebro_app_version", "cerebro_app_kv"} {
		if !strings.Contains(schema, "REFERENCES cerebro_app(id) ON DELETE CASCADE") {
			t.Fatalf("%s is not covered by app cascade deletion", table)
		}
	}
}

func TestAllergenFormatterIsAnInstallablePublishedBuiltin(t *testing.T) {
	firstWorkspace := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	secondWorkspace := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	firstID := allergenFormatterIDForWorkspace(firstWorkspace)
	if firstID == allergenFormatterIDForWorkspace(secondWorkspace) {
		t.Fatal("Allergen Formatter must have a workspace-owned app id")
	}
	if firstID != allergenFormatterIDForWorkspace(firstWorkspace) {
		t.Fatal("Allergen Formatter installation must be idempotent within a workspace")
	}
	if !strings.Contains(string(allergenFormatterSnapshot), `"Allergen Formatter"`) || !strings.Contains(string(allergenFormatterSnapshot), `"integration"`) {
		t.Fatal("FIR-154 snapshot is not the real scoped app bundle")
	}
	bundle, err := ValidateBundle("Allergen Formatter", allergenFormatterVersion, allergenFormatterBundleFiles())
	if err != nil {
		t.Fatalf("built-in bundle is not publishable: %v", err)
	}
	if len(bundle.Files) != 4 || bundle.SHA256 == "" {
		t.Fatalf("built-in bundle is incomplete: %+v", bundle)
	}
	backend := string(allergenFormatterBundleFiles()[3].Content)
	if !strings.Contains(backend, `multica.connections.call("ai_gateway", "chat.completions"`) || strings.Contains(backend, `const names=`) {
		t.Fatal("Allergen Formatter must make one person-bound AI call instead of formatting locally")
	}
	frontend := string(allergenFormatterBundleFiles()[2].Content)
	if strings.Contains(frontend, "f1540000-0000-4154-8154-000000000001") ||
		!strings.Contains(frontend, "window.location.pathname") {
		t.Fatal("Allergen Formatter must derive its installed app identity from the runtime URL")
	}
	for _, safeguard := range []string{
		`String.fromCharCode(96).repeat(3)`,
		`Array.isArray(result.formatted_ingredients)`,
		`.trim().toUpperCase()`,
	} {
		if !strings.Contains(backend, safeguard) {
			t.Fatalf("Allergen Formatter must normalize AI JSON before rendering; missing %q", safeguard)
		}
	}
}

func TestAllergenFormatterInstallsIdempotentlyInTwoWorkspacesDB(t *testing.T) {
	fixture := newAppAccessFixture(t)
	fixture.insertMember(t, fixture.otherWorkspaceID, fixture.appOwnerID, "admin")
	runtime := &successfulAppRuntime{}
	fixture.handler.runtime = runtime

	install := func(workspaceID uuid.UUID) int {
		request := httptest.NewRequest(http.MethodPost, "/api/cerebro/apps/builtins/allergen-formatter/install", strings.NewReader("{}"))
		request.Header.Set("X-User-ID", fixture.appOwnerID.String())
		request = request.WithContext(middleware.SetMemberContext(request.Context(), workspaceID.String(), db.Member{}))
		response := httptest.NewRecorder()
		fixture.handler.InstallAllergenFormatter(response, request)
		if response.Code != http.StatusAccepted && response.Code != http.StatusOK {
			t.Fatalf("install in %s returned %d: %s", workspaceID, response.Code, response.Body.String())
		}
		return response.Code
	}

	if install(fixture.workspaceID) != http.StatusAccepted || install(fixture.otherWorkspaceID) != http.StatusAccepted {
		t.Fatal("the first installation in each workspace must start a deployment")
	}
	if install(fixture.workspaceID) != http.StatusOK || install(fixture.otherWorkspaceID) != http.StatusOK {
		t.Fatal("repeating the same installation must be idempotent")
	}

	rows, err := fixture.pool.Query(context.Background(), `SELECT workspace_id,id FROM cerebro_app WHERE slug='allergen-formatter' AND workspace_id=ANY($1::uuid[]) ORDER BY workspace_id`, []uuid.UUID{fixture.workspaceID, fixture.otherWorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ids := make(map[uuid.UUID]uuid.UUID)
	for rows.Next() {
		var workspaceID, appID uuid.UUID
		if err := rows.Scan(&workspaceID, &appID); err != nil {
			t.Fatal(err)
		}
		ids[workspaceID] = appID
	}
	if len(ids) != 2 || ids[fixture.workspaceID] == ids[fixture.otherWorkspaceID] {
		t.Fatalf("expected one distinct built-in app per workspace, got %#v", ids)
	}
	if len(runtime.deployments) != 2 {
		t.Fatalf("idempotent installs started %d deployments, want 2", len(runtime.deployments))
	}
}
