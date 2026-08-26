package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

const testManifestJSON = `{
  "manifest_version": 1,
  "key": "com.example.hello",
  "name": "Hello Panel",
  "version": "1.0.0",
  "author": { "name": "example" },
  "scopes": ["issues:read", "comments:write", "storage:user"],
  "config": {
    "repo": { "type": "string", "label": "Repo", "required": true },
    "count": { "type": "number", "label": "Count" },
    "verbose": { "type": "bool", "label": "Verbose" },
    "mode": { "type": "enum", "label": "Mode", "options": ["fast", "thorough"] },
    "token": { "type": "secret", "label": "Token" }
  },
  "contributes": {
    "surfaces": [{ "key": "hello", "type": "issue_panel", "name": "Hello", "entry": "ui/main.js" }]
  }
}`

func testManifest(t *testing.T) plugincontract.Manifest {
	t.Helper()
	manifest, _, err := plugincontract.ParseManifest([]byte(testManifestJSON))
	if err != nil {
		t.Fatalf("parse test manifest: %v", err)
	}
	return manifest
}

func pluginErrKind(t *testing.T, err error) PluginErrorKind {
	t.Helper()
	var pluginErr *PluginError
	if !errors.As(err, &pluginErr) {
		t.Fatalf("error %v is not a *PluginError", err)
	}
	return pluginErr.Kind
}

func TestRequireExactScopes(t *testing.T) {
	manifest := testManifest(t)

	if err := requireExactScopes(manifest.Scopes, []string{"comments:write", "storage:user", "issues:read"}); err != nil {
		t.Fatalf("exact scopes in a different order should be accepted: %v", err)
	}
	// Partial consent would install a plugin that silently fails on the scope
	// the administrator withheld, so it is a conflict rather than a warning.
	if err := requireExactScopes(manifest.Scopes, []string{"issues:read"}); err == nil {
		t.Fatal("partial consent was accepted")
	} else if kind := pluginErrKind(t, err); kind != PluginErrorConflict {
		t.Fatalf("partial consent kind = %q, want %q", kind, PluginErrorConflict)
	}
	// Granting more than the manifest asks for would hand out access the
	// consent screen never justified.
	if err := requireExactScopes(manifest.Scopes, []string{"issues:read", "comments:write", "issues:write"}); err == nil {
		t.Fatal("consent to an unrequested scope was accepted")
	}
}

func TestNormalizeConfigValue(t *testing.T) {
	manifest := testManifest(t)
	field := func(key string) plugincontract.ConfigField {
		got, ok := manifest.Config.Field(key)
		if !ok {
			t.Fatalf("config field %q missing from the test manifest", key)
		}
		return got
	}

	if value, err := normalizeConfigValue(field("repo"), "multica-ai/multica"); err != nil || value != "multica-ai/multica" {
		t.Fatalf("string field = (%v, %v)", value, err)
	}
	if value, err := normalizeConfigValue(field("count"), float64(3)); err != nil || value != float64(3) {
		t.Fatalf("number field = (%v, %v)", value, err)
	}
	if value, err := normalizeConfigValue(field("verbose"), true); err != nil || value != true {
		t.Fatalf("bool field = (%v, %v)", value, err)
	}
	if value, err := normalizeConfigValue(field("mode"), "fast"); err != nil || value != "fast" {
		t.Fatalf("enum field = (%v, %v)", value, err)
	}

	rejected := []struct {
		name  string
		key   string
		value any
	}{
		{"string given a number", "repo", float64(1)},
		{"number given a string", "count", "3"},
		{"bool given a string", "verbose", "true"},
		{"enum outside its options", "mode", "sloppy"},
		{"string over the length cap", "repo", strings.Repeat("x", 4097)},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normalizeConfigValue(field(tc.key), tc.value); err == nil {
				t.Fatalf("normalizeConfigValue accepted %v", tc.value)
			} else if kind := pluginErrKind(t, err); kind != PluginErrorInvalid {
				t.Fatalf("kind = %q, want %q", kind, PluginErrorInvalid)
			}
		})
	}
}

func TestPruneConfigDropsFieldsTheNewManifestRemoved(t *testing.T) {
	manifest := testManifest(t)
	pruned := pruneConfig([]byte(`{"repo":"a","gone":"b"}`), manifest)

	var values map[string]any
	if err := json.Unmarshal(pruned, &values); err != nil {
		t.Fatalf("decode pruned config: %v", err)
	}
	if values["repo"] != "a" {
		t.Fatalf("pruneConfig dropped a field the manifest still declares: %s", pruned)
	}
	if _, present := values["gone"]; present {
		t.Fatalf("pruneConfig kept an unreachable field: %s", pruned)
	}
	if got := string(pruneConfig(nil, manifest)); got != "{}" {
		t.Fatalf("pruneConfig(nil) = %q, want %q", got, "{}")
	}
}

