package execenv

import (
	"os"
	"path/filepath"
	"reflect"
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

func parsedShellEnvPolicy(t *testing.T, content string) codexShellEnvironmentPolicy {
	t.Helper()
	var parsed struct {
		Policy codexShellEnvironmentPolicy `toml:"shell_environment_policy"`
	}
	if err := toml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("parse generated config.toml: %v\n---\n%s", err, content)
	}
	return parsed.Policy
}

func TestCodexShellEnvAllowlistUsesExactTaskAndSafeInheritedNames(t *testing.T) {
	t.Parallel()

	inherited := []string{
		"PATH=/usr/bin",
		"SystemRoot=C:\\Windows",
		"COMSPEC=C:\\Windows\\System32\\cmd.exe",
		"PATHEXT=.COM;.EXE;.BAT;.CMD",
		"USERPROFILE=C:\\Users\\test",
		"APPDATA=C:\\Users\\test\\AppData\\Roaming",
		"LOCALAPPDATA=C:\\Users\\test\\AppData\\Local",
		"LANG=en_US.UTF-8",
		"HTTPS_PROXY=http://proxy.example",
		"SSL_CERT_FILE=/etc/ssl/cert.pem",
		"SDKROOT=/opt/sdk",
		"OPENAI_API_KEY=host-secret",
		"GH_TOKEN=host-secret",
		"MULTICA_LLM_API_KEY=daemon-secret",
		"MULTICA_SERVER_URL=https://wrong.example",
	}
	explicit := map[string]string{
		"MULTICA_TOKEN":      "mat_task",
		"MULTICA_SERVER_URL": "https://task.example",
		"CUSTOM_FLAG":        "enabled",
		"ANTHROPIC_API_KEY":  "agent-secret",
	}

	got := CodexShellEnvAllowlist(inherited, explicit)
	want := []string{
		"APPDATA",
		"COMSPEC",
		"CUSTOM_FLAG",
		"HTTPS_PROXY",
		"LANG",
		"LOCALAPPDATA",
		"MULTICA_SERVER_URL",
		"MULTICA_TOKEN",
		"PATH",
		"PATHEXT",
		"SDKROOT",
		"SSL_CERT_FILE",
		"SystemRoot",
		"USERPROFILE",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CodexShellEnvAllowlist() = %#v, want %#v", got, want)
	}
}

func TestEnsureCodexShellEnvPolicyConfigReplacesAllLegalPolicyForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing string
		kept     []string
		removed  []string
	}{
		{
			name: "root dotted multiline arrays",
			existing: `# keep root comment
model = "gpt-5.6"
shell_environment_policy.inherit = "none"
shell_environment_policy.include_only = [
  "PATH", # old item comment
  "HOME",
]
shell_environment_policy.exclude = [
  "AWS_*",
]

[features]
foo = true
`,
			kept:    []string{"# keep root comment", `model = "gpt-5.6"`, "[features]", "foo = true"},
			removed: []string{"old item comment", `"AWS_*"`},
		},
		{
			name: "quoted table and comments",
			existing: `model = "gpt-5.6"

["shell_environment_policy"] # legal quoted table
# policy-local comment remains harmless
inherit = "none"
include_only = [
  "PATH",
  "HOME",
]
set = { FOO = "bar", MULTICA_TOKEN = "stale" }

[features]
# keep feature comment
foo = true
`,
			kept:    []string{`model = "gpt-5.6"`, "# policy-local comment remains harmless", "# keep feature comment", "[features]", "foo = true"},
			removed: []string{"legal quoted table", `FOO = "bar"`, `MULTICA_TOKEN = "stale"`},
		},
		{
			name: "inline root and profile policies",
			existing: `shell_environment_policy = { inherit = "none", include_only = ["PATH"] }

[profiles.work]
model = "gpt-5.6"
shell_environment_policy = { inherit = "core", exclude = ["AWS_*"] }

[features]
foo = true

[tools]
shell_environment_policy = "unrelated key with the same name"
`,
			kept:    []string{"[profiles.work]", `model = "gpt-5.6"`, "[features]", "foo = true", "[tools]", `shell_environment_policy = "unrelated key with the same name"`},
			removed: []string{`inherit = "none"`, `inherit = "core"`, `"AWS_*"`},
		},
		{
			name: "nested profile policy tables",
			existing: `[profiles.work]
model = "gpt-5.6"

[profiles.work.shell_environment_policy]
include_only = [
  "PATH",
]

[profiles.work.shell_environment_policy.set]
FOO = "bar"

[features]
foo = true
`,
			kept:    []string{"[profiles.work]", `model = "gpt-5.6"`, "[features]", "foo = true"},
			removed: []string{"[profiles.work.shell_environment_policy]", "[profiles.work.shell_environment_policy.set]", `FOO = "bar"`},
		},
	}

	includeOnly := []string{"CUSTOM_FLAG", "MULTICA_SERVER_URL", "MULTICA_TOKEN", "PATH", "SystemRoot"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			configPath := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(configPath, []byte(tt.existing), 0o644); err != nil {
				t.Fatalf("write config.toml: %v", err)
			}

			if err := EnsureCodexShellEnvPolicyConfig(configPath, includeOnly, testLogger()); err != nil {
				t.Fatalf("EnsureCodexShellEnvPolicyConfig: %v", err)
			}
			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("read config.toml: %v", err)
			}
			s := string(data)
			assertValidToml(t, s)
			for _, want := range tt.kept {
				if !strings.Contains(s, want) {
					t.Errorf("lost unrelated content %q:\n%s", want, s)
				}
			}
			for _, unwanted := range tt.removed {
				if strings.Contains(s, unwanted) {
					t.Errorf("stale policy content %q remains:\n%s", unwanted, s)
				}
			}
			if strings.Contains(s, `"MULTICA_*"`) {
				t.Fatalf("managed policy must not use a MULTICA_* wildcard:\n%s", s)
			}
			if n := strings.Count(s, "[shell_environment_policy]"); n != 1 {
				t.Fatalf("expected exactly one managed policy table, got %d:\n%s", n, s)
			}
			policy := parsedShellEnvPolicy(t, s)
			if policy.Inherit != "all" || !policy.IgnoreDefaultExcludes {
				t.Fatalf("unexpected managed policy: %#v", policy)
			}
			if !reflect.DeepEqual(policy.IncludeOnly, includeOnly) {
				t.Fatalf("include_only = %#v, want %#v", policy.IncludeOnly, includeOnly)
			}
		})
	}
}

func TestEnsureCodexShellEnvPolicyConfigRejectsInvalidInputWithoutWriting(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	existing := []byte("model = [\n")
	if err := os.WriteFile(configPath, existing, 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
	if err := EnsureCodexShellEnvPolicyConfig(configPath, []string{"MULTICA_TOKEN", "PATH"}, testLogger()); err == nil {
		t.Fatal("expected malformed input to fail")
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	if !reflect.DeepEqual(got, existing) {
		t.Fatalf("invalid input was modified: got %q, want %q", got, existing)
	}
}

func TestEnsureCodexShellEnvPolicyConfigIsIdempotent(t *testing.T) {
	t.Parallel()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	includeOnly := []string{"MULTICA_SERVER_URL", "MULTICA_TOKEN", "PATH"}

	for i := 0; i < 3; i++ {
		if err := EnsureCodexShellEnvPolicyConfig(configPath, includeOnly, testLogger()); err != nil {
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
