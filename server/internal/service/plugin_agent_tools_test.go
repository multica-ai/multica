package service

import (
	"encoding/json"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// Two plugins may both contribute a hook called "summarize". The agent sees one
// tool list, so the names have to be distinguishable in it.
func TestPluginToolNameNamespacesByPlugin(t *testing.T) {
	first := PluginToolName("com.example.triage", "summarize")
	second := PluginToolName("ai.multica.release", "summarize")
	if first == second {
		t.Fatalf("two plugins' hooks collapsed to one tool name: %q", first)
	}
	for _, name := range []string{first, second} {
		for _, r := range name {
			isSafe := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
			if !isSafe {
				t.Fatalf("tool name %q contains %q, which providers vary on accepting", name, r)
			}
		}
	}
}

// Raw keys containing separator characters must not forge another namespace.
func TestPluginToolNameSeparatorCannotBeForged(t *testing.T) {
	// `a.b` + `c` vs `a.b_` + `c`: with a single-underscore separator both
	// become a_b_c.
	if PluginToolName("a.b", "c") == PluginToolName("a.b_", "c") {
		t.Fatal("a plugin key ending in a separator character collided with another")
	}
	if PluginToolName("x", "y__z") == PluginToolName("x__y", "z") {
		t.Fatal("a hook key containing the separator collided with a different pair")
	}
}

func TestPluginToolNameDistinguishesValidHookKeys(t *testing.T) {
	var manifest plugincontract.Manifest
	if err := json.Unmarshal([]byte(`{
		"manifest_version": 1, "key": "com.example.checks", "name": "Checks",
		"description": "Checks", "version": "1.0.0", "author": {"name": "example"},
		"scopes": ["net:example.com"]
	}`), &manifest); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"check-status", "check_status", "check-status_now", "check_status-now",
		"check-status-now", "check_status_now",
	} {
		manifest.Contributes.Hooks = append(manifest.Contributes.Hooks, plugincontract.Hook{
			Key: key, Name: key, Description: "Run " + key,
			Triggers:  []string{plugincontract.TriggerAgent},
			Transport: plugincontract.HookTransport{Type: plugincontract.TransportHTTP, URL: "https://example.com/" + key},
		})
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, err := plugincontract.ParseManifest(raw)
	if err != nil {
		t.Fatalf("the hooks must be valid in the same manifest: %v", err)
	}
	names := make(map[string]string)
	for _, hook := range parsed.Contributes.Hooks {
		name := PluginToolName(parsed.Key, hook.Key)
		if previous, exists := names[name]; exists {
			t.Errorf("valid hooks %q and %q both map to %q", previous, hook.Key, name)
		}
		names[name] = hook.Key
		if toolNameUnsafe.MatchString(name) {
			t.Errorf("tool name %q contains provider-unsafe characters", name)
		}
		if again := PluginToolName(parsed.Key, hook.Key); again != name {
			t.Errorf("tool name changed between calls: %q != %q", again, name)
		}
	}
	// Existing keys without hyphens retain their advertised names.
	if got := PluginToolName(parsed.Key, "check_status"); got != "checks_ae2ab7__check_status" {
		t.Errorf("underscore-only hook name changed: %q", got)
	}
}

// A hook that did not declare the agent trigger is not a tool. The host does
// not get to offer a call site the manifest never listed.
func TestAgentHookToolsOnlyIncludesDeclaredAgentHooks(t *testing.T) {
	installation := db.PluginInstallation{
		ID:      testInstallationID(t),
		Enabled: true,
		Manifest: []byte(`{
			"manifest_version": 1,
			"key": "com.example.mixed",
			"name": "Mixed",
			"description": "d",
			"version": "1.0.0",
			"author": {"name": "example"},
			"scopes": ["net:example.com"],
			"contributes": {"hooks": [
				{"key": "for_agent", "name": "For agent", "description": "Agent may call this.",
				 "triggers": ["agent"], "transport": {"type": "http", "url": "https://example.com/a"}},
				{"key": "manual_only", "name": "Manual only", "description": "A person picks this.",
				 "triggers": ["manual"], "transport": {"type": "http", "url": "https://example.com/m"}}
			]}
		}`),
	}

	manifest, err := ParseInstallationManifest(installation)
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	agentHooks := 0
	for _, hook := range manifest.Contributes.Hooks {
		if HookAllowsTrigger(hook, "agent") {
			agentHooks++
			if hook.Key != "for_agent" {
				t.Fatalf("hook %q is not an agent hook but was treated as one", hook.Key)
			}
		}
	}
	if agentHooks != 1 {
		t.Fatalf("agent hooks = %d, want 1", agentHooks)
	}
}

// The description the agent reads is the plugin author's, verbatim. It is what
// the agent decides on, so the host must not paraphrase it.
func TestAgentToolDescriptionIsTheManifestText(t *testing.T) {
	const wanted = "Summarize the discussion so far into decisions and open questions."
	installation := db.PluginInstallation{
		ID:      testInstallationID(t),
		Enabled: true,
		Manifest: json.RawMessage(`{
			"manifest_version": 1,
			"key": "com.example.sum",
			"name": "Sum",
			"description": "d",
			"version": "1.0.0",
			"author": {"name": "example"},
			"scopes": ["net:example.com"],
			"contributes": {"hooks": [{"key": "summarize", "name": "Summarize",
			 "description": "` + wanted + `",
			 "triggers": ["agent"], "transport": {"type": "http", "url": "https://example.com/s"}}]}
		}`),
	}
	manifest, err := ParseInstallationManifest(installation)
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if got := manifest.Contributes.Hooks[0].Description; got != wanted {
		t.Fatalf("description = %q, want the manifest text verbatim", got)
	}
}
