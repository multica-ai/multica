package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// requireMemoryDisabled asserts that the parsed config has Codex memory
// effectively turned off: features.memories = false, plus
// memories.generate_memories = false and memories.use_memories = false.
func requireMemoryDisabled(t *testing.T, parsed map[string]any) {
	t.Helper()

	features, ok := parsed["features"].(map[string]any)
	if !ok {
		t.Fatalf("expected `features` table in parsed config, got: %#v", parsed["features"])
	}
	v, ok := features["memories"].(bool)
	if !ok {
		t.Fatalf("expected features.memories to be a bool, got: %#v", features["memories"])
	}
	if v {
		t.Errorf("expected features.memories = false, got true")
	}

	memories, ok := parsed["memories"].(map[string]any)
	if !ok {
		t.Fatalf("expected `memories` table in parsed config, got: %#v", parsed["memories"])
	}
	gen, ok := memories["generate_memories"].(bool)
	if !ok {
		t.Fatalf("expected memories.generate_memories to be a bool, got: %#v", memories["generate_memories"])
	}
	if gen {
		t.Errorf("expected memories.generate_memories = false, got true")
	}
	use, ok := memories["use_memories"].(bool)
	if !ok {
		t.Fatalf("expected memories.use_memories to be a bool, got: %#v", memories["use_memories"])
	}
	if use {
		t.Errorf("expected memories.use_memories = false, got true")
	}
}

