package apps

import (
	"encoding/json"
	"testing"
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
