package workflows

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:generate go run ./cmd/generate-hook-action-catalog -output ../../../../packages/cerebro-workflows/core/hook-action-catalog.generated.ts

//go:embed hook_action_manifest.json
var hookActionManifestJSON []byte

type HookActionManifest struct {
	Version int                        `json:"version"`
	Actions []HookActionManifestAction `json:"actions"`
}

type HookActionManifestAction struct {
	Type        string                    `json:"type"`
	Label       string                    `json:"label"`
	Description string                    `json:"description"`
	Capability  string                    `json:"capability,omitempty"`
	Fields      []HookActionManifestField `json:"fields"`
}

type HookActionManifestField struct {
	Key      string                     `json:"key"`
	Label    string                     `json:"label"`
	Input    string                     `json:"input"`
	Target   string                     `json:"target,omitempty"`
	Required bool                       `json:"required,omitempty"`
	Default  *bool                      `json:"default,omitempty"`
	Summary  string                     `json:"summary"`
	Help     string                     `json:"help,omitempty"`
	Options  []HookActionManifestOption `json:"options,omitempty"`
}

type HookActionManifestOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

var hookActionManifest = mustParseHookActionManifest(hookActionManifestJSON)
var hookActionManifestByType = indexHookActionManifest(hookActionManifest)

func mustParseHookActionManifest(data []byte) HookActionManifest {
	var manifest HookActionManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		panic(fmt.Errorf("parse Hook action manifest: %w", err))
	}
	if manifest.Version != 1 || len(manifest.Actions) == 0 {
		panic("Hook action manifest must define version 1 actions")
	}
	seen := make(map[string]bool, len(manifest.Actions))
	for _, action := range manifest.Actions {
		if strings.TrimSpace(action.Type) == "" || strings.TrimSpace(action.Label) == "" || strings.TrimSpace(action.Description) == "" {
			panic(fmt.Sprintf("Hook action manifest contains an incomplete action: %#v", action))
		}
		if seen[action.Type] {
			panic("Hook action manifest contains duplicate action " + action.Type)
		}
		seen[action.Type] = true
		for _, field := range action.Fields {
			if field.Key == "" || field.Label == "" || field.Input == "" || field.Summary == "" {
				panic(fmt.Sprintf("Hook action manifest contains an incomplete %s field: %#v", action.Type, field))
			}
		}
	}
	return manifest
}

func indexHookActionManifest(manifest HookActionManifest) map[string]HookActionManifestAction {
	index := make(map[string]HookActionManifestAction, len(manifest.Actions))
	for _, action := range manifest.Actions {
		index[action.Type] = action
	}
	return index
}

func manifestHookActionTypes() []string {
	actionTypes := make([]string, 0, len(hookActionManifest.Actions))
	for _, action := range hookActionManifest.Actions {
		actionTypes = append(actionTypes, action.Type)
	}
	return actionTypes
}

func GenerateHookActionTypeScript() []byte {
	catalog, err := json.MarshalIndent(hookActionManifest.Actions, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(append([]byte("// Code generated from server/internal/cerebro/workflows/hook_action_manifest.json. DO NOT EDIT.\n\nexport const HOOK_ACTION_CATALOG = "), catalog...), []byte(" as const;\n")...)
}

func generateHookActionTypeScript() []byte {
	return GenerateHookActionTypeScript()
}