func TestStripUserMemoryDirectives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "drops root dotted-key forms",
			in: `model = "o3"
features.memories = true
memories.generate_memories = true
memories.use_memories = true

[profiles.default]
model = "o3"
`,
			want: `model = "o3"

[profiles.default]
model = "o3"
`,
		},
		{
			name: "drops root dotted-key form with whitespace",
			in: `model = "o3"
features . memories = true
memories . generate_memories = true
`,
			want: `model = "o3"
`,
		},
		{
			name: "drops memories inside [features] table",
			in: `[features]
memories = true
multi_agent = false

[profiles.default]
model = "o3"
`,
			want: `[features]
multi_agent = false

[profiles.default]
model = "o3"
`,
		},
		{
			name: "drops generate_memories / use_memories inside [memories] table",
			in: `[memories]
generate_memories = true
use_memories = true
some_other_key = "keep"

[profiles.default]
model = "o3"
`,
			want: `[memories]
some_other_key = "keep"

[profiles.default]
model = "o3"
`,
		},
		{
			name: "preserves keys under nested [features.experimental]",
			in: `[features.experimental]
memories = true
`,
			want: `[features.experimental]
memories = true
`,
		},
		{
			name: "preserves keys under nested [memories.advanced]",
			in: `[memories.advanced]
generate_memories = true
use_memories = true
`,
			want: `[memories.advanced]
generate_memories = true
use_memories = true
`,
		},
		{
			name: "no memory directives — content unchanged",
			in: `model = "o3"

[profiles.default]
model = "o3"
`,
			want: `model = "o3"

[profiles.default]
model = "o3"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stripUserMemoryDirectives(tt.in)
			if got != tt.want {
				t.Errorf("stripUserMemoryDirectives mismatch\n--- got ---\n%s\n--- want ---\n%s", got, tt.want)
			}
		})
	}
}

func TestEnsureCodexMemoryConfigEmptyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	if err := ensureCodexMemoryConfig(configPath, nil); err != nil {
		t.Fatalf("ensureCodexMemoryConfig failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"features.memories = false",
		"memories.generate_memories = false",
		"memories.use_memories = false",
		multicaMemoryFeatureBeginMarker,
		multicaMemoryFeatureEndMarker,
		multicaMemoryConfigBeginMarker,
		multicaMemoryConfigEndMarker,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output, got:\n%s", want, got)
		}
	}
	requireMemoryDisabled(t, parseTOML(t, got))
}

func TestEnsureCodexMemoryConfigDottedKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `model = "o3"
features.memories = true
memories.generate_memories = true
memories.use_memories = true

[profiles.default]
model = "o3"
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMemoryConfig(configPath, nil); err != nil {
		t.Fatalf("ensureCodexMemoryConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)
	for _, banned := range []string{
		"features.memories = true",
		"memories.generate_memories = true",
		"memories.use_memories = true",
	} {
		if strings.Contains(got, banned) {
			t.Errorf("expected user %q stripped, got:\n%s", banned, got)
		}
	}
	if !strings.Contains(got, `[profiles.default]`) || !strings.Contains(got, `model = "o3"`) {
		t.Errorf("expected unrelated content preserved, got:\n%s", got)
	}
	requireMemoryDisabled(t, parseTOML(t, got))
}

func TestEnsureCodexMemoryConfigFeaturesTable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `[features]
memories = true
experimental_thinking = true

[profiles.default]
model = "o3"
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMemoryConfig(configPath, nil); err != nil {
		t.Fatalf("ensureCodexMemoryConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)

	// User's `memories = true` inside [features] must be gone.
	// Managed override must be INSIDE [features], not as a root dotted key
	// (which would redefine the table and break the strict TOML parser).
	if strings.Contains(got, "memories = true") {
		t.Errorf("expected user memories = true to be stripped, got:\n%s", got)
	}
	if strings.Contains(got, "features.memories = false") {
		t.Errorf("managed memory-feature block must NOT use root dotted-key form when [features] table exists (would redefine the table); got:\n%s", got)
	}
	if !strings.Contains(got, "[features]") {
		t.Errorf("expected [features] header preserved, got:\n%s", got)
	}
	if !strings.Contains(got, "experimental_thinking = true") {
		t.Errorf("expected sibling features.* keys preserved, got:\n%s", got)
	}

	// memory-config side still root-form because no [memories] table existed.
	if !strings.Contains(got, "memories.generate_memories = false") {
		t.Errorf("expected memories.generate_memories = false at root, got:\n%s", got)
	}
	if !strings.Contains(got, "memories.use_memories = false") {
		t.Errorf("expected memories.use_memories = false at root, got:\n%s", got)
	}

	requireMemoryDisabled(t, parseTOML(t, got))
}

func TestEnsureCodexMemoryConfigMemoriesTable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `[memories]
generate_memories = true
use_memories = true
storage_path = "/somewhere"

[profiles.default]
model = "o3"
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMemoryConfig(configPath, nil); err != nil {
		t.Fatalf("ensureCodexMemoryConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)

	if strings.Contains(got, "generate_memories = true") {
		t.Errorf("expected user generate_memories = true to be stripped, got:\n%s", got)
	}
	if strings.Contains(got, "use_memories = true") {
		t.Errorf("expected user use_memories = true to be stripped, got:\n%s", got)
	}
	if strings.Contains(got, "memories.generate_memories = false") {
		t.Errorf("managed memory-config block must NOT use root dotted-key form when [memories] table exists (would redefine the table); got:\n%s", got)
	}
	if !strings.Contains(got, `storage_path = "/somewhere"`) {
		t.Errorf("expected sibling memories.* keys preserved, got:\n%s", got)
	}

	// memory-feature side still root-form because no [features] table existed.
	if !strings.Contains(got, "features.memories = false") {
		t.Errorf("expected features.memories = false at root, got:\n%s", got)
	}

	requireMemoryDisabled(t, parseTOML(t, got))
}

func TestEnsureCodexMemoryConfigBothTables(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `[features]
memories = true
experimental_thinking = true

[memories]
generate_memories = true
use_memories = true
storage_path = "/somewhere"
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMemoryConfig(configPath, nil); err != nil {
		t.Fatalf("ensureCodexMemoryConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)

	// No root dotted-key forms when both tables exist.
	if strings.Contains(got, "features.memories = false") {
		t.Errorf("managed block must NOT use root dotted-key form when [features] table exists; got:\n%s", got)
	}
	if strings.Contains(got, "memories.generate_memories = false") {
		t.Errorf("managed block must NOT use root dotted-key form when [memories] table exists; got:\n%s", got)
	}

	// User content preserved.
	if !strings.Contains(got, "experimental_thinking = true") {
		t.Errorf("expected experimental_thinking preserved, got:\n%s", got)
	}
	if !strings.Contains(got, `storage_path = "/somewhere"`) {
		t.Errorf("expected storage_path preserved, got:\n%s", got)
	}

	requireMemoryDisabled(t, parseTOML(t, got))
}

func TestEnsureCodexMemoryConfigFeaturesSubtableOnly(t *testing.T) {
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

	if err := ensureCodexMemoryConfig(configPath, nil); err != nil {
		t.Fatalf("ensureCodexMemoryConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)
	if !strings.Contains(got, "features.memories = false") {
		t.Errorf("expected root dotted-key form when only sub-tables exist, got:\n%s", got)
	}

	parsed := parseTOML(t, got)
	requireMemoryDisabled(t, parsed)
	features := parsed["features"].(map[string]any)
	exp, _ := features["experimental"].(map[string]any)
	if v, _ := exp["thinking"].(bool); !v {
		t.Errorf("expected features.experimental.thinking preserved, got: %#v", exp)
	}
}

func TestEnsureCodexMemoryConfigIdempotent(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"root_form": `model = "o3"
features.memories = true
memories.generate_memories = true
memories.use_memories = true
`,
		"features_table_only": `[features]
memories = true
multi_agent = false
`,
		"memories_table_only": `[memories]
generate_memories = true
use_memories = true
storage_path = "/somewhere"
`,
		"both_tables": `[features]
memories = true

[memories]
generate_memories = true
use_memories = true
`,
		"empty": ``,
	}
	for name, original := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.toml")
			if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			if err := ensureCodexMemoryConfig(configPath, nil); err != nil {
				t.Fatalf("first run failed: %v", err)
			}
			first, _ := os.ReadFile(configPath)
			infoFirst, _ := os.Stat(configPath)

			if err := ensureCodexMemoryConfig(configPath, nil); err != nil {
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
			requireMemoryDisabled(t, parseTOML(t, string(second)))
		})
	}
}

func TestEnsureCodexMemoryConfigEscapeHatch(t *testing.T) {
	// Cannot run in parallel: mutates process env.
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `model = "o3"
features.memories = true
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Setenv(MulticaCodexMemoryEnv, "1")

	if err := ensureCodexMemoryConfig(configPath, nil); err != nil {
		t.Fatalf("ensureCodexMemoryConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)
	if got != original {
		t.Errorf("expected file untouched when escape hatch set\n--- got ---\n%s\n--- want ---\n%s", got, original)
	}
}

func TestCodexMemoryEnabledTruthy(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "yes", "On"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(MulticaCodexMemoryEnv, v)
			if !codexMemoryEnabled() {
				t.Errorf("expected %q to be truthy", v)
			}
		})
	}
}

func TestCodexMemoryEnabledFalsy(t *testing.T) {
	for _, v := range []string{"", "0", "false", "no", "off", "anything else"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(MulticaCodexMemoryEnv, v)
			if codexMemoryEnabled() {
				t.Errorf("expected %q to be falsy", v)
			}
		})
	}
}

