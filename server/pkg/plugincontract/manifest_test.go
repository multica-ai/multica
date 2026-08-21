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

// The gate's job, stated without naming today's configuration.
//
// An earlier version listed the specific contributions HostCapabilities had not
// shipped yet, so every staged flip turned this red with a message about the
// flip rather than about the gate. What has to stay true is the mechanism:
// everything the host cannot run is reported, everything it can run is not, and
// all of it arrives at once.
func TestCheckCapabilitiesReportsEveryUnavailableContribution(t *testing.T) {
	manifest, _, err := ParseManifest([]byte(validManifest))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	// Against a host that supports nothing, every declared contribution in the
	// fixture must be named — not the first one found.
	err = manifest.CheckCapabilities(Capabilities{})
	if err == nil {
		t.Fatal("a host with no capabilities accepted contributions it cannot run")
	}
	var unavailable *ErrCapabilityUnavailable
	if !asCapabilityError(err, &unavailable) {
		t.Fatalf("error type = %T, want *ErrCapabilityUnavailable", err)
	}
	wantAll := []string{}
	for _, surface := range manifest.Contributes.Surfaces {
		wantAll = append(wantAll, "surface "+surface.Type)
	}
	for _, hook := range manifest.Contributes.Hooks {
		for _, trigger := range hook.Triggers {
			wantAll = append(wantAll, "hook trigger "+trigger)
		}
		wantAll = append(wantAll, "hook transport "+hook.Transport.Type)
	}
	for _, resource := range manifest.Contributes.Resources {
		wantAll = append(wantAll, "resource "+resource.Type)
	}
	for _, want := range wantAll {
		if !containsString(unavailable.Missing, want) {
			t.Fatalf("missing = %v, want it to include %q — every gap must be reported at once, not one install at a time", unavailable.Missing, want)
		}
	}

	// Against the real host set: whatever is shipped must NOT be reported, and
	// whatever is not shipped must be. Derived from HostCapabilities rather than
	// restated, so a flip changes one place and this keeps testing the gate.
	host := HostCapabilities()
	hostErr := manifest.CheckCapabilities(host)
	reported := []string{}
	if hostErr != nil {
		var hostUnavailable *ErrCapabilityUnavailable
		if !asCapabilityError(hostErr, &hostUnavailable) {
			t.Fatalf("error type = %T, want *ErrCapabilityUnavailable", hostErr)
		}
		reported = hostUnavailable.Missing
	}
	for _, surface := range manifest.Contributes.Surfaces {
		assertGateAgrees(t, reported, "surface "+surface.Type, host.SurfaceTypes[surface.Type])
	}
	for _, hook := range manifest.Contributes.Hooks {
		for _, trigger := range hook.Triggers {
			assertGateAgrees(t, reported, "hook trigger "+trigger, host.HookTriggers[trigger])
		}
		assertGateAgrees(t, reported, "hook transport "+hook.Transport.Type, host.HookTransport[hook.Transport.Type])
	}
	for _, resource := range manifest.Contributes.Resources {
		assertGateAgrees(t, reported, "resource "+resource.Type, host.ResourceTypes[resource.Type])
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

// assertGateAgrees pins the gate to the host set in both directions: a shipped
// capability reported as missing would fail every install of a plugin the host
// can actually run, and an unshipped one left unreported would install a
// contribution that silently never fires.
func assertGateAgrees(t *testing.T, reported []string, name string, shipped bool) {
	t.Helper()
	if shipped && containsString(reported, name) {
		t.Fatalf("%q is shipped by this host but was reported unavailable", name)
	}
	if !shipped && !containsString(reported, name) {
		t.Fatalf("%q is NOT shipped by this host but was not reported — it would install and never fire", name)
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

// An event subscription delivers the same content the Action API would have
// required a scope to read: issue.* carries the description, comment.created
// carries the body. Without this check, subscribing was a way to receive what
// reading was never granted.
func TestEventSubscriptionRequiresTheMatchingReadScope(t *testing.T) {
	manifest := func(scopes, events string) []byte {
		return []byte(`{
			"manifest_version": 1,
			"key": "com.example.events",
			"name": "Events",
			"description": "d",
			"version": "1.0.0",
			"author": {"name": "example"},
			"scopes": ` + scopes + `,
			"contributes": {"hooks": [{
				"key": "watch",
				"name": "Watch",
				"description": "Watch things happen.",
				"triggers": ["event"],
				"events": ` + events + `,
				"transport": {"type": "http", "url": "https://example.com/hooks/watch"}
			}]}
		}`)
	}

	for name, tc := range map[string]struct {
		scopes  string
		events  string
		wantErr bool
	}{
		"issue event without issues:read":     {`["net:example.com"]`, `["issue.created"]`, true},
		"issue event with issues:read":        {`["issues:read", "net:example.com"]`, `["issue.created"]`, false},
		"comment event without comments:read": {`["issues:read", "net:example.com"]`, `["comment.created"]`, true},
		"comment event with comments:read":    {`["comments:read", "net:example.com"]`, `["comment.created"]`, false},
		"task event without tasks:read":       {`["net:example.com"]`, `["task.failed"]`, true},
		"task event with tasks:read":          {`["tasks:read", "net:example.com"]`, `["task.failed"]`, false},
		"one of several events unscoped": {
			`["issues:read", "net:example.com"]`, `["issue.created", "comment.created"]`, true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := ParseManifest(manifest(tc.scopes, tc.events))
			if tc.wantErr && err == nil {
				t.Fatal("subscribing to content the manifest may not read must be refused at install")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("a properly scoped subscription must parse: %v", err)
			}
		})
	}
}
