package apps

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/cerebro/apps/tokens"
)

type recordingDeploymentExec struct {
	queries []string
	args    [][]any
}

type recordingLifecycleRuntime struct {
	calls []string
	err   error
}

func (r *recordingLifecycleRuntime) Lifecycle(_ context.Context, action, serviceID string) error {
	r.calls = append(r.calls, action+":"+serviceID)
	return r.err
}

func (f *recordingDeploymentExec) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.queries = append(f.queries, sql)
	f.args = append(f.args, args)
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func TestValidateCreateRequestRequiresStableIdentity(t *testing.T) {
	if err := validateCreateRequest(createRequest{Name: "Allergen Formatter", Slug: "allergen-formatter"}); err != nil {
		t.Fatalf("valid app rejected: %v", err)
	}
	for _, req := range []createRequest{
		{Name: "", Slug: "app"},
		{Name: "App", Slug: ""},
		{Name: "App", Slug: "Not Stable"},
	} {
		if err := validateCreateRequest(req); err == nil {
			t.Fatalf("invalid app accepted: %+v", req)
		}
	}
}

func TestMiniAppsServerSurfaceDefaultsOff(t *testing.T) {
	t.Setenv("CEREBRO_MINI_APPS_ENABLED", "")
	h := NewHandler(nil)
	recorder := httptest.NewRecorder()
	h.RequireEnabled(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("default-off surface returned %d", recorder.Code)
	}
	t.Setenv("CEREBRO_MINI_APPS_ENABLED", "true")
	h = NewHandler(nil)
	recorder = httptest.NewRecorder()
	h.RequireEnabled(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("enabled surface returned %d", recorder.Code)
	}
}

func TestValidatePublishRequestRequiresSemverReleaseNotesAndFiles(t *testing.T) {
	valid := publishRequest{
		Version:      "1.0.0",
		ReleaseNotes: "Initial release",
		Files:        validBundleFiles(),
	}
	if err := validatePublishRequest(valid); err != nil {
		t.Fatalf("valid publish rejected: %v", err)
	}
	for _, mutate := range []func(*publishRequest){
		func(r *publishRequest) { r.Version = "latest" },
		func(r *publishRequest) { r.ReleaseNotes = " " },
		func(r *publishRequest) { r.Files = nil },
	} {
		req := valid
		mutate(&req)
		if err := validatePublishRequest(req); err == nil {
			t.Fatalf("invalid publish accepted: %+v", req)
		}
	}
}

func TestRuntimeCallbackOnlyMakesReadyVersionCurrent(t *testing.T) {
	appID := uuid.MustParse("f1540000-0000-4154-8154-000000000001")
	ready := &recordingDeploymentExec{}
	if err := updateDeploymentState(context.Background(), ready, appID, "1.0.0", deploymentCallback{Status: "ready", ExternalServiceID: "service-1", InternalDomain: "app.internal"}); err != nil {
		t.Fatalf("ready callback: %v", err)
	}
	if len(ready.queries) != 3 || !strings.Contains(ready.queries[1], "current_version") || !strings.Contains(ready.queries[1], "status='approved'") || !strings.Contains(ready.queries[2], "app.version.published") {
		t.Fatalf("ready callback did not switch current version atomically: %#v", ready.queries)
	}
	// Parameters used only inside jsonb_build_object need an explicit cast:
	// pgx prepares the statement and Postgres cannot infer the type, which
	// rejected every runtime callback on staging (FIR-3315).
	if !strings.Contains(ready.queries[2], "jsonb_build_object('version',$2::text)") {
		t.Fatalf("ready audit insert lost its parameter cast: %#v", ready.queries[2])
	}

	failed := &recordingDeploymentExec{}
	if err := updateDeploymentState(context.Background(), failed, appID, "2.0.0", deploymentCallback{Status: "failed", Error: "/srv/private.js"}); err != nil {
		t.Fatalf("failed callback: %v", err)
	}
	if len(failed.queries) != 2 || strings.Contains(failed.queries[0], "current_version") || !strings.Contains(failed.queries[1], "app.runtime.failed") {
		t.Fatalf("failed callback changed current version: %#v", failed.queries)
	}
	if !strings.Contains(failed.queries[1], "jsonb_build_object('version',$2::text)") {
		t.Fatalf("failed audit insert lost its parameter cast: %#v", failed.queries[1])
	}
	if err := updateDeploymentState(context.Background(), failed, appID, "2.0.0", deploymentCallback{Status: "unknown"}); err == nil {
		t.Fatal("unknown deployment status was accepted")
	}
}

