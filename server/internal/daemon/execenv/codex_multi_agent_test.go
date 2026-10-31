package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// parseTOML decodes the given content with a spec-strict parser. Codex's
// `toml-rs` follows the same strict semantics, so a config that parses
// here will load in Codex.
func parseTOML(t *testing.T, content string) map[string]any {
	t.Helper()
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("expected valid TOML, got parser error: %v\n--- file ---\n%s", err, content)
	}
	return parsed
}

// requireMultiAgentDisabled asserts that both feature flags and agents.enabled
// are false, including the V2 flag that takes precedence over agents.enabled.
func requireMultiAgentDisabled(t *testing.T, parsed map[string]any) {
	t.Helper()
	requireAgentsDisabled(t, parsed)
	features, ok := parsed["features"].(map[string]any)
	if !ok {
		t.Fatalf("expected `features` table in parsed config, got: %#v", parsed["features"])
	}
	for _, key := range []string{"multi_agent", "multi_agent_v2"} {
		v, ok := features[key].(bool)
		if !ok {
			t.Fatalf("expected features.%s to be a bool, got: %#v", key, features[key])
		}
		if v {
			t.Errorf("expected features.%s = false, got true", key)
		}
	}
}

// requireAgentsDisabled asserts that the parsed config has the explicit
// agents.enabled override set to false.
func requireAgentsDisabled(t *testing.T, parsed map[string]any) {
	t.Helper()
	agents, ok := parsed["agents"].(map[string]any)
	if !ok {
		t.Fatalf("expected `agents` table in parsed config, got: %#v", parsed["agents"])
	}
	v, ok := agents["enabled"].(bool)
	if !ok {
		t.Fatalf("expected agents.enabled to be a bool, got: %#v", agents["enabled"])
	}
	if v {
		t.Errorf("expected agents.enabled = false, got true")
	}
}

func TestStripUserMultiAgentDirectives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "drops top-level dotted-key form",
			in: `model = "o3"
features.multi_agent = true

[profiles.default]
model = "o3"
`,
			want: `model = "o3"

[profiles.default]
model = "o3"
`,
		},
		{
			name: "drops top-level dotted-key form with whitespace",
			in: `model = "o3"
features . multi_agent = true

[profiles.default]
model = "o3"
`,
			want: `model = "o3"

[profiles.default]
model = "o3"
`,
		},
		{
			name: "drops multi_agent inside [features] table",
			in: `[features]
multi_agent = true
experimental_foo = true

[profiles.default]
model = "o3"
`,
			want: `[features]
experimental_foo = true

[profiles.default]
model = "o3"
`,
		},
		{
			name: "drops multi_agent inside [features] table with inline comment",
			in: `[features] # user feature flags
multi_agent = true
experimental_foo = true
`,
			want: `[features] # user feature flags
experimental_foo = true
`,
		},
		{
			name: "drops multi_agent inside spaced [ features ] table",
			in: `[ features ]
multi_agent = true
experimental_foo = true
`,
			want: `[ features ]
experimental_foo = true
`,
		},
		{
			name: "preserves multi_agent under unrelated table",
			in: `[profiles.experimental]
multi_agent = true
`,
			want: `[profiles.experimental]
multi_agent = true
`,
		},
		{
			name: "preserves multi_agent under nested [features.experimental]",
			in: `[features.experimental]
multi_agent = true
`,
			want: `[features.experimental]
multi_agent = true
`,
		},
		{
			name: "no multi_agent — content unchanged",
			in: `model = "o3"

[profiles.default]
model = "o3"
`,
			want: `model = "o3"

[profiles.default]
model = "o3"
`,
		},
		{
			name: "drops both forms simultaneously",
			in: `features.multi_agent = true

[features]
multi_agent = false
something_else = "keep"
`,
			want: `
[features]
something_else = "keep"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stripUserMultiAgentDirectives(tt.in)
			if got != tt.want {
				t.Errorf("stripUserMultiAgentDirectives mismatch\n--- got ---\n%s\n--- want ---\n%s", got, tt.want)
			}
		})
	}
}

func TestEnsureCodexMultiAgentConfigEmptyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "features.multi_agent = false") {
		t.Errorf("expected managed block to set features.multi_agent = false at root, got:\n%s", got)
	}
	if !strings.Contains(got, multicaMultiAgentBeginMarker) {
		t.Errorf("expected begin marker, got:\n%s", got)
	}
	if !strings.Contains(got, multicaMultiAgentEndMarker) {
		t.Errorf("expected end marker, got:\n%s", got)
	}
	requireMultiAgentDisabled(t, parseTOML(t, got))
}

func TestEnsureCodexMultiAgentConfigDottedKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `model = "o3"
features.multi_agent = true
features.multi_agent_v2 = true

[profiles.default]
model = "o3"
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)
	if strings.Contains(got, "features.multi_agent = true") {
		t.Errorf("expected user features.multi_agent = true to be stripped, got:\n%s", got)
	}
	if !strings.Contains(got, "features.multi_agent = false") {
		t.Errorf("expected managed features.multi_agent = false at root, got:\n%s", got)
	}
	if !strings.Contains(got, `[profiles.default]`) || !strings.Contains(got, `model = "o3"`) {
		t.Errorf("expected unrelated content preserved, got:\n%s", got)
	}
	requireMultiAgentDisabled(t, parseTOML(t, got))
}