func TestAddedScopesReportsOnlyNewGrants(t *testing.T) {
	added := addedScopes([]string{"issues:read"}, []string{"issues:read", "comments:write"})
	if len(added) != 1 || added[0] != "comments:write" {
		t.Fatalf("addedScopes = %v", added)
	}
	if added := addedScopes([]string{"issues:read", "comments:write"}, []string{"issues:read"}); len(added) != 0 {
		t.Fatalf("a narrowed scope set must not report additions, got %v", added)
	}
}

func TestParseInstallationManifestReadsTheConsentedSnapshot(t *testing.T) {
	_, canonical, err := plugincontract.ParseManifest([]byte(testManifestJSON))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	manifest, err := ParseInstallationManifest(db.PluginInstallation{Manifest: canonical})
	if err != nil {
		t.Fatalf("ParseInstallationManifest: %v", err)
	}
	if manifest.Key != "com.example.hello" || manifest.Config.Len() != 5 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if _, err := ParseInstallationManifest(db.PluginInstallation{Manifest: []byte("not json")}); err == nil {
		t.Fatal("ParseInstallationManifest accepted a corrupt snapshot")
	}
}

func TestConfigFieldsForManifestKeepsDeclarationOrder(t *testing.T) {
	fields := ConfigFieldsForManifest(testManifest(t))
	want := []string{"repo", "count", "verbose", "mode", "token"}
	if len(fields) != len(want) {
		t.Fatalf("config fields = %d, want %d", len(fields), len(want))
	}
	for i, key := range want {
		if fields[i].Key != key {
			t.Fatalf("field %d = %q, want %q", i, fields[i].Key, key)
		}
	}
}

// readLocalFile is the only path left that touches the filesystem on behalf of
// an API caller. The directory name arrives over HTTP, so it must not be able to
// escape MULTICA_PLUGIN_DIR or name a dotfile path.
func TestReadLocalFileStaysInsideThePluginDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "hello"), 0o755); err != nil {
		t.Fatalf("create local plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "hello", plugincontract.ManifestFilename), []byte(testManifestJSON), 0o644); err != nil {
		t.Fatalf("write local manifest: %v", err)
	}
	// A file the caller must not be able to reach by naming a traversing entry.
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatalf("write sibling file: %v", err)
	}
	service := &PluginService{LocalDir: root}

	raw, err := service.readLocalFile("hello", plugincontract.ManifestFilename)
	if err != nil {
		t.Fatalf("readLocalFile: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("readLocalFile returned nothing")
	}

	if _, err := service.readLocalFile("hello", "missing.js"); err == nil {
		t.Fatal("readLocalFile accepted a missing entry")
	} else if kind := pluginErrKind(t, err); kind != PluginErrorNotFound {
		t.Fatalf("missing entry kind = %q, want %q", kind, PluginErrorNotFound)
	}

	for _, name := range []string{"../secrets", "nested/plugin", ".hidden", ""} {
		if _, err := service.readLocalFile(name, plugincontract.ManifestFilename); err == nil {
			t.Fatalf("readLocalFile accepted local source %q", name)
		}
	}
	if _, err := service.readLocalFile("hello", "../secret.txt"); err == nil {
		t.Fatal("readLocalFile followed a traversing entry out of the plugin directory")
	}

	disabled := &PluginService{}
	if _, err := disabled.readLocalFile("hello", plugincontract.ManifestFilename); err == nil {
		t.Fatal("local sources must require MULTICA_PLUGIN_DIR")
	}
}

// A published version's manifest is read back from the row, never re-parsed
// from anything the author still controls.
func TestManifestForVersionReadsTheStoredSnapshot(t *testing.T) {
	_, canonical, err := plugincontract.ParseManifest([]byte(testManifestJSON))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	manifest, raw, err := ManifestForVersion(db.PluginPackageVersion{Manifest: canonical})
	if err != nil {
		t.Fatalf("ManifestForVersion: %v", err)
	}
	if manifest.Key != "com.example.hello" || len(raw) == 0 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if _, _, err := ManifestForVersion(db.PluginPackageVersion{Manifest: []byte("not json")}); err == nil {
		t.Fatal("ManifestForVersion accepted a corrupt snapshot")
	}
}

