package plugincontract

import (
	"encoding/json"
	"strings"
	"testing"
)

// validManifest is the reference document every negative case mutates. Keeping
// one source avoids a test that passes because it drifted from the real shape.
const validManifest = `{
  "manifest_version": 1,
  "key": "com.example.hello",
  "name": "Hello Panel",
  "description": "A greeting panel.",
  "version": "1.0.0",
  "author": { "name": "example", "url": "https://example.com" },
  "icon": "icon.svg",
  "scopes": ["issues:read", "comments:write", "storage:user", "net:example.com"],
  "config": {
    "repo": { "type": "string", "label": "GitHub Repo", "required": true },
    "token": { "type": "secret", "label": "Access Token", "required": true },
    "mode": { "type": "enum", "label": "Mode", "options": ["fast", "thorough"] }
  },
  "contributes": {
    "surfaces": [{
      "key": "hello", "type": "issue_panel", "name": "Hello",
      "entry": "ui/main.js", "platforms": ["web", "desktop"]
    }],
    "hooks": [{
      "key": "summarize_thread",
      "name": "Summarize",
      "description": "Compress the issue discussion into bullet points.",
      "input_schema": { "type": "object", "properties": { "issue_id": { "type": "string" } } },
      "triggers": ["ui", "manual", "agent"],
      "transport": { "type": "http", "url": "https://example.com/hooks/summarize" },
      "timeout_ms": 10000
    }],
    "resources": [
      { "type": "skill", "key": "pr-review", "entry": "skills/pr-review/SKILL.md" }
    ]
  }
}`

func mutate(t *testing.T, edit func(doc map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(validManifest), &doc); err != nil {
		t.Fatalf("decode reference manifest: %v", err)
	}
	edit(doc)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode mutated manifest: %v", err)
	}
	return raw
}

func TestParseManifestAcceptsReferenceDocument(t *testing.T) {
	manifest, canonical, err := ParseManifest([]byte(validManifest))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if manifest.Key != "com.example.hello" || manifest.Version != "1.0.0" {
		t.Fatalf("unexpected identity: %+v", manifest)
	}
	if len(manifest.Contributes.Surfaces) != 1 || len(manifest.Contributes.Hooks) != 1 || len(manifest.Contributes.Resources) != 1 {
		t.Fatalf("unexpected contributions: %+v", manifest.Contributes)
	}
	if len(canonical) == 0 {
		t.Fatal("canonical manifest is empty")
	}
	// The canonical form must reparse: it is what an installation stores and
	// later reads back as the consented snapshot.
	if _, _, err := ParseManifest(canonical); err != nil {
		t.Fatalf("canonical manifest does not reparse: %v", err)
	}
}

func TestConfigSchemaPreservesDeclarationOrder(t *testing.T) {
	manifest, canonical, err := ParseManifest([]byte(validManifest))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	want := []string{"repo", "token", "mode"}
	if len(manifest.Config.Fields) != len(want) {
		t.Fatalf("config fields = %d, want %d", len(manifest.Config.Fields), len(want))
	}
	for i, key := range want {
		if manifest.Config.Fields[i].Key != key {
			t.Fatalf("config field %d = %q, want %q", i, manifest.Config.Fields[i].Key, key)
		}
	}
	// The generated form order must survive the snapshot round trip, otherwise
	// the same plugin renders its fields differently after an upgrade.
	if repo, token := strings.Index(string(canonical), `"repo"`), strings.Index(string(canonical), `"token"`); repo > token {
		t.Fatalf("canonical config lost declaration order: %s", canonical)
	}
	field, ok := manifest.Config.Field("mode")
	if !ok || field.Type != ConfigEnum || len(field.Options) != 2 {
		t.Fatalf("enum field not preserved: %+v", field)
	}
}