func TestEnsureCodexMultiAgentConfigDottedKeyWithWhitespace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `model = "o3"
features . multi_agent = true
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)
	if strings.Contains(got, "features . multi_agent = true") {
		t.Errorf("expected user features . multi_agent = true to be stripped, got:\n%s", got)
	}
	requireMultiAgentDisabled(t, parseTOML(t, got))
}

func TestEnsureCodexMultiAgentConfigFeaturesTable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `[features]
multi_agent = true
experimental_thinking = true

[profiles.default]
model = "o3"
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)

	// User's `multi_agent = true` must be gone, our managed `multi_agent = false`
	// must be inside the [features] table (NOT at the root as a dotted key,
	// which would redefine the table and break the strict TOML parser).
	if strings.Contains(got, "multi_agent = true") {
		t.Errorf("expected user multi_agent = true to be stripped, got:\n%s", got)
	}
	if strings.Contains(got, "features.multi_agent = false") {
		t.Errorf("managed block must NOT use root dotted-key form when [features] table exists (would redefine the table); got:\n%s", got)
	}
	if !strings.Contains(got, "[features]") {
		t.Errorf("expected [features] header preserved, got:\n%s", got)
	}
	if !strings.Contains(got, "experimental_thinking = true") {
		t.Errorf("expected sibling features.* keys preserved, got:\n%s", got)
	}

	// Output must parse as valid TOML and have features.multi_agent = false.
	requireMultiAgentDisabled(t, parseTOML(t, got))
}

func TestEnsureCodexMultiAgentConfigFeaturesTableHeaderVariants(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"inline_comment": `[features] # user feature flags
multi_agent = true
experimental_thinking = true
`,
		"spaced_header": `[ features ]
multi_agent = true
experimental_thinking = true
`,
	}

	for name, original := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.toml")
			if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
				t.Fatalf("ensureCodexMultiAgentConfig failed: %v", err)
			}

			data, _ := os.ReadFile(configPath)
			got := string(data)
			if strings.Contains(got, "features.multi_agent = false") {
				t.Errorf("managed block must NOT use root dotted-key form when [features] table exists; got:\n%s", got)
			}
			if strings.Contains(got, "multi_agent = true") {
				t.Errorf("expected user multi_agent = true to be stripped, got:\n%s", got)
			}

			parsed := parseTOML(t, got)
			requireMultiAgentDisabled(t, parsed)
			features := parsed["features"].(map[string]any)
			if v, _ := features["experimental_thinking"].(bool); !v {
				t.Errorf("expected user's features.experimental_thinking preserved, got %v", features["experimental_thinking"])
			}
		})
	}
}

func TestEnsureCodexMultiAgentConfigFeaturesTableEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `[features]
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	requireMultiAgentDisabled(t, parseTOML(t, string(data)))
}

func TestEnsureCodexMultiAgentConfigFeaturesSubtableOnly(t *testing.T) {
	t.Parallel()

	// User has [features.experimental] but no bare [features] header. The
	// dotted-key form at root is fine — both implicitly define `features`,
	// neither defines `[features]` explicitly, so no redefinition.
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `[features.experimental]
thinking = true
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)
	if !strings.Contains(got, "features.multi_agent = false") {
		t.Errorf("expected root dotted-key form when only sub-tables exist, got:\n%s", got)
	}

	parsed := parseTOML(t, got)
	requireMultiAgentDisabled(t, parsed)
	features := parsed["features"].(map[string]any)
	exp, _ := features["experimental"].(map[string]any)
	if v, _ := exp["thinking"].(bool); !v {
		t.Errorf("expected features.experimental.thinking preserved, got: %#v", exp)
	}
}