// Publishing the same version twice is a conflict for an upload — that
// collision IS immutability — but a development publish takes a build suffix,
// because re-publishing an unchanged version is what editing a file looks like.
func TestResolvePublishVersion(t *testing.T) {
	existing := []db.PluginPackageVersion{
		{Version: "1.0.0"},
		{Version: "1.0.0+dev.1"},
		{Version: "1.0.0+dev.2"},
	}

	if got, err := resolvePublishVersion("1.0.0", existing, false); err != nil || got != "1.0.0" {
		t.Fatalf("upload version = %q, %v; want the manifest's own version so the unique index refuses it", got, err)
	}
	if got, err := resolvePublishVersion("1.0.0", existing, true); err != nil || got != "1.0.0+dev.3" {
		t.Fatalf("dev version = %q, %v; want 1.0.0+dev.3", got, err)
	}
	if got, err := resolvePublishVersion("2.0.0", existing, true); err != nil || got != "2.0.0" {
		t.Fatalf("unused dev version = %q, %v; want 2.0.0 with no suffix", got, err)
	}
	long := "1.0.0+" + strings.Repeat("a", plugincontract.MaxVersionLength-6)
	if _, err := resolvePublishVersion(long, []db.PluginPackageVersion{{Version: long}}, true); err == nil {
		t.Fatal("a version with no room for a dev suffix must be refused, not silently truncated")
	}
}

func TestResolveStorageScope(t *testing.T) {
	workspaceID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	userID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	if got, err := ResolveStorageScope(PluginStorageWorkspace, workspaceID, userID); err != nil || got != workspaceID {
		t.Fatalf("workspace scope = (%v, %v)", got, err)
	}
	if got, err := ResolveStorageScope(PluginStorageUser, workspaceID, userID); err != nil || got != userID {
		t.Fatalf("user scope = (%v, %v)", got, err)
	}
	if _, err := ResolveStorageScope("global", workspaceID, userID); err == nil {
		t.Fatal("ResolveStorageScope accepted an undefined scope")
	}
}

func TestValidateStorageKeyEnforcesTheKeyQuota(t *testing.T) {
	if err := validateStorageKey(""); err == nil {
		t.Fatal("validateStorageKey accepted an empty key")
	}
	if err := validateStorageKey(strings.Repeat("k", MaxPluginStorageKeyBytes)); err != nil {
		t.Fatalf("a key exactly at the limit must be accepted: %v", err)
	}
	err := validateStorageKey(strings.Repeat("k", MaxPluginStorageKeyBytes+1))
	if err == nil {
		t.Fatal("validateStorageKey accepted an oversized key")
	}
	if kind := pluginErrKind(t, err); kind != PluginErrorQuota {
		t.Fatalf("oversized key kind = %q, want %q", kind, PluginErrorQuota)
	}
}

func TestCapabilityMessageNamesTheMissingCapabilities(t *testing.T) {
	// Asserted against an explicitly empty host set, not the shipped one: this
	// covers the message, and tying it to whatever HostCapabilities happens to
	// enable today makes every future capability flip fail a test that has
	// nothing to say about the flip.
	manifest := testManifest(t)
	err := manifest.CheckCapabilities(plugincontract.Capabilities{})
	if err == nil {
		t.Fatal("an empty host set must reject every contribution")
	}
	message := capabilityMessage(err)
	if !strings.Contains(message, "surface issue_panel") {
		t.Fatalf("capabilityMessage = %q, want it to name the missing capability", message)
	}
}