func TestParseManifestRejectsMalformedDocuments(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
		want string
	}{
		{"empty", []byte(""), "empty"},
		{"unknown top-level field", []byte(`{"manifest_version":1,"surprise":true}`), "unknown field"},
		{"trailing JSON", append([]byte(validManifest), []byte(`{"extra":1}`)...), "trailing"},
		{
			"unknown config field property",
			mutate(t, func(doc map[string]any) {
				doc["config"] = map[string]any{"a": map[string]any{"type": "string", "label": "A", "secret": true}}
			}),
			"unknown field",
		},
		{
			"wrong manifest version",
			mutate(t, func(doc map[string]any) { doc["manifest_version"] = 2 }),
			"manifest_version",
		},
		{
			"non reverse-DNS key",
			mutate(t, func(doc map[string]any) { doc["key"] = "hello" }),
			"reverse-DNS",
		},
		{
			"non semver version",
			mutate(t, func(doc map[string]any) { doc["version"] = "1.0" }),
			"semantic versioning",
		},
		{
			"overlong name",
			mutate(t, func(doc map[string]any) { doc["name"] = strings.Repeat("n", 161) }),
			"exceeds",
		},
		{
			"unknown scope",
			mutate(t, func(doc map[string]any) { doc["scopes"] = []any{"issues:read", "billing:write"} }),
			"unsupported scope",
		},
		{
			"malformed net scope",
			mutate(t, func(doc map[string]any) { doc["scopes"] = []any{"net:https://example.com"} }),
			"invalid domain",
		},
		{
			"duplicate scope",
			mutate(t, func(doc map[string]any) { doc["scopes"] = []any{"issues:read", "issues:read"} }),
			"duplicate",
		},
		{
			"empty scopes",
			mutate(t, func(doc map[string]any) { doc["scopes"] = []any{} }),
			"must not be empty",
		},
		{
			"unsupported config type",
			mutate(t, func(doc map[string]any) {
				doc["config"] = map[string]any{"a": map[string]any{"type": "json", "label": "A"}}
			}),
			"unsupported",
		},
		{
			"enum without options",
			mutate(t, func(doc map[string]any) {
				doc["config"] = map[string]any{"a": map[string]any{"type": "enum", "label": "A"}}
			}),
			"options must not be empty",
		},
		{
			"no contributions",
			mutate(t, func(doc map[string]any) { doc["contributes"] = map[string]any{} }),
			"at least one",
		},
		{
			"unsupported surface type",
			mutate(t, func(doc map[string]any) {
				contributes := doc["contributes"].(map[string]any)
				surfaces := contributes["surfaces"].([]any)
				surfaces[0].(map[string]any)["type"] = "fullscreen"
			}),
			"unsupported",
		},
		{
			"surface entry escapes the package",
			mutate(t, func(doc map[string]any) {
				contributes := doc["contributes"].(map[string]any)
				surfaces := contributes["surfaces"].([]any)
				surfaces[0].(map[string]any)["entry"] = "../../etc/passwd"
			}),
			"path traversal",
		},
		{
			"version over the column bound",
			mutate(t, func(doc map[string]any) {
				// A legal semver whose build metadata pushes it past the
				// plugin_installation.version cap.
				doc["version"] = "1.0.0+" + strings.Repeat("b", 64)
			}),
			"version exceeds",
		},
		{
			"surface entry is an HTML document",
			mutate(t, func(doc map[string]any) {
				contributes := doc["contributes"].(map[string]any)
				surfaces := contributes["surfaces"].([]any)
				surfaces[0].(map[string]any)["entry"] = "ui/index.html"
			}),
			"must be a .js or .mjs script",
		},
		{
			"hook transport on a subdomain of a net: scope",
			mutate(t, func(doc map[string]any) {
				contributes := doc["contributes"].(map[string]any)
				hooks := contributes["hooks"].([]any)
				hooks[0].(map[string]any)["transport"] = map[string]any{"type": "http", "url": "https://api.example.com/hooks/summarize"}
			}),
			"not covered by a net: scope",
		},
		{
			"surface entry is an absolute URL",
			mutate(t, func(doc map[string]any) {
				contributes := doc["contributes"].(map[string]any)
				surfaces := contributes["surfaces"].([]any)
				surfaces[0].(map[string]any)["entry"] = "https://evil.test/index.html"
			}),
			"relative path",
		},
		{
			"unsupported trigger",
			mutate(t, func(doc map[string]any) {
				contributes := doc["contributes"].(map[string]any)
				hooks := contributes["hooks"].([]any)
				hooks[0].(map[string]any)["triggers"] = []any{"cron"}
			}),
			"unsupported trigger",
		},
		{
			"event trigger without events",
			mutate(t, func(doc map[string]any) {
				contributes := doc["contributes"].(map[string]any)
				hooks := contributes["hooks"].([]any)
				hooks[0].(map[string]any)["triggers"] = []any{"event"}
			}),
			"events must not be empty",
		},
		{
			"events without the event trigger",
			mutate(t, func(doc map[string]any) {
				contributes := doc["contributes"].(map[string]any)
				hooks := contributes["hooks"].([]any)
				hooks[0].(map[string]any)["events"] = []any{"issue.created"}
			}),
			"requires the event trigger",
		},
		{
			"unknown event",
			mutate(t, func(doc map[string]any) {
				contributes := doc["contributes"].(map[string]any)
				hooks := contributes["hooks"].([]any)
				hooks[0].(map[string]any)["triggers"] = []any{"event"}
				hooks[0].(map[string]any)["events"] = []any{"issue.exploded"}
			}),
			"unsupported event",
		},
		{
			"hook transport outside the granted net scope",
			mutate(t, func(doc map[string]any) {
				contributes := doc["contributes"].(map[string]any)
				hooks := contributes["hooks"].([]any)
				hooks[0].(map[string]any)["transport"] = map[string]any{"type": "http", "url": "https://evil.test/hook"}
			}),
			"not covered by a net: scope",
		},
		{
			"plaintext hook transport",
			mutate(t, func(doc map[string]any) {
				contributes := doc["contributes"].(map[string]any)
				hooks := contributes["hooks"].([]any)
				hooks[0].(map[string]any)["transport"] = map[string]any{"type": "http", "url": "http://example.com/hook"}
			}),
			"HTTPS",
		},
		{
			"duplicate hook key",
			mutate(t, func(doc map[string]any) {
				contributes := doc["contributes"].(map[string]any)
				hooks := contributes["hooks"].([]any)
				clone := map[string]any{}
				for k, v := range hooks[0].(map[string]any) {
					clone[k] = v
				}
				contributes["hooks"] = []any{hooks[0], clone}
			}),
			"duplicate hook key",
		},
		{
			"unsupported resource type",
			mutate(t, func(doc map[string]any) {
				contributes := doc["contributes"].(map[string]any)
				contributes["resources"] = []any{map[string]any{"type": "font", "key": "a", "entry": "a"}}
			}),
			"unsupported",
		},
		{
			"resource entry does not match its key",
			mutate(t, func(doc map[string]any) {
				contributes := doc["contributes"].(map[string]any)
				contributes["resources"] = []any{map[string]any{"type": "skill", "key": "review", "entry": "skills/other/SKILL.md"}}
			}),
			"must be",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseManifest(tc.raw)
			if err == nil {
				t.Fatalf("ParseManifest accepted %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to mention %q", err.Error(), tc.want)
			}
		})
	}
}