func TestEnsureCodexMemoryConfigCoexistsWithSandboxAndMultiAgent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `model = "o3"
features.memories = true
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
	if err := ensureCodexMemoryConfig(configPath, nil); err != nil {
		t.Fatalf("ensureCodexMemoryConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	got := string(data)
	for _, marker := range []string{
		multicaManagedBeginMarker,
		multicaMultiAgentBeginMarker,
		multicaMemoryFeatureBeginMarker,
		multicaMemoryConfigBeginMarker,
	} {
		if !strings.Contains(got, marker) {
			t.Errorf("expected marker %q in combined output, got:\n%s", marker, got)
		}
	}
	if strings.Contains(got, "features.memories = true") {
		t.Errorf("expected user features.memories = true stripped, got:\n%s", got)
	}
	if strings.Contains(got, "features.multi_agent = true") {
		t.Errorf("expected user features.multi_agent = true stripped, got:\n%s", got)
	}

	// File must parse as valid TOML with everything disabled.
	parsed := parseTOML(t, got)
	requireMemoryDisabled(t, parsed)
	requireMultiAgentDisabled(t, parsed)

	// Re-running all three must be idempotent.
	if err := ensureCodexSandboxConfig(configPath, policy, "0.121.0", nil); err != nil {
		t.Fatalf("ensureCodexSandboxConfig (rerun) failed: %v", err)
	}
	if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
		t.Fatalf("ensureCodexMultiAgentConfig (rerun) failed: %v", err)
	}
	if err := ensureCodexMemoryConfig(configPath, nil); err != nil {
		t.Fatalf("ensureCodexMemoryConfig (rerun) failed: %v", err)
	}
	dataAfter, _ := os.ReadFile(configPath)
	if string(dataAfter) != got {
		t.Errorf("expected idempotent combined rewrite\n--- first ---\n%s\n--- second ---\n%s", got, dataAfter)
	}
}

// Regression: when the user's config has a `[features]` table, naively
// writing `features.memories = false` at the TOML root implicitly
// redefines the same table. The strict TOML parser used by Codex
// (`toml-rs`) rejects that with `table 'features' already exists`. Same
// trap exists for `[memories]`.
func TestRegressionMemoryProducesValidTOML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `[features]
experimental_thinking = true

[memories]
storage_path = "/somewhere"
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMemoryConfig(configPath, nil); err != nil {
		t.Fatalf("ensureCodexMemoryConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	parsed := parseTOML(t, string(data))
	requireMemoryDisabled(t, parsed)

	features := parsed["features"].(map[string]any)
	if v, _ := features["experimental_thinking"].(bool); !v {
		t.Errorf("expected user's features.experimental_thinking preserved, got %v", features["experimental_thinking"])
	}
	memories := parsed["memories"].(map[string]any)
	if v, _ := memories["storage_path"].(string); v != "/somewhere" {
		t.Errorf("expected user's memories.storage_path preserved, got %v", memories["storage_path"])
	}
}