func TestEnsureCodexMultiAgentConfigIdempotent(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"root_form": `model = "o3"
features.multi_agent = true
features.multi_agent_v2 = true
`,
		"in_features_table": `[features]
multi_agent = true
multi_agent_v2 = true
experimental_thinking = true
`,
		"in_features_table_with_other_keys": `[features]
experimental_thinking = true

[profiles.default]
model = "o3"
`,
		"in_agents_table": `[agents]
enabled = true
max_threads = 4

[agents.researcher]
description = "Research-focused role."
`,
		"features_and_agents_tables": `[features]
multi_agent = true

[agents]
max_threads = 4
`,
	}
	for name, original := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.toml")
			if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
				t.Fatalf("first run failed: %v", err)
			}
			first, _ := os.ReadFile(configPath)
			infoFirst, _ := os.Stat(configPath)

			if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
				t.Fatalf("second run failed: %v", err)
			}
			second, _ := os.ReadFile(configPath)
			infoSecond, _ := os.Stat(configPath)

			if string(first) != string(second) {
				t.Errorf("expected idempotent rewrite\n--- first ---\n%s\n--- second ---\n%s", first, second)
			}
			if !infoSecond.ModTime().Equal(infoFirst.ModTime()) {
				t.Errorf("expected no rewrite on second pass (file was touched)")
			}
			// Final output must parse as valid TOML.
			requireMultiAgentDisabled(t, parseTOML(t, string(second)))
		})
	}
}

func TestEnsureCodexMultiAgentConfigEscapeHatch(t *testing.T) {
	// Cannot run in parallel: mutates process env.
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `model = "o3"
features.multi_agent = true
features.multi_agent_v2 = true
agents.enabled = true
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Setenv(MulticaCodexMultiAgentEnv, "1")

	if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)
	if got != original {
		t.Errorf("expected file untouched when escape hatch set\n--- got ---\n%s\n--- want ---\n%s", got, original)
	}
}

func TestCodexMultiAgentEnabledTruthy(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "yes", "On"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(MulticaCodexMultiAgentEnv, v)
			if !codexMultiAgentEnabled() {
				t.Errorf("expected %q to be truthy", v)
			}
		})
	}
}

func TestCodexMultiAgentEnabledFalsy(t *testing.T) {
	for _, v := range []string{"", "0", "false", "no", "off", "anything else"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(MulticaCodexMultiAgentEnv, v)
			if codexMultiAgentEnabled() {
				t.Errorf("expected %q to be falsy", v)
			}
		})
	}
}

func TestEnsureCodexMultiAgentConfigCoexistsWithSandboxBlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `model = "o3"
features.multi_agent = true
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	policy := codexSandboxPolicy{Mode: "workspace-write", NetworkAccess: true, Reason: "test"}
	if err := ensureCodexSandboxConfig(configPath, policy, "0.121.0", nil); err != nil {
		t.Fatalf("ensureCodexSandboxConfig failed: %v", err)
	}
	if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)
	if !strings.Contains(got, multicaManagedBeginMarker) {
		t.Errorf("expected sandbox managed block, got:\n%s", got)
	}
	if !strings.Contains(got, multicaMultiAgentBeginMarker) {
		t.Errorf("expected multi-agent managed block, got:\n%s", got)
	}
	if strings.Contains(got, "features.multi_agent = true") {
		t.Errorf("expected user features.multi_agent = true to be stripped, got:\n%s", got)
	}

	// File must parse as valid TOML and have multi_agent disabled.
	requireMultiAgentDisabled(t, parseTOML(t, got))

	// Re-running both should be idempotent.
	if err := ensureCodexSandboxConfig(configPath, policy, "0.121.0", nil); err != nil {
		t.Fatalf("ensureCodexSandboxConfig (rerun) failed: %v", err)
	}
	if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig (rerun) failed: %v", err)
	}
	dataAfter, _ := os.ReadFile(configPath)
	if string(dataAfter) != got {
		t.Errorf("expected idempotent combined rewrite\n--- first ---\n%s\n--- second ---\n%s", got, dataAfter)
	}
}

