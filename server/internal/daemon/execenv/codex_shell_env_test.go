package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func assertValidToml(t *testing.T, content string) {
	t.Helper()
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("generated config.toml is invalid: %v\n---\n%s", err, content)
	}
}

func TestEnsureCodexShellEnvPolicyConfigAddsRootPolicy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	existing := `model = "gpt-5.6"
approval_policy = "never"
`
	if err := os.WriteFile(configPath, []byte(existing), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	if err := ensureCodexShellEnvPolicyConfig(configPath, testLogger()); err != nil {
		t.Fatalf("ensureCodexShellEnvPolicyConfig: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	s := string(data)
	assertValidToml(t, s)
	if !strings.Contains(s, `shell_environment_policy.inherit = "all"`) {
		t.Fatalf("missing shell_environment_policy inherit override:\n%s", s)
	}
	if !strings.Contains(s, "shell_environment_policy.ignore_default_excludes = true") {
		t.Fatalf("missing default-exclude override:\n%s", s)
	}
	if !strings.Contains(s, `"MULTICA_*"`) {
		t.Fatalf("missing MULTICA_* passthrough:\n%s", s)
	}
	if !strings.Contains(s, `model = "gpt-5.6"`) || !strings.Contains(s, `approval_policy = "never"`) {
		t.Fatalf("lost unrelated user config:\n%s", s)
	}
}

func TestEnsureCodexShellEnvPolicyConfigInjectsIntoExistingTable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	existing := `model = "gpt-5.6"

[shell_environment_policy]
inherit = "none"
set = { FOO = "bar", MULTICA_TOKEN = "stale" }

[features]
foo = true
`
	if err := os.WriteFile(configPath, []byte(existing), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	if err := ensureCodexShellEnvPolicyConfig(configPath, testLogger()); err != nil {
		t.Fatalf("ensureCodexShellEnvPolicyConfig: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	s := string(data)
	assertValidToml(t, s)
	if strings.Contains(s, "shell_environment_policy.inherit") {
		t.Fatalf("must not use dotted shell_environment_policy keys when table exists:\n%s", s)
	}
	if !strings.Contains(s, "[shell_environment_policy]\n"+multicaShellEnvBeginMarker+"\ninherit = \"all\"") {
		t.Fatalf("managed policy was not injected inside existing shell_environment_policy table:\n%s", s)
	}
	if strings.Contains(s, `FOO = "bar"`) || strings.Contains(s, `MULTICA_TOKEN = "stale"`) {
		t.Fatalf("stale user shell env policy directives should be replaced:\n%s", s)
	}
	if !strings.Contains(s, "[features]\nfoo = true") {
		t.Fatalf("lost unrelated table:\n%s", s)
	}
}

func TestEnsureCodexShellEnvPolicyConfigIsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	for i := 0; i < 3; i++ {
		if err := ensureCodexShellEnvPolicyConfig(configPath, testLogger()); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	s := string(data)
	assertValidToml(t, s)
	if n := strings.Count(s, multicaShellEnvBeginMarker); n != 1 {
		t.Fatalf("expected exactly one managed block, got %d:\n%s", n, s)
	}
}