// Binds every shipped capability to the host code that actually runs it.
//
// The gate is only meaningful if it tracks what renders. A capability enabled
// here with no mount point installs successfully and then never appears — the
// exact silent failure the gate exists to prevent — and nothing else in the
// build would notice, because the manifest is valid and the install succeeds.
//
// Each line names the host code that makes it true. Flipping one on means
// changing this file too, in the same commit as its runtime.
func TestShippedHostCapabilitiesRunTheSurfacesTheHostMounts(t *testing.T) {
	shipped := plugincontract.HostCapabilities()

	// packages/views/plugins/plugin-panel-section.tsx
	if !shipped.SurfaceTypes[plugincontract.SurfaceIssuePanel] {
		t.Fatal("issue_panel is mounted by PluginPanelSection and must be shipped")
	}
	// packages/views/plugins/plugin-modal-surface.tsx, opened from the issue
	// actions menu by a person — never on the plugin's own initiative.
	if !shipped.SurfaceTypes[plugincontract.SurfaceModal] {
		t.Fatal("modal is mounted by PluginModalSurface and must be shipped")
	}
	// No host location exists for this one. Enabling it would install surfaces
	// that never render.
	if shipped.SurfaceTypes[plugincontract.SurfaceSidebarPanel] {
		t.Fatal("sidebar_panel has no mount point yet; enabling it would install surfaces that never render")
	}

	// handler.InvokePluginHook serves ui and manual; service.PluginEventDispatcher
	// serves event off the internal bus; daemon/plugin_hook_mcp.go renders
	// agent-trigger hooks as MCP tools.
	for _, trigger := range []string{
		plugincontract.TriggerUI, plugincontract.TriggerManual,
		plugincontract.TriggerEvent, plugincontract.TriggerAgent,
	} {
		if !shipped.HookTriggers[trigger] {
			t.Fatalf("hook trigger %q has a host call site and must be shipped", trigger)
		}
	}

	// service.callHookEndpoint speaks http; service.AgentMCPConnections turns an
	// approved mcp hook into a broker connection.
	for _, transport := range []string{plugincontract.TransportHTTP, plugincontract.TransportMCP} {
		if !shipped.HookTransport[transport] {
			t.Fatalf("hook transport %q has an implementation and must be shipped", transport)
		}
	}

	// service.InstallSkillResources writes these into the skill table, and
	// Uninstall removes them again.
	if !shipped.ResourceTypes[plugincontract.ResourceSkill] {
		t.Fatal("skill resources are installed by InstallSkillResources and must be shipped")
	}
}

func TestEnforceStorageQuota(t *testing.T) {
	// Canonical layer for the three storage limits. SetStorageValue only wires
	// the usage query to this function; plugin_storage_db_test.go covers the
	// query's own semantics.
	cases := []struct {
		name       string
		usage      db.GetPluginStorageUsageRow
		valueBytes int
		wantKind   PluginErrorKind
	}{
		{"empty scope", db.GetPluginStorageUsageRow{}, 10, ""},
		{"value exactly at the cap", db.GetPluginStorageUsageRow{}, MaxPluginStorageValueBytes, ""},
		{"value one byte over the cap", db.GetPluginStorageUsageRow{}, MaxPluginStorageValueBytes + 1, PluginErrorQuota},
		{
			"last free key slot",
			db.GetPluginStorageUsageRow{KeyCount: MaxPluginStorageKeys - 1},
			1, "",
		},
		{
			"key slots exhausted",
			db.GetPluginStorageUsageRow{KeyCount: MaxPluginStorageKeys},
			1, PluginErrorQuota,
		},
		{
			"total budget exactly filled",
			db.GetPluginStorageUsageRow{KeyCount: 1, TotalBytes: MaxPluginStorageTotalBytes - 10},
			10, "",
		},
		{
			"total budget exceeded by one byte",
			db.GetPluginStorageUsageRow{KeyCount: 1, TotalBytes: MaxPluginStorageTotalBytes - 10},
			11, PluginErrorQuota,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := EnforceStorageQuota(tc.usage, tc.valueBytes)
			if tc.wantKind == "" {
				if err != nil {
					t.Fatalf("EnforceStorageQuota = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("EnforceStorageQuota accepted a write over quota")
			}
			if kind := pluginErrKind(t, err); kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q", kind, tc.wantKind)
			}
		})
	}
}

func TestEnforceStorageQuotaCountsBytesNotCharacters(t *testing.T) {
	// A 4-byte emoji is one character. If any bound counted characters the
	// effective budget would be up to 4x what the limit advertises.
	emoji := "🙂"
	if len(emoji) != 4 {
		t.Fatalf("fixture is not 4 bytes: %d", len(emoji))
	}
	oversized := strings.Repeat(emoji, MaxPluginStorageValueBytes/4+1)
	if err := EnforceStorageQuota(db.GetPluginStorageUsageRow{}, len(oversized)); err == nil {
		t.Fatal("a value over the byte cap was accepted")
	}
	if len([]rune(oversized)) >= MaxPluginStorageValueBytes {
		t.Fatal("fixture does not distinguish characters from bytes")
	}
}

func TestOrphanedSecretFieldsAreDetectedByType(t *testing.T) {
	// The pure half of upgrade-time secret pruning: which stored keys the new
	// manifest no longer declares as a secret. A field that changed type away
	// from secret counts as orphaned — its ciphertext is unreachable too.
	manifest := testManifest(t)
	for _, tc := range []struct {
		key  string
		want bool
	}{
		{"token", false},
		{"repo", true},
		{"removed_field", true},
	} {
		field, ok := manifest.Config.Field(tc.key)
		orphaned := !ok || field.Type != plugincontract.ConfigSecret
		if orphaned != tc.want {
			t.Fatalf("%q orphaned = %v, want %v", tc.key, orphaned, tc.want)
		}
	}
}