// Regression for PR #1845 review: when the user's config has a `[features]`
// table, naively writing `features.multi_agent = false` at the TOML root
// implicitly redefines the same table. The strict TOML parser used by
// Codex (`toml-rs`) rejects that with `table 'features' already exists`.
func TestRegressionFeaturesTableProducesValidTOML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `[features]
experimental_thinking = true
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	parsed := parseTOML(t, string(data))
	requireMultiAgentDisabled(t, parsed)

	features := parsed["features"].(map[string]any)
	if v, _ := features["experimental_thinking"].(bool); !v {
		t.Errorf("expected user's features.experimental_thinking preserved, got %v", features["experimental_thinking"])
	}
}

func TestEnsureCodexMultiAgentConfigEmptyFileWritesAgentsDisabled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)
	for _, want := range []string{multicaAgentsBeginMarker, "agents.enabled = false", multicaAgentsEndMarker} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output, got:\n%s", want, got)
		}
	}
	requireMultiAgentDisabled(t, parseTOML(t, got))
}

func TestEnsureCodexMultiAgentConfigAgentsTable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `[agents]
enabled = true
max_threads = 4

[agents.researcher]
description = "Research-focused role."
enabled = true
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)
	if strings.Contains(got, "agents.enabled = false") {
		t.Errorf("managed block must NOT use root dotted-key form when [agents] table exists; got:\n%s", got)
	}

	parsed := parseTOML(t, got)
	requireMultiAgentDisabled(t, parsed)
	agents := parsed["agents"].(map[string]any)
	if v, _ := agents["max_threads"].(int64); v != 4 {
		t.Errorf("expected agents.max_threads preserved, got %#v", agents["max_threads"])
	}
	role, _ := agents["researcher"].(map[string]any)
	if v, _ := role["enabled"].(bool); !v {
		t.Errorf("expected [agents.researcher] keys preserved, got %#v", role)
	}
}

func TestEnsureCodexMultiAgentConfigDottedAgentsEnabled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `model = "o3"
agents . enabled = true
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)
	if strings.Contains(got, "agents . enabled = true") {
		t.Errorf("expected user agents.enabled = true to be stripped, got:\n%s", got)
	}
	requireMultiAgentDisabled(t, parseTOML(t, got))
}

func TestEnsureCodexMultiAgentConfigAgentsRoleSubtableOnly(t *testing.T) {
	t.Parallel()

	// A role sub-table implicitly creates `agents` without a bare `[agents]`
	// header, so the root dotted-key form is still valid TOML.
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `[agents.researcher]
description = "Research-focused role."
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)
	if !strings.Contains(got, "agents.enabled = false") {
		t.Errorf("expected root dotted-key form when only role sub-tables exist, got:\n%s", got)
	}
	parsed := parseTOML(t, got)
	requireMultiAgentDisabled(t, parsed)
	role, _ := parsed["agents"].(map[string]any)["researcher"].(map[string]any)
	if v, _ := role["description"].(string); v != "Research-focused role." {
		t.Errorf("expected [agents.researcher] preserved, got %#v", role)
	}
}

// Configs written by daemons before agents.enabled was managed carry only
// the multi-agent block; the next prepare must add the agents block.
func TestEnsureCodexMultiAgentConfigUpgradesLegacyManagedBlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `model = "o3"

[features]
` + multicaMultiAgentBeginMarker + `
multi_agent = false
` + multicaMultiAgentEndMarker + `
unified_exec = false
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)
	if n := strings.Count(got, multicaMultiAgentBeginMarker); n != 1 {
		t.Errorf("expected exactly one multi-agent block, got %d:\n%s", n, got)
	}
	parsed := parseTOML(t, got)
	requireMultiAgentDisabled(t, parsed)
	if v, _ := parsed["features"].(map[string]any)["unified_exec"].(bool); v {
		t.Errorf("expected features.unified_exec preserved as false, got:\n%s", got)
	}
}

func TestPrepareCodexHomeMultiAgentVersion(t *testing.T) {
	sharedHome := t.TempDir()
	sharedConfig := filepath.Join(sharedHome, "config.toml")
	original := `[features]
multi_agent = true
multi_agent_v2 = true

[agents]
max_threads = 4