func TestRuntimeLifecycleAppliesEveryServiceAndStopsOnFailure(t *testing.T) {
	runtime := &recordingLifecycleRuntime{}
	if err := applyRuntimeLifecycle(context.Background(), runtime, "delete", []string{"service-1", "service-2"}); err != nil {
		t.Fatalf("delete services: %v", err)
	}
	if strings.Join(runtime.calls, ",") != "delete:service-1,delete:service-2" {
		t.Fatalf("unexpected lifecycle calls: %#v", runtime.calls)
	}
	runtime.err = errors.New("provider unavailable")
	if err := applyRuntimeLifecycle(context.Background(), runtime, "pause", []string{"service-3"}); err == nil {
		t.Fatal("provider failure was ignored")
	}
}

func TestLifecycleStateTransitionsAreDurableAndAudited(t *testing.T) {
	appID := uuid.MustParse("f1540000-0000-4154-8154-000000000001")
	actorID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	recorder := &recordingDeploymentExec{}
	if err := markDeploymentsDeleting(context.Background(), recorder, appID); err != nil {
		t.Fatalf("mark deleting: %v", err)
	}
	if err := recordAppLifecycleAudit(context.Background(), recorder, appID, actorID, "app.deleted"); err != nil {
		t.Fatalf("record delete: %v", err)
	}
	if err := recordAppLifecycleAudit(context.Background(), recorder, appID, actorID, "app.disabled"); err != nil {
		t.Fatalf("record disable: %v", err)
	}
	if len(recorder.queries) != 3 || !strings.Contains(recorder.queries[0], "status='deleting'") || recorder.args[1][2] != "app.deleted" || recorder.args[2][2] != "app.disabled" {
		t.Fatalf("lifecycle state was not durable and audited: %#v", recorder.queries)
	}
}

func TestRollbackWaitsForAHealthyTargetAndRecordsIntent(t *testing.T) {
	appID := uuid.MustParse("f1540000-0000-4154-8154-000000000001")
	actorID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	recorder := &recordingDeploymentExec{}
	if err := markRollbackProvisioning(context.Background(), recorder, appID, actorID, "1.0.0"); err != nil {
		t.Fatalf("prepare rollback: %v", err)
	}
	if len(recorder.queries) != 2 || !strings.Contains(recorder.queries[0], "status='provisioning'") || strings.Contains(recorder.queries[0], "current_version") || recorder.args[1][2] != "app.version.rollback" {
		t.Fatalf("rollback switched before health or missed audit: queries=%#v args=%#v", recorder.queries, recorder.args)
	}
	if !strings.Contains(recorder.queries[1], "jsonb_build_object('version',$4::text") {
		t.Fatalf("rollback audit insert lost its parameter cast: %#v", recorder.queries[1])
	}
}

func TestDeploymentRetryOnlyResumesTheSameFailedBundle(t *testing.T) {
	appID := uuid.MustParse("f1540000-0000-4154-8154-000000000001")
	recorder := &recordingDeploymentExec{}
	if err := markDeploymentRetrying(context.Background(), recorder, appID, "1.0.0", "bundle-sha"); err != nil {
		t.Fatalf("prepare retry: %v", err)
	}
	if len(recorder.queries) != 1 || !strings.Contains(recorder.queries[0], "status='failed'") || !strings.Contains(recorder.queries[0], "bundle_sha256=$3") || !strings.Contains(recorder.queries[0], "status='provisioning'") {
		t.Fatalf("retry transition is not immutable and atomic: %#v", recorder.queries)
	}
}

