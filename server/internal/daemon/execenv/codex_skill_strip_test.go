package execenv

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestStripTaskIrrelevantCodexConfigEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no skills config — returned unchanged",
			in:   "model = \"o3\"\n",
			want: "model = \"o3\"\n",
		},
		{
			name: "drops well-formed file-backed entry",
			in: `model = "o3"

[[skills.config]]
path = "/Users/x/SKILL.md"
enabled = true
`,
			want: `model = "o3"
`,
		},
		{
			name: "drops plugin entry that lacks path",
			in: `[[skills.config]]
name = "superpowers:brainstorming"
enabled = false

[profiles.default]
model = "o3"
`,
			want: `[profiles.default]
model = "o3"
`,
		},
		{
			name: "drops a mix of consecutive entries and preserves surrounding tables",
			in: `model = "o3"

[[skills.config]]
path = "/Users/x/SKILL.md"
enabled = false

[[skills.config]]
path = "/Users/y/SKILL.md"
enabled = false

[[skills.config]]
name = "superpowers:brainstorming"
enabled = false

[profiles.default]
model = "o3"

[mcp_servers.foo]
command = "foo"
`,
			want: `model = "o3"

[profiles.default]
model = "o3"

[mcp_servers.foo]
command = "foo"
`,
		},
		{
			name: "skills.config at EOF",
			in: `model = "o3"

[[skills.config]]
name = "superpowers:dispatching-parallel-agents"
enabled = false
`,
			want: `model = "o3"
`,
		},
		{
			name: "preserves unrelated [skills] table (single brackets)",
			in: `[skills]
discovery_path = "skills"
`,
			want: `[skills]
discovery_path = "skills"
`,
		},
		{
			name: "fully empty after strip returns empty string",
			in: `[[skills.config]]
name = "x"
enabled = false
`,
			want: ``,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := stripTaskIrrelevantCodexConfigEntries([]byte(tt.in))
			if err != nil {
				t.Fatalf("stripTaskIrrelevantCodexConfigEntries failed: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("stripTaskIrrelevantCodexConfigEntries result mismatch\n--- got ---\n%s\n--- want ---\n%s", got, tt.want)
			}
		})
	}
}

func TestSanitizeCopiedCodexConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := `model = "o3"

[marketplaces.claude-plugins-official]
last_updated = "2026-07-17T03:11:33Z"
source_type = "git"
source = "https://github.com/anthropics/claude-plugins-official.git"

[plugins."superpowers@claude-plugins-official"]
enabled = true

[[skills.config]]
name = "superpowers:brainstorming"
enabled = false

[[skills.config]]
path = "/Users/x/SKILL.md"
enabled = true

[profiles.default]
model = "o3"

[mcp_servers.foo]
command = "foo"
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := sanitizeCopiedCodexConfig(configPath); err != nil {
		t.Fatalf("sanitizeCopiedCodexConfig failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "[[skills.config]]") {
		t.Errorf("expected all [[skills.config]] entries to be removed, got:\n%s", got)
	}
	if strings.Contains(got, "[marketplaces.") {
		t.Errorf("expected user-level marketplace entries to be removed, got:\n%s", got)
	}
	if !strings.Contains(got, `[plugins."superpowers@claude-plugins-official"]`) {
		t.Errorf("expected installed plugin registry to be preserved, got:\n%s", got)
	}
	if !strings.Contains(got, `[profiles.default]`) {
		t.Errorf("unrelated tables should be preserved, got:\n%s", got)
	}
	if !strings.Contains(got, `[mcp_servers.foo]`) {
		t.Errorf("unrelated MCP configuration should be preserved, got:\n%s", got)
	}
	if !strings.Contains(got, `model = "o3"`) {
		t.Errorf("top-level keys should be preserved, got:\n%s", got)
	}
}

func TestSanitizeCopiedCodexConfigHandlesSemanticRegistryForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		original string
		want     string
	}{
		{
			name: "dotted and inline root keys",
			original: `model = "o3"
marketplaces.foo.source = "https://example.test/plugins.git"
plugins."demo@foo".enabled = true
skills.config = [{ path = "/tmp/SKILL.md", enabled = true }]
`,
			want: `model = "o3"
plugins."demo@foo".enabled = true
`,
		},
		{
			name: "quoted and array table headers",
			original: `model = "o3"

[ "marketplaces" . foo ]
source = "https://example.test/plugins.git"

