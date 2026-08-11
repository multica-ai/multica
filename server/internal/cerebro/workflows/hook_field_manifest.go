package workflows

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:generate go run ./cmd/generate-hook-field-catalog -output ../../../../packages/cerebro-workflows/core/hook-field-catalog.generated.ts

//go:embed hook_field_manifest.json
var hookFieldManifestJSON []byte

// HookFieldManifest is the canonical list of condition fields every hook
// event exposes. Both the server gates and the interface's hook editor stay
// truthful against it: the editor consumes the generated TypeScript, and Go
// tests compare managed policies and gate-emitted contexts against it.
type HookFieldManifest struct {
	Version int                      `json:"version"`
	Common  []HookFieldManifestField `json:"common"`
	Events  []HookFieldManifestEvent `json:"events"`
}

type HookFieldManifestEvent struct {
	Type   string                   `json:"type"`
	Fields []HookFieldManifestField `json:"fields"`
}

type HookFieldManifestField struct {
	Path      string                    `json:"path"`
	Label     string                    `json:"label"`
	Input     string                    `json:"input"`
	Sensitive bool                      `json:"sensitive,omitempty"`
	Options   []HookFieldManifestOption `json:"options,omitempty"`
}

type HookFieldManifestOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

var hookFieldManifest = mustParseHookFieldManifest(hookFieldManifestJSON)

func mustParseHookFieldManifest(data []byte) HookFieldManifest {
	var manifest HookFieldManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		panic(fmt.Errorf("parse Hook field manifest: %w", err))
	}
	if manifest.Version != 1 || len(manifest.Events) == 0 {
		panic("Hook field manifest must define version 1 events")
	}
	validateField := func(owner string, field HookFieldManifestField) {
		if field.Path == "" || field.Label == "" {
			panic(fmt.Sprintf("Hook field manifest contains an incomplete field in %q: %#v", owner, field))
		}
		switch field.Input {
		case "text", "number", "boolean", "select":
		default:
			panic(fmt.Sprintf("Hook field manifest field %q has unknown input %q", field.Path, field.Input))
		}
		if field.Input == "select" && len(field.Options) == 0 {
			panic(fmt.Sprintf("Hook field manifest field %q is a select without options", field.Path))
		}
	}
	commonPaths := map[string]bool{}
	for _, field := range manifest.Common {
		if commonPaths[field.Path] {
			panic("Hook field manifest contains duplicate common field " + field.Path)
		}
		commonPaths[field.Path] = true
		validateField("common", field)
	}
	seen := make(map[string]bool, len(manifest.Events))
	for _, event := range manifest.Events {
		if !isSupportedHookEventType(HookEventType(event.Type)) {
			panic("Hook field manifest lists unknown event type " + event.Type)
		}
		if seen[event.Type] {
			panic("Hook field manifest contains duplicate event " + event.Type)
		}
		seen[event.Type] = true
		eventPaths := map[string]bool{}
		for _, field := range event.Fields {
			if eventPaths[field.Path] || commonPaths[field.Path] {
				panic(fmt.Sprintf("Hook field manifest contains duplicate field %q for event %s", field.Path, event.Type))
			}
			eventPaths[field.Path] = true
			validateField(event.Type, field)
		}
	}
	for eventType := range HookEventCatalog {
		if !seen[string(eventType)] {
			panic("Hook field manifest is missing event type " + string(eventType))
		}
	}
	return manifest
}

// HookFieldManifestCatalog exposes the parsed manifest to tests in other
// packages that verify gate-emitted contexts stay inside the catalog.
func HookFieldManifestCatalog() HookFieldManifest {
	return hookFieldManifest
}

// HookFieldManifestPaths returns every matchable field path for an event —
// the common paths plus its own — or nil for an unknown event type.
func HookFieldManifestPaths(eventType HookEventType) map[string]bool {
	exists := false
	for _, event := range hookFieldManifest.Events {
		if event.Type == string(eventType) {
			exists = true
			break
		}
	}
	if !exists {
		return nil
	}
	paths := map[string]bool{}
	for _, field := range hookFieldManifest.Common {
		paths[field.Path] = true
	}
	for _, event := range hookFieldManifest.Events {
		if event.Type != string(eventType) {
			continue
		}
		for _, field := range event.Fields {
			paths[field.Path] = true
		}
	}
	return paths
}

// GenerateHookFieldTypeScript renders the manifest as the TypeScript module
// the hook editor derives its field suggestions, labels, and inputs from.
func GenerateHookFieldTypeScript() []byte {
	payload := map[string]any{
		"common": hookFieldManifest.Common,
		"events": hookFieldManifest.Events,
	}
	catalog, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		panic(err)
	}
	const header = "// Code generated from server/internal/cerebro/workflows/hook_field_manifest.json. DO NOT EDIT.\n\nexport const HOOK_FIELD_CATALOG = "
	return append(append([]byte(header), catalog...), []byte(" as const;\n")...)
}