func TestValidatePreviewSnapshotAcceptsTheAppFileDirectly(t *testing.T) {
	snapshot := json.RawMessage(`{"manifest":{"schema_version":"1","name":"Allergen Formatter"},"frontend":{"entry":"index.js"}}`)
	if err := validateSnapshot(snapshot); err != nil {
		t.Fatalf("valid preview rejected: %v", err)
	}
	if err := validateSnapshot(json.RawMessage(`{"manifest":{}}`)); err == nil {
		t.Fatal("preview without a valid manifest was accepted")
	}
}

func TestValidateWorkflowDefinitionSupportsV1LinearChain(t *testing.T) {
	valid := json.RawMessage(`{
		"schema_version":"1",
		"trigger":{"id":"trigger","type":"manual","config":{}},
		"steps":[
			{"id":"read","type":"registry.read","config":{"data_source_id":"source"}},
			{"id":"filter","type":"filter","config":{"expression":"read.count > 0"}},
			{"id":"view","type":"view.show_and_wait","config":{"view_id":"approve"}}
		]
	}`)
	if err := validateWorkflowDefinition(valid); err != nil {
		t.Fatalf("valid workflow rejected: %v", err)
	}
	if err := validateWorkflowDefinition(json.RawMessage(`{"schema_version":"1","steps":[]}`)); err == nil {
		t.Fatal("workflow without a trigger was accepted")
	}
}

func TestValidateScopesRejectsUnknownOrEmptyCeilings(t *testing.T) {
	valid := []tokens.Scope{{ResourceType: "data_source", ResourceID: "products", Access: "read_write"}}
	if err := validateScopes(valid); err != nil {
		t.Fatalf("valid scopes rejected: %v", err)
	}
	for _, scopes := range [][]tokens.Scope{
		nil,
		{{ResourceType: "unknown", ResourceID: "products", Access: "read"}},
		{{ResourceType: "data_source", ResourceID: "", Access: "read"}},
		{{ResourceType: "data_source", ResourceID: "products", Access: "admin"}},
	} {
		if err := validateScopes(scopes); err == nil {
			t.Fatalf("invalid scopes accepted: %+v", scopes)
		}
	}
}

func TestMiniAppsRouterExposesAppDetail(t *testing.T) {
	raw, err := os.ReadFile("../../../cmd/server/router.go")
	if err != nil {
		t.Fatalf("read router: %v", err)
	}
	if !strings.Contains(string(raw), `r.Get("/{id}", cerebroAppsHandler.Get)`) {
		t.Fatal("mini apps detail route is missing")
	}
}

func TestAppListAndOpenUseCollectionAccess(t *testing.T) {
	handler, err := os.ReadFile("handler.go")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../../migrations/9140_cerebro_app_collections.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{"handler": string(handler), "migration": string(migration)} {
		if !strings.Contains(raw, "cerebro_app_folder_grant_visible") {
			t.Fatalf("%s does not enforce Apps Collection access", name)
		}
	}
}

func TestMiniAppsRouterExposesInteractiveViewRequests(t *testing.T) {
	raw, err := os.ReadFile("../../../cmd/server/router.go")
	if err != nil {
		t.Fatalf("read router: %v", err)
	}
	if !strings.Contains(string(raw), `r.Get("/view-requests/{requestId}", cerebroAppsHandler.GetViewRequest)`) {
		t.Fatal("interactive view request route is missing")
	}
}

func TestViewSubmissionCanResumeOneRequest(t *testing.T) {
	req := viewSubmissionRequest{RequestID: "11111111-1111-4111-8111-111111111111", Version: "1.0.0", Value: json.RawMessage(`{"approved":true}`)}
	if err := validateViewSubmission(req); err != nil {
		t.Fatalf("valid request-bound submission rejected: %v", err)
	}
	req.RequestID = "not-a-uuid"
	if err := validateViewSubmission(req); err == nil {
		t.Fatal("invalid request id was accepted")
	}
}

func TestConnectionCallRequiresAnApprovedIntegrationScope(t *testing.T) {
	scopes := []tokens.Scope{{ResourceType: "integration", ResourceID: "connection-1", Access: "read_write"}}
	if !approvedConnectionScope(scopes, "connection-1") {
		t.Fatal("approved connection scope was rejected")
	}
	if approvedConnectionScope(scopes, "connection-2") {
		t.Fatal("a different connection escaped the app scope ceiling")
	}
	if approvedConnectionScope([]tokens.Scope{{ResourceType: "data_source", ResourceID: "connection-1", Access: "read"}}, "connection-1") {
		t.Fatal("a data source scope was treated as a connection grant")
	}
}

