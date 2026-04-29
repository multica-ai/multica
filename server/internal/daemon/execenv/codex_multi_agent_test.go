package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
		t.Errorf("expected managed block to set features.multi_agent = false, got:\n%s", got)
	}
	if !strings.Contains(got, multicaMultiAgentBeginMarker) {
		t.Errorf("expected begin marker, got:\n%s", got)
	}
	if !strings.Contains(got, multicaMultiAgentEndMarker) {
		t.Errorf("expected end marker, got:\n%s", got)
	}
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
	// User's `features.multi_agent = true` must be gone.
	if strings.Contains(got, "features.multi_agent = true") {
		t.Errorf("expected user features.multi_agent = true to be stripped, got:\n%s", got)
	}
	// Daemon's `features.multi_agent = false` must be present.
	if !strings.Contains(got, "features.multi_agent = false") {
		t.Errorf("expected managed features.multi_agent = false, got:\n%s", got)
	}
	// Unrelated content preserved.
	if !strings.Contains(got, `[profiles.default]`) || !strings.Contains(got, `model = "o3"`) {
		t.Errorf("expected unrelated content preserved, got:\n%s", got)
	}
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
	if strings.Contains(got, "multi_agent = true") {
		t.Errorf("expected user multi_agent = true to be stripped, got:\n%s", got)
	}
	if !strings.Contains(got, "features.multi_agent = false") {
		t.Errorf("expected managed features.multi_agent = false, got:\n%s", got)
	}
	if !strings.Contains(got, "experimental_thinking = true") {
		t.Errorf("expected sibling features.* keys preserved, got:\n%s", got)
	}
	if !strings.Contains(got, "[features]") {
		t.Errorf("expected [features] header preserved, got:\n%s", got)
	}
}

func TestEnsureCodexMultiAgentConfigIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `model = "o3"
features.multi_agent = true
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	first, _ := os.ReadFile(configPath)
	infoFirst, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat first: %v", err)
	}

	if err := ensureCodexMultiAgentConfig(configPath, nil); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	second, _ := os.ReadFile(configPath)
	infoSecond, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat second: %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("expected idempotent rewrite\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	if !infoSecond.ModTime().Equal(infoFirst.ModTime()) {
		t.Errorf("expected no rewrite on second pass (file was touched)")
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
	if !strings.Contains(got, "features.multi_agent = false") {
		t.Errorf("expected managed features.multi_agent = false, got:\n%s", got)
	}

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