func TestParseManifestRejectsOversizedDocument(t *testing.T) {
	oversized := make([]byte, MaxManifestSize+1)
	for i := range oversized {
		oversized[i] = ' '
	}
	if _, _, err := ParseManifest(oversized); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized manifest error = %v", err)
	}
}

func TestValidateScope(t *testing.T) {
	valid := []string{
		ScopeIssuesRead, ScopeIssuesWrite, ScopeCommentsRead, ScopeCommentsWrite,
		ScopeTasksRead, ScopeTasksWrite, ScopeAgentsRead, ScopeMembersRead,
		ScopeStorageUser, ScopeStorageWorkspace,
		"net:example.com", "net:api.example.co.uk",
	}
	for _, scope := range valid {
		if err := ValidateScope(scope); err != nil {
			t.Fatalf("ValidateScope(%q) = %v", scope, err)
		}
	}
	invalid := []string{
		"", "issues", "issues:delete", "storage:global", "net:", "net:localhost",
		"net:EXAMPLE.com", "net:example.com/path", "net:*.example.com", "NET:example.com",
	}
	for _, scope := range invalid {
		if err := ValidateScope(scope); err == nil {
			t.Fatalf("ValidateScope(%q) accepted an invalid scope", scope)
		}
	}
}

func TestNetDomainsOnlyReturnsNetScopes(t *testing.T) {
	domains := NetDomains([]string{ScopeIssuesRead, "net:example.com", ScopeStorageUser, "net:api.example.com"})
	if len(domains) != 2 || domains[0] != "example.com" || domains[1] != "api.example.com" {
		t.Fatalf("NetDomains = %v", domains)
	}
}

func TestCheckCapabilitiesReportsEveryUnavailableContribution(t *testing.T) {
	manifest, _, err := ParseManifest([]byte(validManifest))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	// The shipped host set is what gates a staged rollout: a manifest naming a
	// capability this build cannot run must fail loudly, never install
	// half-working.
	err = manifest.CheckCapabilities(HostCapabilities())
	if err == nil {
		t.Fatal("CheckCapabilities accepted contributions the host cannot run")
	}
	var unavailable *ErrCapabilityUnavailable
	if !asCapabilityError(err, &unavailable) {
		t.Fatalf("error type = %T, want *ErrCapabilityUnavailable", err)
	}
	for _, want := range []string{"surface issue_panel", "hook trigger ui", "hook transport http", "resource skill"} {
		if !containsString(unavailable.Missing, want) {
			t.Fatalf("missing = %v, want it to include %q", unavailable.Missing, want)
		}
	}

	full := Capabilities{
		SurfaceTypes:  map[string]bool{SurfaceIssuePanel: true, SurfaceSidebarPanel: true, SurfaceModal: true},
		HookTriggers:  map[string]bool{TriggerUI: true, TriggerManual: true, TriggerAgent: true, TriggerEvent: true},
		HookTransport: map[string]bool{TransportHTTP: true, TransportMCP: true},
		ResourceTypes: map[string]bool{ResourceSkill: true},
	}
	if err := manifest.CheckCapabilities(full); err != nil {
		t.Fatalf("CheckCapabilities with full host support: %v", err)
	}
}

func asCapabilityError(err error, target **ErrCapabilityUnavailable) bool {
	candidate, ok := err.(*ErrCapabilityUnavailable)
	if ok {
		*target = candidate
	}
	return ok
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