[agents.researcher]
description = "Research-focused role."
`
	if err := os.WriteFile(sharedConfig, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", sharedHome)
	t.Setenv(MulticaCodexMultiAgentEnv, "")

	for _, tc := range []struct {
		version string
		modern  bool
	}{
		{version: "0.100.0"},
		{version: "0.105.0"},
		{version: "0.144.0"},
		{version: "0.144.99"},
		{version: ""},
		{version: "unknown"},
		{version: "0.145.0", modern: true},
		{version: "v0.145.0", modern: true},
		{version: "codex-cli 0.162.1", modern: true},
	} {
		t.Run(tc.version, func(t *testing.T) {
			taskHome := filepath.Join(t.TempDir(), "codex-home")
			opts := CodexHomeOptions{GOOS: "linux", CodexVersion: tc.version}
			if err := prepareCodexHomeWithOpts(taskHome, opts, discardLogger()); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(taskHome, "config.toml"))
			if err != nil {
				t.Fatal(err)
			}
			parsed := parseTOML(t, string(data))
			features := parsed["features"].(map[string]any)
			if features["multi_agent"] != false {
				t.Errorf("expected legacy multi-agent disabled, got %#v", features)
			}
			agents := parsed["agents"].(map[string]any)
			if tc.modern {
				requireMultiAgentDisabled(t, parsed)
			} else {
				if _, exists := features["multi_agent_v2"]; exists {
					t.Error("unexpected V2 flag for unsupported version")
				}
				if _, exists := agents["enabled"]; exists {
					t.Error("agents.enabled is unsupported by this Codex version")
				}
				if strings.Contains(string(data), multicaAgentsBeginMarker) {
					t.Error("unexpected managed agents block for unsupported version")
				}
			}
			if agents["max_threads"] != int64(4) {
				t.Errorf("lost agents.max_threads: %#v", agents)
			}
			role := agents["researcher"].(map[string]any)
			if role["description"] != "Research-focused role." {
				t.Errorf("lost custom role: %#v", role)
			}
		})
	}
	data, err := os.ReadFile(sharedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Error("shared Codex config was modified")
	}
}

func TestEnsureCodexMultiAgentConfigDowngrade(t *testing.T) {
	t.Parallel()

	for name, original := range map[string]string{
		"root": `features.multi_agent_v2 = true
agents.enabled = true
agents.max_threads = 4

[agents.researcher]
description = "Research-focused role."
`,
		"tables": `[features]
multi_agent_v2 = true

[agents]
enabled = true
max_threads = 4

[agents.researcher]
description = "Research-focused role."
`,
	} {
		for _, oldVersion := range []string{"0.144.0", ""} {
			t.Run(name+"/"+oldVersion, func(t *testing.T) {
				t.Parallel()
				configPath := filepath.Join(t.TempDir(), "config.toml")
				if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
					t.Fatal(err)
				}
				if err := ensureCodexMultiAgentConfig(configPath, oldVersion, nil); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				parsed := parseTOML(t, string(data))
				features := parsed["features"].(map[string]any)
				if features["multi_agent"] != false {
					t.Errorf("legacy multi-agent not disabled: %#v", features)
				}
				if _, exists := features["multi_agent_v2"]; exists {
					t.Error("V2 managed flag survived downgrade")
				}
				agents := parsed["agents"].(map[string]any)
				if _, exists := agents["enabled"]; exists {
					t.Error("agents.enabled survived downgrade")
				}
				if strings.Contains(string(data), multicaAgentsBeginMarker) {
					t.Error("managed agents block survived downgrade")
				}
				if agents["max_threads"] != int64(4) {
					t.Errorf("lost agents.max_threads: %#v", agents)
				}
				role := agents["researcher"].(map[string]any)
				if role["description"] != "Research-focused role." {
					t.Errorf("lost custom role: %#v", role)
				}
				if err := ensureCodexMultiAgentConfig(configPath, oldVersion, nil); err != nil {
					t.Fatal(err)
				}
				repeated, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				if string(repeated) != string(data) {
					t.Error("downgraded config was not idempotent")
				}
				if err := ensureCodexMultiAgentConfig(configPath, "0.145.0", nil); err != nil {
					t.Fatal(err)
				}
				upgraded, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				requireMultiAgentDisabled(t, parseTOML(t, string(upgraded)))
			})
		}
	}
}
