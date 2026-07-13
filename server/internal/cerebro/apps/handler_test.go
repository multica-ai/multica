package apps

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/apps/tokens"
)

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

func TestValidatePublishRequestRequiresSemverReleaseNotesAndManifest(t *testing.T) {
	valid := publishRequest{
		Version:      "1.0.0",
		ReleaseNotes: "Initial release",
		Snapshot:     json.RawMessage(`{"manifest":{"schema_version":"1","name":"Allergen Formatter"},"frontend":{"entry":"index.js"}}`),
	}
	if err := validatePublishRequest(valid); err != nil {
		t.Fatalf("valid publish rejected: %v", err)
	}
	for _, mutate := range []func(*publishRequest){
		func(r *publishRequest) { r.Version = "latest" },
		func(r *publishRequest) { r.ReleaseNotes = " " },
		func(r *publishRequest) { r.Snapshot = json.RawMessage(`{}`) },
	} {
		req := valid
		mutate(&req)
		if err := validatePublishRequest(req); err == nil {
			t.Fatalf("invalid publish accepted: %+v", req)
		}
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
