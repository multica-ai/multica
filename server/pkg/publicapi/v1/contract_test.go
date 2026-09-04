package publicapiv1

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPICoversCapabilityLedger(t *testing.T) {
	var doc struct {
		OpenAPI string `yaml:"openapi"`
		Info    struct {
			Version string `yaml:"version"`
		} `yaml:"info"`
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(OpenAPI(), &doc); err != nil {
		t.Fatalf("parse embedded OpenAPI: %v", err)
	}
	if doc.OpenAPI != "3.1.0" || doc.Info.Version != "v1" {
		t.Fatalf("unexpected contract version: openapi=%q info.version=%q", doc.OpenAPI, doc.Info.Version)
	}

	want := make(map[string]map[string]bool)
	for _, operation := range Operations {
		methods := want[operation.Path]
		if methods == nil {
			methods = make(map[string]bool)
			want[operation.Path] = methods
		}
		methods[strings.ToLower(operation.Method)] = true
	}
	if len(doc.Paths) != len(want) {
		t.Fatalf("OpenAPI path count = %d, ledger path count = %d", len(doc.Paths), len(want))
	}
	for path, methods := range want {
		pathItem, ok := doc.Paths[path]
		if !ok {
			t.Errorf("OpenAPI is missing %s", path)
			continue
		}
		for method := range methods {
			if _, ok := pathItem[method]; !ok {
				t.Errorf("OpenAPI is missing %s %s", strings.ToUpper(method), path)
			}
		}
	}
	for _, operation := range Operations {
		method := strings.ToLower(operation.Method)
		raw, ok := doc.Paths[operation.Path][method].(map[string]any)
		if !ok {
			t.Errorf("OpenAPI operation %s %s is not an object", operation.Method, operation.Path)
			continue
		}
		if got, _ := raw["x-multica-contract"].(string); got != string(operation.Contract) {
			t.Errorf("%s %s x-multica-contract = %q, want %q", operation.Method, operation.Path, got, operation.Contract)
		}
		gotScope, _ := raw["x-multica-scope"].(string)
		if gotScope != operation.Policy.Scope {
			t.Errorf("%s %s x-multica-scope = %q, want %q", operation.Method, operation.Path, gotScope, operation.Policy.Scope)
		}
	}
	if _, exposed := doc.Paths["/hooks/{hook_key}"]; exposed {
		t.Fatal("Plugin-only person hook leaked into the public contract")
	}
}
