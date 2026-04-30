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

// requireMultiAgentDisabled asserts that the parsed config has
// features.multi_agent set to false.
func requireMultiAgentDisabled(t *testing.T, parsed map[string]any) {
	t.Helper()
	features, ok := parsed["features"].(map[string]any)
	if !ok {
		t.Fatalf("expected `features` table in parsed config, got: %#v", parsed["features"])
	}
	v, ok := features["multi_agent"].(bool)
	if !ok {
		t.Fatalf("expected features.multi_agent to be a bool, got: %#v", features["multi_agent"])
	}
	if v {
		t.Errorf("expected features.multi_agent = false, got true")
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

	if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
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

[profiles.default]
model = "o3"
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
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

	if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
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

	if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
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

			if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
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

	if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
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

	if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
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
`,
		"in_features_table": `[features]
multi_agent = true
experimental_thinking = true
`,
		"in_features_table_with_other_keys": `[features]
experimental_thinking = true

[profiles.default]
model = "o3"
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

			if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
				t.Fatalf("first run failed: %v", err)
			}
			first, _ := os.ReadFile(configPath)
			infoFirst, _ := os.Stat(configPath)

			if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
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
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Setenv(MulticaCodexMultiAgentEnv, "1")

	if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
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
	if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
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
	if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
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

	if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
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