func TestAIGatewayCallUsesThePersonBoundToken(t *testing.T) {
	var authorization string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{\"allergens\":[\"MILK\"]}`}}}})
	}))
	defer gateway.Close()

	result, err := callAIGateway(context.Background(), gateway.Client(), tokens.Token{Key: "person-key", AIBaseURL: gateway.URL}, map[string]any{"model": "claude-haiku-4-5", "messages": []any{}})
	if err != nil {
		t.Fatalf("AI gateway call: %v", err)
	}
	if authorization != "Bearer person-key" || !strings.Contains(string(result), `"choices"`) {
		t.Fatalf("person-bound gateway contract was not preserved: authorization=%q result=%s", authorization, result)
	}
}

func TestWorkerRegistryCallRequiresExactApprovedResourceScope(t *testing.T) {
	scopes := []tokens.Scope{{ResourceType: "data_source", ResourceID: "products", Access: "read"}, {ResourceType: "data_destination", ResourceID: "orders", Access: "write"}}
	if !approvedRegistryScope(scopes, "read", "products") || !approvedRegistryScope(scopes, "write", "orders") {
		t.Fatal("approved Registry resource scope was rejected")
	}
	if approvedRegistryScope(scopes, "write", "products") || approvedRegistryScope(scopes, "read", "orders") || approvedRegistryScope(scopes, "read", "customers") {
		t.Fatal("Registry host call escaped its exact resource scope")
	}
}

func TestWorkerGrantFailsClosedWithoutRuntimeSecret(t *testing.T) {
	handler := &Handler{}
	if _, _, err := handler.workerGrant(httptest.NewRequest(http.MethodPost, "/", nil)); err == nil {
		t.Fatal("worker grant authentication accepted an empty signing secret")
	}
}

func TestMiniAppsRouterExposesScopedConnectionCall(t *testing.T) {
	raw, err := os.ReadFile("../../../cmd/server/router.go")
	if err != nil {
		t.Fatalf("read router: %v", err)
	}
	if !strings.Contains(string(raw), `r.Post("/api/cerebro/connections/{id}/call", cerebroAppsHandler.CallConnection)`) {
		t.Fatal("scoped mini-app connection call route is missing")
	}
}

func TestMiniAppsRouterExposesMemberBoundInvoke(t *testing.T) {
	raw, err := os.ReadFile("../../../cmd/server/router.go")
	if err != nil {
		t.Fatalf("read router: %v", err)
	}
	if !strings.Contains(string(raw), `r.Post("/{id}/invoke", cerebroAppsHandler.Invoke)`) {
		t.Fatal("member-bound app invoke route is missing")
	}
}

func TestMiniAppsRouterExposesSignedRuntimeBundleAndCallback(t *testing.T) {
	raw, err := os.ReadFile("../../../cmd/server/router.go")
	if err != nil {
		t.Fatalf("read router: %v", err)
	}
	router := string(raw)
	for _, route := range []string{
		`r.Get("/api/cerebro/apps-internal/deployments", cerebroAppsHandler.PendingDeployments)`,
		`r.Get("/api/cerebro/apps-internal/deployments/{id}/{version}", cerebroAppsHandler.DeploymentInfo)`,
		`r.Get("/api/cerebro/apps-internal/{id}/{version}/bundle", cerebroAppsHandler.BundleDownload)`,
		`r.Post("/api/cerebro/apps-internal/{id}/{version}/callback", cerebroAppsHandler.RuntimeCallback)`,
		`r.Post("/api/cerebro/apps-internal/host/registry", cerebroAppsHandler.WorkerRegistryCall)`,
		`r.Post("/api/cerebro/apps-internal/host/connection", cerebroAppsHandler.WorkerConnectionCall)`,
	} {
		if !strings.Contains(router, route) {
			t.Fatalf("signed mini-app runtime route is missing: %s", route)
		}
	}
}