[ "plugins" . "demo@foo" ]
enabled = true

[[skills.config]]
path = "/tmp/SKILL.md"
enabled = true
`,
			want: `model = "o3"

[ "plugins" . "demo@foo" ]
enabled = true
`,
		},
		{
			name: "inline marketplace and plugin roots",
			original: `marketplaces = { foo = { source_type = "git", source = "https://example.test/plugins.git" } }
plugins = { "demo@foo" = { enabled = true } }
model = "o3"
`,
			want: `plugins = { "demo@foo" = { enabled = true } }
model = "o3"
`,
		},
		{
			name: "inline skills table removes only config",
			original: `skills = { config = [{ name = "superpowers:brainstorming", enabled = false }], discovery_path = "skills" }
model = "o3"
`,
			want: `skills = { discovery_path = "skills" }
model = "o3"
`,
		},
		{
			name: "inline skills table removes trailing config",
			original: `skills = { discovery_path = "literal } remains", config = [{ path = "/tmp/SKILL.md" }] }
model = "o3"
`,
			want: `skills = { discovery_path = "literal } remains" }
model = "o3"
`,
		},
		{
			name: "inline skills table preserves four-quote multiline basic string",
			original: `skills = { discovery_path = """
Closing with four quotes
"""", config = [{ path = "/tmp/SKILL.md" }] }
model = "o3"
`,
			want: `skills = { discovery_path = """
Closing with four quotes
"""" }
model = "o3"
`,
		},
		{
			name: "inline skills table preserves four-quote multiline literal string",
			original: `skills = { discovery_path = '''
Closing with four apostrophes
'''', config = [{ path = '/tmp/SKILL.md' }] }
model = "o3"
`,
			want: `skills = { discovery_path = '''
Closing with four apostrophes
'''' }
model = "o3"
`,
		},
		{
			name: "inline skills table with only config is dropped",
			original: `model = "o3"
skills = { config = [{ path = "/tmp/SKILL.md" }] }
`,
			want: `model = "o3"
`,
		},
		{
			name: "header-like text inside multiline string",
			original: `developer_instructions = """
[plugins.demo]
do not load this example
"""
model = "o3"
`,
			want: `developer_instructions = """
[plugins.demo]
do not load this example
"""
model = "o3"
`,
		},
		{
			name: "unrelated nested keys",
			original: `[profiles.default]
plugins.demo = "profile-local-value"

[skills]
discovery_path = "skills"
`,
			want: `[profiles.default]
plugins.demo = "profile-local-value"

[skills]
discovery_path = "skills"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			configPath := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(configPath, []byte(tt.original), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			if err := sanitizeCopiedCodexConfig(configPath); err != nil {
				t.Fatalf("sanitizeCopiedCodexConfig failed: %v", err)
			}
			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("read result: %v", err)
			}
			if string(data) != tt.want {
				t.Errorf("sanitized config mismatch\n--- got ---\n%s\n--- want ---\n%s", data, tt.want)
			}
			var decoded map[string]any
			if err := toml.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("sanitized config is invalid TOML: %v\n%s", err, data)
			}
		})
	}
}

func TestSanitizeCopiedCodexConfigNoop(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := "model = \"o3\"\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	infoBefore, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}

	if err := sanitizeCopiedCodexConfig(configPath); err != nil {
		t.Fatalf("sanitizeCopiedCodexConfig failed: %v", err)
	}

	infoAfter, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !infoAfter.ModTime().Equal(infoBefore.ModTime()) {
		t.Errorf("file should not be rewritten when there is nothing to strip")
	}
	data, _ := os.ReadFile(configPath)
	if string(data) != original {
		t.Errorf("content drifted: got %q, want %q", data, original)
	}
}

func TestSanitizeCopiedCodexConfigMissingFile(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "does-not-exist.toml")
	if err := sanitizeCopiedCodexConfig(missing); err != nil {
		t.Errorf("missing file should be a no-op, got error: %v", err)
	}
}

func TestSanitizeCopiedCodexConfigRejectsInvalidInputWithoutWriting(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	original := []byte("plugins.demo = [\n")
	if err := os.WriteFile(configPath, original, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := sanitizeCopiedCodexConfig(configPath); err == nil {
		t.Fatal("expected malformed config to fail")
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("invalid config was modified: got %q, want %q", got, original)
	}
}
