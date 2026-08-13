package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// hermesExternalDirs parses skills.external_dirs out of a derived config.yaml.
func hermesExternalDirs(t *testing.T, configPath string) []string {
	t.Helper()
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read derived config: %v", err)
	}
	var parsed struct {
		Skills struct {
			ExternalDirs []string `yaml:"external_dirs"`
		} `yaml:"skills"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse derived config: %v\n%s", err, data)
	}
	return parsed.Skills.ExternalDirs
}

// hermesMemoryProvider returns the memory.provider value in a derived config,
// and whether the key is present.
func hermesMemoryProvider(t *testing.T, configPath string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read derived config: %v", err)
	}
	var parsed struct {
		Memory struct {
			Provider *string `yaml:"provider"`
		} `yaml:"memory"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse derived config: %v", err)
	}
	if parsed.Memory.Provider == nil {
		return "", false
	}
	return *parsed.Memory.Provider, true
}

// TestPrepareHermesHomeOverlay verifies the compatibility overlay: shared state
// is mirrored via symlink, the derived config references the user's real skills
// as an external root, and only the bound skill lands in the task-local skills/
// dir (the user's global skills are referenced, not copied).
func TestPrepareHermesHomeOverlay(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "auth.json"), `{"token":"secret"}`)
	mustWrite(t, filepath.Join(sharedHome, ".env"), "API_KEY=abc")
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), "model: hermes-4\n")
	mustWrite(t, filepath.Join(sharedHome, "plugins", "custom-provider", "plugin.py"), "# provider plugin")
	mustWrite(t, filepath.Join(sharedHome, "oauth_state.json"), `{"nous":"tok"}`)
	mustWrite(t, filepath.Join(sharedHome, "skills", "personal-notes", "SKILL.md"), "My personal notes skill.")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "Help review code."}}
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	for _, name := range []string{"auth.json", "plugins", "oauth_state.json"} {
		fi, err := os.Lstat(filepath.Join(hermesHome, name))
		if err != nil {
			t.Fatalf("%s not mirrored into overlay: %v", name, err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s should be a symlink into the shared home", name)
		}
	}

	// .env is overlay-owned/derived (not symlinked): it preserves the source's
	// credentials but pins HERMES_HOME to the overlay so Hermes' override=True
	// dotenv load can't relocate the home past it.
	envPath := filepath.Join(hermesHome, ".env")
	if fi, err := os.Lstat(envPath); err != nil {
		t.Fatalf(".env missing from overlay: %v", err)
	} else if fi.Mode()&os.ModeSymlink != 0 {
		t.Error(".env should be a derived copy, not a symlink into the shared home")
	}
	if data, _ := os.ReadFile(envPath); !strings.Contains(string(data), "API_KEY=abc") {
		t.Error("derived .env dropped the source credentials")
	} else if !strings.Contains(string(data), "HERMES_HOME='"+hermesHome+"'") {
		t.Errorf("derived .env must pin HERMES_HOME to the overlay, got:\n%s", data)
	}

	cfgPath := filepath.Join(hermesHome, "config.yaml")
	if fi, err := os.Lstat(cfgPath); err != nil {
		t.Fatalf("config.yaml missing: %v", err)
	} else if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("config.yaml should be a derived copy, not a symlink")
	}
	if data, _ := os.ReadFile(cfgPath); !strings.Contains(string(data), "hermes-4") {
		t.Error("derived config dropped the user's model setting")
	}
	wantExternal := filepath.Join(sharedHome, "skills")
	if got := hermesExternalDirs(t, cfgPath); len(got) != 1 || got[0] != wantExternal {
		t.Errorf("external_dirs = %v, want [%s]", got, wantExternal)
	}

	if body, err := os.ReadFile(filepath.Join(hermesHome, "skills", "review-helper", "SKILL.md")); err != nil {
		t.Fatalf("bound skill not written: %v", err)
	} else if !strings.Contains(string(body), "Help review code.") {
		t.Error("bound SKILL.md missing content")
	}
	if _, err := os.Stat(filepath.Join(hermesHome, "skills", "personal-notes")); !os.IsNotExist(err) {
		t.Error("user global skill should be referenced via external_dirs, not copied into the task-local skills/")
	}
}

// TestHermesDisablesExternalMemoryProvider is the regression for the shared
// memory-backend blocker: a host-configured memory.provider must be neutralized
// in the derived config so managed tasks don't share an external memory bank.
func TestHermesDisablesExternalMemoryProvider(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"),
		"memory:\n  provider: supermemory\n  memory_enabled: true\n")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	got, ok := hermesMemoryProvider(t, filepath.Join(hermesHome, "config.yaml"))
	if !ok {
		t.Fatal("memory.provider should be present and explicitly disabled")
	}
	if got != "" {
		t.Errorf("memory.provider = %q, want \"\" (external backend disabled)", got)
	}
	if data, _ := os.ReadFile(filepath.Join(hermesHome, "config.yaml")); !strings.Contains(string(data), "memory_enabled: true") {
		t.Error("built-in memory settings should be preserved")
	}
}

func TestPrepareHermesScheduledRunOnlyDisablesOnlyFigmaMCP(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	hostConfig := `model: hermes-4
mcp_servers:
  FiGmA:
    transport: http
    url: https://mcp.figma.example
    auth: oauth
    enabled: true
    custom_setting: keep-me
  github:
    command: github-mcp
    args: ["serve"]
    enabled: true
`
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), hostConfig)

	type mcpServer struct {
		Enabled       *bool    `yaml:"enabled"`
		Transport     string   `yaml:"transport"`
		URL           string   `yaml:"url"`
		Auth          string   `yaml:"auth"`
		CustomSetting string   `yaml:"custom_setting"`
		Command       string   `yaml:"command"`
		Args          []string `yaml:"args"`
	}
	readServers := func(configPath string) map[string]mcpServer {
		t.Helper()
		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read derived config: %v", err)
		}
		var parsed struct {
			MCPServers map[string]mcpServer `yaml:"mcp_servers"`
		}
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("parse derived config: %v", err)
		}
		return parsed.MCPServers
	}

	tests := []struct {
		name             string
		taskID           string
		task             TaskContextForEnv
		wantFigmaEnabled bool
	}{
		{
			name:             "scheduled run-only",
			taskID:           "aaaa1111-2222-3333-4444-555566667777",
			task:             TaskContextForEnv{AutopilotRunID: "run-scheduled", AutopilotSource: "schedule"},
			wantFigmaEnabled: false,
		},
		{
			name:             "manual run-only",
			taskID:           "bbbb1111-2222-3333-4444-555566667777",
			task:             TaskContextForEnv{AutopilotRunID: "run-manual", AutopilotSource: "manual"},
			wantFigmaEnabled: true,
		},
		{
			name:             "webhook run-only",
			taskID:           "cccc1111-2222-3333-4444-555566667777",
			task:             TaskContextForEnv{AutopilotRunID: "run-webhook", AutopilotSource: "webhook"},
			wantFigmaEnabled: true,
		},
		{
			name:             "regular issue",
			taskID:           "dddd1111-2222-3333-4444-555566667777",
			task:             TaskContextForEnv{IssueID: "issue-1"},
			wantFigmaEnabled: true,
		},
		{
			name:             "regular chat",
			taskID:           "eeee1111-2222-3333-4444-555566667777",
			task:             TaskContextForEnv{ChatSessionID: "chat-1"},
			wantFigmaEnabled: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.task.AgentSkills = []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
			env, err := Prepare(PrepareParams{
				WorkspacesRoot:   t.TempDir(),
				WorkspaceID:      "ws-hermes-figma",
				TaskID:           tt.taskID,
				Provider:         "hermes",
				HermesSourceHome: sharedHome,
				Task:             tt.task,
			}, testLogger())
			if err != nil {
				t.Fatalf("Prepare failed: %v", err)
			}
			defer env.Cleanup(true)

			servers := readServers(filepath.Join(env.HermesHome, "config.yaml"))
			figma := servers["FiGmA"]
			if figma.Enabled == nil || *figma.Enabled != tt.wantFigmaEnabled {
				t.Errorf("Figma enabled = %v, want %t", figma.Enabled, tt.wantFigmaEnabled)
			}
			if figma.Transport != "http" || figma.URL != "https://mcp.figma.example" || figma.Auth != "oauth" || figma.CustomSetting != "keep-me" {
				t.Errorf("Figma fields changed: %+v", figma)
			}
			github := servers["github"]
			if github.Enabled == nil || !*github.Enabled || github.Command != "github-mcp" || strings.Join(github.Args, " ") != "serve" {
				t.Errorf("unrelated MCP changed: %+v", github)
			}
		})
	}

	if data, err := os.ReadFile(filepath.Join(sharedHome, "config.yaml")); err != nil {
		t.Fatalf("read host config after prepare: %v", err)
	} else if string(data) != hostConfig {
		t.Error("host config was modified")
	}
}

func TestPrepareHermesScheduledRunOnlyFailsClosedOnInvalidConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config string
	}{
		{name: "malformed YAML", config: "mcp_servers:\n  figma: [\n"},
		{name: "non-mapping root", config: "- mcp_servers\n- figma\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sharedHome := t.TempDir()
			mustWrite(t, filepath.Join(sharedHome, "config.yaml"), tt.config)
			task := TaskContextForEnv{
				AutopilotRunID:  "scheduled-run",
				AutopilotSource: "schedule",
				AgentSkills:     []SkillContextForEnv{{Name: "Review Helper", Content: "x"}},
			}
			if env, err := Prepare(PrepareParams{
				WorkspacesRoot:   t.TempDir(),
				WorkspaceID:      "ws-hermes-invalid",
				TaskID:           "aaaa1111-2222-3333-4444-555566667777",
				Provider:         "hermes",
				HermesSourceHome: sharedHome,
				Task:             task,
			}, testLogger()); err == nil {
				env.Cleanup(true)
				t.Fatal("scheduled task must fail closed when Figma isolation cannot be applied")
			}

			// Preserve the legacy non-scheduled fallback: malformed/non-mapping
			// source config is copied verbatim rather than blocking the task.
			task.AutopilotRunID = ""
			task.AutopilotSource = ""
			task.IssueID = "issue-1"
			env, err := Prepare(PrepareParams{
				WorkspacesRoot:   t.TempDir(),
				WorkspaceID:      "ws-hermes-invalid",
				TaskID:           "bbbb1111-2222-3333-4444-555566667777",
				Provider:         "hermes",
				HermesSourceHome: sharedHome,
				Task:             task,
			}, testLogger())
			if err != nil {
				t.Fatalf("regular task should retain legacy verbatim fallback: %v", err)
			}
			defer env.Cleanup(true)
			if data, err := os.ReadFile(filepath.Join(env.HermesHome, "config.yaml")); err != nil {
				t.Fatalf("read regular derived config: %v", err)
			} else if string(data) != tt.config {
				t.Errorf("regular derived config = %q, want verbatim source %q", data, tt.config)
			}
			if data, err := os.ReadFile(filepath.Join(sharedHome, "config.yaml")); err != nil {
				t.Fatalf("read source config: %v", err)
			} else if string(data) != tt.config {
				t.Error("source config was modified")
			}
		})
	}
}

func TestPrepareHermesScheduledRunOnlyMaterializesFigmaAlias(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	hostConfig := `shared_mcp: &shared_mcp
  transport: http
  auth: oauth
  enabled: true
  custom_setting: keep-me
mcp_servers:
  figma: *shared_mcp
  unrelated: *shared_mcp
`
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), hostConfig)
	task := TaskContextForEnv{
		AutopilotRunID:  "scheduled-run",
		AutopilotSource: "schedule",
		AgentSkills:     []SkillContextForEnv{{Name: "Review Helper", Content: "x"}},
	}
	env, err := Prepare(PrepareParams{
		WorkspacesRoot:   t.TempDir(),
		WorkspaceID:      "ws-hermes-alias",
		TaskID:           "aaaa1111-2222-3333-4444-555566667777",
		Provider:         "hermes",
		HermesSourceHome: sharedHome,
		Task:             task,
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	data, err := os.ReadFile(filepath.Join(env.HermesHome, "config.yaml"))
	if err != nil {
		t.Fatalf("read derived config: %v", err)
	}
	var parsed struct {
		MCPServers map[string]struct {
			Enabled       bool   `yaml:"enabled"`
			Transport     string `yaml:"transport"`
			Auth          string `yaml:"auth"`
			CustomSetting string `yaml:"custom_setting"`
		} `yaml:"mcp_servers"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse derived config: %v", err)
	}
	if got := parsed.MCPServers["figma"]; got.Enabled || got.Transport != "http" || got.Auth != "oauth" || got.CustomSetting != "keep-me" {
		t.Errorf("materialized Figma = %+v, want preserved fields with enabled false", got)
	}
	if got := parsed.MCPServers["unrelated"]; !got.Enabled || got.Transport != "http" || got.Auth != "oauth" || got.CustomSetting != "keep-me" {
		t.Errorf("unrelated anchored MCP changed: %+v", got)
	}
	if data, err := os.ReadFile(filepath.Join(sharedHome, "config.yaml")); err != nil {
		t.Fatalf("read source config: %v", err)
	} else if string(data) != hostConfig {
		t.Error("source config was modified")
	}
}

func TestPrepareHermesScheduledRunOnlyFailsClosedOnNonMappingFigmaAlias(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	hostConfig := `shared_command: &shared_command figma-mcp
mcp_servers:
  figma: *shared_command
`
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), hostConfig)
	task := TaskContextForEnv{
		AutopilotRunID:  "scheduled-run",
		AutopilotSource: "schedule",
		AgentSkills:     []SkillContextForEnv{{Name: "Review Helper", Content: "x"}},
	}
	if env, err := Prepare(PrepareParams{
		WorkspacesRoot:   t.TempDir(),
		WorkspaceID:      "ws-hermes-invalid-alias",
		TaskID:           "aaaa1111-2222-3333-4444-555566667777",
		Provider:         "hermes",
		HermesSourceHome: sharedHome,
		Task:             task,
	}, testLogger()); err == nil {
		env.Cleanup(true)
		t.Fatal("scheduled task must fail closed on non-mapping Figma alias")
	}
	if data, err := os.ReadFile(filepath.Join(sharedHome, "config.yaml")); err != nil {
		t.Fatalf("read source config: %v", err)
	} else if string(data) != hostConfig {
		t.Error("source config was modified")
	}
}

func TestPrepareHermesScheduledRunOnlyHandlesMCPServersShape(t *testing.T) {
	t.Parallel()
	scheduledTask := TaskContextForEnv{
		AutopilotRunID:  "scheduled-run",
		AutopilotSource: "schedule",
		AgentSkills:     []SkillContextForEnv{{Name: "Review Helper", Content: "x"}},
	}
	prepare := func(t *testing.T, hostConfig string) (*Environment, string) {
		t.Helper()
		sharedHome := t.TempDir()
		mustWrite(t, filepath.Join(sharedHome, "config.yaml"), hostConfig)
		env, err := Prepare(PrepareParams{
			WorkspacesRoot:   t.TempDir(),
			WorkspaceID:      "ws-hermes-servers-shape",
			TaskID:           "aaaa1111-2222-3333-4444-555566667777",
			Provider:         "hermes",
			HermesSourceHome: sharedHome,
			Task:             scheduledTask,
		}, testLogger())
		if err != nil {
			t.Fatalf("Prepare failed: %v", err)
		}
		t.Cleanup(func() { env.Cleanup(true) })
		return env, sharedHome
	}

	t.Run("mapping alias is materialized", func(t *testing.T) {
		hostConfig := `shared_servers: &shared_servers
  figma:
    enabled: true
    custom_setting: keep-me
  unrelated:
    enabled: true
mcp_servers: *shared_servers
`
		env, sharedHome := prepare(t, hostConfig)
		data, err := os.ReadFile(filepath.Join(env.HermesHome, "config.yaml"))
		if err != nil {
			t.Fatalf("read derived config: %v", err)
		}
		var parsed struct {
			SharedServers map[string]struct {
				Enabled bool `yaml:"enabled"`
			} `yaml:"shared_servers"`
			MCPServers map[string]struct {
				Enabled       bool   `yaml:"enabled"`
				CustomSetting string `yaml:"custom_setting"`
			} `yaml:"mcp_servers"`
		}
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("parse derived config: %v", err)
		}
		if got := parsed.MCPServers["figma"]; got.Enabled || got.CustomSetting != "keep-me" {
			t.Errorf("materialized Figma = %+v, want enabled false with fields preserved", got)
		}
		if !parsed.MCPServers["unrelated"].Enabled {
			t.Error("unrelated MCP was disabled")
		}
		if !parsed.SharedServers["figma"].Enabled || !parsed.SharedServers["unrelated"].Enabled {
			t.Error("shared anchor was mutated")
		}
		if source, err := os.ReadFile(filepath.Join(sharedHome, "config.yaml")); err != nil {
			t.Fatalf("read source config: %v", err)
		} else if string(source) != hostConfig {
			t.Error("source config was modified")
		}
	})

	for _, tt := range []struct {
		name   string
		config string
	}{
		{name: "scalar", config: "mcp_servers: disabled\n"},
		{name: "sequence", config: "mcp_servers: [figma]\n"},
		{name: "non-mapping alias", config: "shared: &shared disabled\nmcp_servers: *shared\n"},
	} {
		t.Run(tt.name+" fails closed", func(t *testing.T) {
			sharedHome := t.TempDir()
			mustWrite(t, filepath.Join(sharedHome, "config.yaml"), tt.config)
			if env, err := Prepare(PrepareParams{
				WorkspacesRoot:   t.TempDir(),
				WorkspaceID:      "ws-hermes-servers-shape-invalid",
				TaskID:           "bbbb1111-2222-3333-4444-555566667777",
				Provider:         "hermes",
				HermesSourceHome: sharedHome,
				Task:             scheduledTask,
			}, testLogger()); err == nil {
				env.Cleanup(true)
				t.Fatal("scheduled task must fail closed on present non-mapping mcp_servers")
			}
			if source, err := os.ReadFile(filepath.Join(sharedHome, "config.yaml")); err != nil {
				t.Fatalf("read source config: %v", err)
			} else if string(source) != tt.config {
				t.Error("source config was modified")
			}
		})
	}

	t.Run("missing key is safe no-op", func(t *testing.T) {
		hostConfig := "model: hermes-4\n"
		env, sharedHome := prepare(t, hostConfig)
		if _, err := os.Stat(filepath.Join(env.HermesHome, "config.yaml")); err != nil {
			t.Fatalf("derived config missing: %v", err)
		}
		if source, err := os.ReadFile(filepath.Join(sharedHome, "config.yaml")); err != nil {
			t.Fatalf("read source config: %v", err)
		} else if string(source) != hostConfig {
			t.Error("source config was modified")
		}
	})
}

func TestPrepareHermesScheduledRunOnlyMaterializesNestedAliases(t *testing.T) {
	t.Parallel()
	scheduledTask := TaskContextForEnv{
		AutopilotRunID:  "scheduled-run",
		AutopilotSource: "schedule",
		AgentSkills:     []SkillContextForEnv{{Name: "Review Helper", Content: "x"}},
	}
	tests := []struct {
		name       string
		hostConfig string
	}{
		{
			name: "servers alias contains Figma entry alias",
			hostConfig: `shared_entry: &shared_entry
  enabled: true
  transport: http
  custom_setting: keep-me
shared_servers: &shared_servers
  figma: *shared_entry
  unrelated: *shared_entry
mcp_servers: *shared_servers
`,
		},
		{
			name: "Figma entry alias contains nested headers alias",
			hostConfig: `shared_headers: &shared_headers
  Authorization: Bearer-placeholder
  X-Custom: keep-me
shared_entry: &shared_entry
  enabled: true
  transport: http
  headers: *shared_headers
mcp_servers:
  figma: *shared_entry
  unrelated:
    enabled: true
    headers: *shared_headers
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sharedHome := t.TempDir()
			mustWrite(t, filepath.Join(sharedHome, "config.yaml"), tt.hostConfig)
			env, err := Prepare(PrepareParams{
				WorkspacesRoot:   t.TempDir(),
				WorkspaceID:      "ws-hermes-nested-alias",
				TaskID:           "aaaa1111-2222-3333-4444-555566667777",
				Provider:         "hermes",
				HermesSourceHome: sharedHome,
				Task:             scheduledTask,
			}, testLogger())
			if err != nil {
				t.Fatalf("Prepare failed: %v", err)
			}
			defer env.Cleanup(true)

			data, err := os.ReadFile(filepath.Join(env.HermesHome, "config.yaml"))
			if err != nil {
				t.Fatalf("read derived config: %v", err)
			}
			var parsed struct {
				SharedEntry struct {
					Enabled bool `yaml:"enabled"`
				} `yaml:"shared_entry"`
				SharedHeaders map[string]string `yaml:"shared_headers"`
				MCPServers    map[string]struct {
					Enabled       bool              `yaml:"enabled"`
					Transport     string            `yaml:"transport"`
					CustomSetting string            `yaml:"custom_setting"`
					Headers       map[string]string `yaml:"headers"`
				} `yaml:"mcp_servers"`
			}
			if err := yaml.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("parse derived config: %v\n%s", err, data)
			}
			figma := parsed.MCPServers["figma"]
			if figma.Enabled || figma.Transport != "http" {
				t.Errorf("materialized Figma = %+v, want enabled false with fields preserved", figma)
			}
			if strings.Contains(tt.name, "servers alias") {
				if figma.CustomSetting != "keep-me" || !parsed.MCPServers["unrelated"].Enabled || parsed.MCPServers["unrelated"].CustomSetting != "keep-me" {
					t.Errorf("shared entry fields changed: figma=%+v unrelated=%+v", figma, parsed.MCPServers["unrelated"])
				}
			} else {
				wantHeaders := map[string]string{"Authorization": "Bearer-placeholder", "X-Custom": "keep-me"}
				if figma.Headers["Authorization"] != wantHeaders["Authorization"] || figma.Headers["X-Custom"] != wantHeaders["X-Custom"] {
					t.Errorf("nested Figma headers = %v, want %v", figma.Headers, wantHeaders)
				}
				if unrelated := parsed.MCPServers["unrelated"]; !unrelated.Enabled || unrelated.Headers["X-Custom"] != "keep-me" {
					t.Errorf("unrelated shared headers changed: %+v", unrelated)
				}
				if parsed.SharedHeaders["Authorization"] != "Bearer-placeholder" || parsed.SharedHeaders["X-Custom"] != "keep-me" {
					t.Errorf("shared headers anchor changed: %v", parsed.SharedHeaders)
				}
			}
			if !parsed.SharedEntry.Enabled {
				t.Error("shared entry anchor was mutated")
			}
			if source, err := os.ReadFile(filepath.Join(sharedHome, "config.yaml")); err != nil {
				t.Fatalf("read source config: %v", err)
			} else if string(source) != tt.hostConfig {
				t.Error("source config was modified")
			}
		})
	}
}

func TestPrepareHermesScheduledRunOnlyFailsClosedOnCyclicAlias(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	hostConfig := `mcp_servers: &servers
  figma: *servers
`
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), hostConfig)
	task := TaskContextForEnv{
		AutopilotRunID:  "scheduled-run",
		AutopilotSource: "schedule",
		AgentSkills:     []SkillContextForEnv{{Name: "Review Helper", Content: "x"}},
	}
	if env, err := Prepare(PrepareParams{
		WorkspacesRoot:   t.TempDir(),
		WorkspaceID:      "ws-hermes-cyclic-alias",
		TaskID:           "aaaa1111-2222-3333-4444-555566667777",
		Provider:         "hermes",
		HermesSourceHome: sharedHome,
		Task:             task,
	}, testLogger()); err == nil {
		env.Cleanup(true)
		t.Fatal("scheduled task must fail closed on cyclic alias")
	}
	if source, err := os.ReadFile(filepath.Join(sharedHome, "config.yaml")); err != nil {
		t.Fatalf("read source config: %v", err)
	} else if string(source) != hostConfig {
		t.Error("source config was modified")
	}
}

func TestPrepareHermesScheduledRunOnlyMaterializesMCPServerMerges(t *testing.T) {
	t.Parallel()
	scheduledTask := TaskContextForEnv{
		AutopilotRunID:  "scheduled-run",
		AutopilotSource: "schedule",
		AgentSkills:     []SkillContextForEnv{{Name: "Review Helper", Content: "x"}},
	}
	tests := []struct {
		name       string
		hostConfig string
		wantMarker string
		wantLocal  string
	}{
		{
			name: "single mapping merge",
			hostConfig: `shared_servers: &shared_servers
  figma:
    enabled: true
    marker: shared
  unrelated:
    enabled: true
    marker: shared-unrelated
mcp_servers:
  <<: *shared_servers
  local:
    enabled: true
    marker: explicit-local
`,
			wantMarker: "shared",
			wantLocal:  "explicit-local",
		},
		{
			name: "merge sequence earlier wins and explicit overrides",
			hostConfig: `earlier: &earlier
  figma:
    enabled: true
    marker: earlier
  unrelated:
    enabled: true
    marker: earlier-unrelated
later: &later
  figma:
    enabled: true
    marker: later
  unrelated:
    enabled: false
    marker: later-unrelated
  later_only:
    enabled: true
    marker: from-later
mcp_servers:
  <<: [*earlier, *later]
  local:
    enabled: true
    marker: explicit-local
  unrelated:
    enabled: true
    marker: explicit-unrelated
`,
			wantMarker: "earlier",
			wantLocal:  "explicit-local",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sharedHome := t.TempDir()
			mustWrite(t, filepath.Join(sharedHome, "config.yaml"), tt.hostConfig)
			env, err := Prepare(PrepareParams{
				WorkspacesRoot:   t.TempDir(),
				WorkspaceID:      "ws-hermes-merge",
				TaskID:           "aaaa1111-2222-3333-4444-555566667777",
				Provider:         "hermes",
				HermesSourceHome: sharedHome,
				Task:             scheduledTask,
			}, testLogger())
			if err != nil {
				t.Fatalf("Prepare failed: %v", err)
			}
			defer env.Cleanup(true)

			data, err := os.ReadFile(filepath.Join(env.HermesHome, "config.yaml"))
			if err != nil {
				t.Fatalf("read derived config: %v", err)
			}
			var doc yaml.Node
			if err := yaml.Unmarshal(data, &doc); err != nil {
				t.Fatalf("parse derived config node: %v", err)
			}
			serversNode := yamlMapValue(yamlDocumentRoot(&doc), "mcp_servers")
			if yamlMapValue(serversNode, "<<") != nil {
				t.Fatal("derived mcp_servers must be materialized without merge key")
			}
			var parsed struct {
				MCPServers map[string]struct {
					Enabled bool   `yaml:"enabled"`
					Marker  string `yaml:"marker"`
				} `yaml:"mcp_servers"`
			}
			if err := yaml.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("parse derived config: %v", err)
			}
			if got := parsed.MCPServers["figma"]; got.Enabled || got.Marker != tt.wantMarker {
				t.Errorf("Figma = %+v, want enabled false marker %q", got, tt.wantMarker)
			}
			if got := parsed.MCPServers["local"]; !got.Enabled || got.Marker != tt.wantLocal {
				t.Errorf("explicit local MCP changed: %+v", got)
			}
			if tt.name == "single mapping merge" {
				if got := parsed.MCPServers["unrelated"]; !got.Enabled || got.Marker != "shared-unrelated" {
					t.Errorf("merged unrelated MCP changed: %+v", got)
				}
			} else {
				if got := parsed.MCPServers["unrelated"]; !got.Enabled || got.Marker != "explicit-unrelated" {
					t.Errorf("explicit key did not override merged value: %+v", got)
				}
				if got := parsed.MCPServers["later_only"]; !got.Enabled || got.Marker != "from-later" {
					t.Errorf("later-only merged key missing: %+v", got)
				}
			}
			if source, err := os.ReadFile(filepath.Join(sharedHome, "config.yaml")); err != nil {
				t.Fatalf("read source config: %v", err)
			} else if string(source) != tt.hostConfig {
				t.Error("source config was modified")
			}
		})
	}
}

func TestPrepareHermesScheduledRunOnlyFailsClosedOnInvalidMCPServerMerge(t *testing.T) {
	t.Parallel()
	scheduledTask := TaskContextForEnv{
		AutopilotRunID:  "scheduled-run",
		AutopilotSource: "schedule",
		AgentSkills:     []SkillContextForEnv{{Name: "Review Helper", Content: "x"}},
	}
	tests := []struct {
		name       string
		hostConfig string
	}{
		{name: "scalar merge", hostConfig: "mcp_servers:\n  <<: invalid\n"},
		{name: "wrong-kind alias merge", hostConfig: "shared: &shared invalid\nmcp_servers:\n  <<: *shared\n"},
		{name: "wrong-kind merge sequence item", hostConfig: "valid: &valid {figma: {enabled: true}}\ninvalid: &invalid nope\nmcp_servers:\n  <<: [*valid, *invalid]\n"},
		{name: "cyclic merge", hostConfig: "mcp_servers: &servers\n  <<: *servers\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sharedHome := t.TempDir()
			mustWrite(t, filepath.Join(sharedHome, "config.yaml"), tt.hostConfig)
			if env, err := Prepare(PrepareParams{
				WorkspacesRoot:   t.TempDir(),
				WorkspaceID:      "ws-hermes-invalid-merge",
				TaskID:           "aaaa1111-2222-3333-4444-555566667777",
				Provider:         "hermes",
				HermesSourceHome: sharedHome,
				Task:             scheduledTask,
			}, testLogger()); err == nil {
				env.Cleanup(true)
				t.Fatal("scheduled task must fail closed on invalid mcp_servers merge")
			}
			if source, err := os.ReadFile(filepath.Join(sharedHome, "config.yaml")); err != nil {
				t.Fatalf("read source config: %v", err)
			} else if string(source) != tt.hostConfig {
				t.Error("source config was modified")
			}
		})
	}
}

func TestHermesHomeOptionsForTaskAndReuseTransitions(t *testing.T) {
	t.Parallel()
	if opts := hermesHomeOptionsForTask(TaskContextForEnv{AutopilotRunID: "run-1", AutopilotSource: "schedule"}); !opts.disableFigmaMCP {
		t.Fatal("scheduled run-only task should disable Figma")
	}
	for _, task := range []TaskContextForEnv{
		{AutopilotSource: "schedule"},
		{AutopilotRunID: "run-1", AutopilotSource: "manual"},
		{AutopilotRunID: "run-1", AutopilotSource: "Schedule"},
		{IssueID: "issue-1"},
	} {
		if opts := hermesHomeOptionsForTask(task); opts.disableFigmaMCP {
			t.Errorf("task %+v should preserve Figma", task)
		}
	}

	sharedHome := t.TempDir()
	hostConfig := `mcp_servers:
  figma:
    enabled: true
    custom_setting: keep-me
  unrelated:
    enabled: true
`
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), hostConfig)
	manualTask := TaskContextForEnv{
		AutopilotRunID:  "manual-run",
		AutopilotSource: "manual",
		AgentSkills:     []SkillContextForEnv{{Name: "Review Helper", Content: "x"}},
	}
	env, err := Prepare(PrepareParams{
		WorkspacesRoot:   t.TempDir(),
		WorkspaceID:      "ws-hermes-reuse-policy",
		TaskID:           "aaaa1111-2222-3333-4444-555566667777",
		Provider:         "hermes",
		HermesSourceHome: sharedHome,
		Task:             manualTask,
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare manual task failed: %v", err)
	}
	defer env.Cleanup(true)

	assertEnabled := func(wantFigma bool) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(env.HermesHome, "config.yaml"))
		if err != nil {
			t.Fatalf("read derived config: %v", err)
		}
		var parsed struct {
			MCPServers map[string]struct {
				Enabled       bool   `yaml:"enabled"`
				CustomSetting string `yaml:"custom_setting"`
			} `yaml:"mcp_servers"`
		}
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("parse derived config: %v", err)
		}
		if got := parsed.MCPServers["figma"]; got.Enabled != wantFigma || got.CustomSetting != "keep-me" {
			t.Errorf("Figma after reuse enabled=%t setting=%q, want enabled=%t setting=keep-me", got.Enabled, got.CustomSetting, wantFigma)
		}
		if !parsed.MCPServers["unrelated"].Enabled {
			t.Error("unrelated MCP disabled during reuse transition")
		}
	}
	assertEnabled(true)

	scheduledTask := manualTask
	scheduledTask.AutopilotRunID = "scheduled-run"
	scheduledTask.AutopilotSource = "schedule"
	if reused := Reuse(ReuseParams{
		WorkDir:          env.WorkDir,
		Provider:         "hermes",
		HermesSourceHome: sharedHome,
		Task:             scheduledTask,
	}, testLogger()); reused == nil {
		t.Fatal("Reuse manual to scheduled returned nil")
	}
	assertEnabled(false)

	if reused := Reuse(ReuseParams{
		WorkDir:          env.WorkDir,
		Provider:         "hermes",
		HermesSourceHome: sharedHome,
		Task:             manualTask,
	}, testLogger()); reused == nil {
		t.Fatal("Reuse scheduled to manual returned nil")
	}
	assertEnabled(true)

	if data, err := os.ReadFile(filepath.Join(sharedHome, "config.yaml")); err != nil {
		t.Fatalf("read source config: %v", err)
	} else if string(data) != hostConfig {
		t.Error("source config was modified")
	}
}

// TestHermesDerivedConfigRebasesRelativeExternalDirs is the regression for the
// silent-repoint bug: relative external_dirs must be rewritten to absolute paths
// anchored at the shared home, absolute entries left intact, and the real skills
// dir appended.
func TestHermesDerivedConfigRebasesRelativeExternalDirs(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"),
		"model: hermes-4\nskills:\n  external_dirs:\n    - team-skills\n    - /opt/shared/skills\n")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	got := hermesExternalDirs(t, filepath.Join(hermesHome, "config.yaml"))
	want := []string{
		filepath.Join(sharedHome, "team-skills"),
		"/opt/shared/skills",
		filepath.Join(sharedHome, "skills"),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("external_dirs =\n%v\nwant\n%v", got, want)
	}
}

// TestHermesExternalDirsExpandsSanitizedEnv verifies a ${VAR} present in the
// sanitized effective env expands, while an UNKNOWN var is preserved verbatim
// (Hermes/Python expandvars semantics) instead of collapsing to empty and being
// silently rewritten to an absolute path.
func TestHermesExternalDirsExpandsSanitizedEnv(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"),
		"skills:\n  external_dirs:\n    - ${TEAM_SKILLS}/reviews\n    - ${MYSTERY_VAR}/x\n")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	env := map[string]string{"TEAM_SKILLS": "/srv/team"} // MYSTERY_VAR set nowhere
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, env, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	got := hermesExternalDirs(t, filepath.Join(hermesHome, "config.yaml"))
	want := []string{
		"/srv/team/reviews", // known var expanded
		"${MYSTERY_VAR}/x",  // unknown var preserved verbatim, NOT absolutized
		filepath.Join(sharedHome, "skills"),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("external_dirs =\n%v\nwant\n%v", got, want)
	}
}

// TestHermesBoundSkillKeepsNaturalSlug asserts a bound skill sharing a name with
// a user global skill keeps its natural slug (so Hermes resolves the bound
// version, home skills first) and leaves the user's shared copy untouched.
func TestHermesBoundSkillKeepsNaturalSlug(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	userSkill := filepath.Join(sharedHome, "skills", "review-helper", "SKILL.md")
	mustWrite(t, userSkill, "USER VERSION")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "WORKSPACE VERSION"}}
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(hermesHome, "skills", "review-helper", "SKILL.md"))
	if err != nil {
		t.Fatalf("read bound skill: %v", err)
	}
	if strings.Contains(string(body), "USER VERSION") || !strings.Contains(string(body), "WORKSPACE VERSION") {
		t.Errorf("bound skill should keep natural slug with its own content, got: %q", body)
	}
	if data, _ := os.ReadFile(userSkill); string(data) != "USER VERSION" {
		t.Errorf("user's shared skill was modified: %q", data)
	}
}

// TestHermesOverlayIsolatesMemories asserts the host memory dir isn't reachable
// from the task, task writes don't touch the host, and the task-local dir
// survives reuse.
func TestHermesOverlayIsolatesMemories(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), "model: hermes-4\n")
	mustWrite(t, filepath.Join(sharedHome, "memories", "MEMORY.md"), "HOST MEMORY — must not leak")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	memDir := filepath.Join(hermesHome, "memories")
	if fi, err := os.Lstat(memDir); err != nil {
		t.Fatalf("task memories dir missing: %v", err)
	} else if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("task memories/ must be a real dir, not a symlink into the host home")
	}
	if _, err := os.Stat(filepath.Join(memDir, "MEMORY.md")); !os.IsNotExist(err) {
		t.Error("host MEMORY.md must not be visible in the task")
	}

	mustWrite(t, filepath.Join(memDir, "MEMORY.md"), "TASK MEMORY")
	if data, _ := os.ReadFile(filepath.Join(sharedHome, "memories", "MEMORY.md")); string(data) != "HOST MEMORY — must not leak" {
		t.Errorf("host memory was modified through the overlay: %q", data)
	}
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome (reuse) failed: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(memDir, "MEMORY.md")); string(data) != "TASK MEMORY" {
		t.Errorf("reuse should preserve task-local memory, got: %q", data)
	}
}

// TestHermesOverlayKeepsSessionDatabaseTaskLocal verifies the live SQLite
// session store is never mirrored from the host. Hermes creates it lazily in
// the task home, and reuse must preserve that task-local database instead of
// replacing it with a fresh host snapshot.
func TestHermesOverlayKeepsSessionDatabaseTaskLocal(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), "model: hermes-4\n")
	stateFiles := []string{"state.db", "state.db-wal", "state.db-shm", "state.db-journal"}
	for _, name := range stateFiles {
		mustWrite(t, filepath.Join(sharedHome, name), "HOST "+name)
	}

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}
	for _, name := range stateFiles {
		if _, err := os.Lstat(filepath.Join(hermesHome, name)); !os.IsNotExist(err) {
			t.Fatalf("host %s must not be mirrored into a fresh task overlay: %v", name, err)
		}
		mustWrite(t, filepath.Join(hermesHome, name), "TASK "+name)
		mustWrite(t, filepath.Join(sharedHome, name), "UPDATED HOST "+name)
	}

	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome (reuse) failed: %v", err)
	}
	for _, name := range stateFiles {
		data, err := os.ReadFile(filepath.Join(hermesHome, name))
		if err != nil {
			t.Fatalf("read task-local %s after reuse: %v", name, err)
		}
		if got, want := string(data), "TASK "+name; got != want {
			t.Errorf("task-local %s after reuse = %q, want %q", name, got, want)
		}
	}
}

// TestHermesOverlayMigratesLegacySessionDatabase verifies an overlay created by
// an older daemon drops potentially inconsistent copied SQLite files once,
// without touching the host, then preserves the database Hermes creates locally.
func TestHermesOverlayMigratesLegacySessionDatabase(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), "model: hermes-4\n")
	stateFiles := []string{"state.db", "state.db-wal", "state.db-shm", "state.db-journal"}
	for _, name := range stateFiles {
		mustWrite(t, filepath.Join(sharedHome, name), "HOST "+name)
	}

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	for _, name := range stateFiles {
		mustWrite(t, filepath.Join(hermesHome, name), "LEGACY COPY "+name)
	}
	// POSIX overlays used symlinks while Windows without symlink privilege used
	// copies. Exercise the symlink migration when the host permits creating one.
	walPath := filepath.Join(hermesHome, "state.db-wal")
	if err := os.Remove(walPath); err != nil {
		t.Fatalf("remove copied WAL before symlink setup: %v", err)
	}
	if err := os.Symlink(filepath.Join(sharedHome, "state.db-wal"), walPath); err != nil {
		mustWrite(t, walPath, "LEGACY COPY state.db-wal")
	}
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}
	for _, name := range stateFiles {
		if _, err := os.Lstat(filepath.Join(hermesHome, name)); !os.IsNotExist(err) {
			t.Errorf("legacy overlay %s should be removed during migration: %v", name, err)
		}
		data, err := os.ReadFile(filepath.Join(sharedHome, name))
		if err != nil {
			t.Fatalf("read host %s after migration: %v", name, err)
		}
		if got, want := string(data), "HOST "+name; got != want {
			t.Errorf("host %s changed during migration: got %q, want %q", name, got, want)
		}
	}

	mustWrite(t, filepath.Join(hermesHome, "state.db"), "TASK DB")
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome (reuse) failed: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(hermesHome, "state.db")); err != nil {
		t.Fatalf("read migrated task-local state.db after reuse: %v", err)
	} else if got := string(data); got != "TASK DB" {
		t.Errorf("migrated task-local state.db after reuse = %q, want TASK DB", got)
	}
}

// TestHermesOverlayPermissions asserts the task home is 0700 and the derived
// config (which can hold inline api_key secrets) is 0600.
func TestHermesOverlayPermissions(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), "model: hermes-4\napi_key: sk-secret\n")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	if fi, err := os.Stat(hermesHome); err != nil {
		t.Fatalf("stat home: %v", err)
	} else if fi.Mode().Perm() != 0o700 {
		t.Errorf("task home perms = %o, want 0700", fi.Mode().Perm())
	}
	if fi, err := os.Stat(filepath.Join(hermesHome, "config.yaml")); err != nil {
		t.Fatalf("stat config: %v", err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Errorf("derived config perms = %o, want 0600", fi.Mode().Perm())
	}
	if fi, err := os.Stat(filepath.Join(hermesHome, hermesTaskLocalStateMarker)); err != nil {
		t.Fatalf("stat task-local state marker: %v", err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Errorf("task-local state marker perms = %o, want 0600", fi.Mode().Perm())
	}
}

// TestHermesOverlayReconcilesDeletedSharedEntry asserts a top-level entry
// removed from the shared home is dropped from the overlay on rebuild.
func TestHermesOverlayReconcilesDeletedSharedEntry(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), "model: hermes-4\n")
	mustWrite(t, filepath.Join(sharedHome, "plugins", "p", "plugin.py"), "# plugin")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(hermesHome, "plugins")); err != nil {
		t.Fatalf("plugins not mirrored: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(sharedHome, "plugins")); err != nil {
		t.Fatalf("remove shared plugins: %v", err)
	}
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome (rebuild) failed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(hermesHome, "plugins")); !os.IsNotExist(err) {
		t.Error("stale mirrored plugins should be reconciled away after deletion in the shared home")
	}
}

// TestPrepareHermesHomeFailsClosed asserts prepareHermesHome returns an error
// when required overlay state can't be built (here an unreadable shared config).
func TestPrepareHermesHomeFailsClosed(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sharedHome, "config.yaml"), 0o755); err != nil {
		t.Fatalf("mkdir config.yaml dir: %v", err)
	}
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err == nil {
		t.Fatal("expected prepareHermesHome to fail closed on an unreadable shared config")
	}
}

// TestResolveHermesProfile exercises the one resolver contract against the
// behaviors the review requires it to match Hermes on: sticky selection, an
// already-profile-scoped home, `-p default`/`-p <sibling>` re-rooting, and native
// failure on reserved/invalid/empty names. Temp dirs stand in for custom Hermes
// roots (they are never under the host's platform-default home, so root
// derivation takes the deterministic custom/profile branch).
func TestResolveHermesProfile(t *testing.T) {
	t.Parallel()

	t.Run("no profile resolves to the base home", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		res := ResolveHermesProfile(base, "", false, false)
		if res.Err != nil || res.SourceHome != base || res.MustExist {
			t.Fatalf("got %+v, want SourceHome=%q MustExist=false Err=nil", res, base)
		}
	})

	// The review's blocker 1: a sticky active_profile must be SELECTED as the
	// overlay source, not merely blocked from bypassing.
	t.Run("root + sticky named profile selects the profile as source", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, "active_profile"), "coder\n")
		res := ResolveHermesProfile(root, "", false, false)
		want := filepath.Join(root, "profiles", "coder")
		if res.Err != nil || res.SourceHome != want || !res.MustExist {
			t.Fatalf("sticky: got %+v, want SourceHome=%q MustExist=true", res, want)
		}
	})

	t.Run("sticky default is ignored", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, "active_profile"), "default\n")
		res := ResolveHermesProfile(root, "", false, false)
		if res.Err != nil || res.SourceHome != root || res.MustExist {
			t.Fatalf("sticky default: got %+v, want SourceHome=%q MustExist=false", res, root)
		}
	})

	t.Run("already-profile-scoped home with no flag is trusted", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		scoped := filepath.Join(root, "profiles", "coder")
		// A sticky at the root must NOT override an explicit profile-scoped home.
		mustWrite(t, filepath.Join(root, "active_profile"), "research\n")
		res := ResolveHermesProfile(scoped, "", false, false)
		if res.Err != nil || res.SourceHome != scoped || !res.MustExist {
			t.Fatalf("profile-scoped: got %+v, want SourceHome=%q MustExist=true", res, scoped)
		}
	})

	t.Run("-p default from a profile-scoped home re-roots to the root", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		scoped := filepath.Join(root, "profiles", "coder")
		res := ResolveHermesProfile(scoped, "default", true, false)
		if res.Err != nil || res.SourceHome != root || res.MustExist {
			t.Fatalf("-p default: got %+v, want SourceHome=%q MustExist=false", res, root)
		}
	})

	t.Run("-p sibling from a profile-scoped home is a sibling, not nested", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		scoped := filepath.Join(root, "profiles", "coder")
		res := ResolveHermesProfile(scoped, "research", true, false)
		want := filepath.Join(root, "profiles", "research")
		if res.Err != nil || res.SourceHome != want || !res.MustExist {
			t.Fatalf("-p sibling: got %+v, want SourceHome=%q MustExist=true", res, want)
		}
	})

	t.Run("explicit named profile resolves under the root", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		res := ResolveHermesProfile(root, "research", true, false)
		want := filepath.Join(root, "profiles", "research")
		if res.Err != nil || res.SourceHome != want || !res.MustExist {
			t.Fatalf("named: got %+v, want SourceHome=%q MustExist=true", res, want)
		}
	})

	t.Run("reserved names fail closed", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		for _, name := range []string{"hermes", "test", "tmp", "root", "sudo"} {
			if res := ResolveHermesProfile(root, name, true, false); res.Err == nil {
				t.Errorf("reserved %q: expected Err, got SourceHome=%q", name, res.SourceHome)
			}
		}
	})

	t.Run("empty inline value fails closed", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		if res := ResolveHermesProfile(root, "", true, true); res.Err == nil {
			t.Error("empty inline --profile= must fail closed, not fall back to default")
		}
	})

	t.Run("empty inputs resolve to a platform default", func(t *testing.T) {
		t.Parallel()
		if res := ResolveHermesProfile("", "", false, false); res.SourceHome == "" {
			t.Error("empty inputs should resolve to a platform default, not empty")
		}
	})
}

// TestHermesExternalDirsExpandsAgainstSelectedProfileHome is the review's blocker
// 3: a ${HERMES_HOME} in a selected profile's skills.external_dirs must expand
// against the SELECTED profile home (what native Hermes sees after applying the
// profile override before loading config.yaml), not the pre-resolution/root home
// or the task overlay. The daemon sets the effective env's HERMES_HOME to the
// resolved source home for exactly this reason.
func TestHermesExternalDirsExpandsAgainstSelectedProfileHome(t *testing.T) {
	t.Parallel()
	profileHome := t.TempDir() // stands in for <root>/profiles/coder
	mustWrite(t, filepath.Join(profileHome, "config.yaml"),
		"skills:\n  external_dirs:\n    - ${HERMES_HOME}/profile-skills\n")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	env := map[string]string{"HERMES_HOME": profileHome} // as the daemon sets it
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if _, err := prepareHermesHome(hermesHome, profileHome, true, skills, env, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	got := hermesExternalDirs(t, filepath.Join(hermesHome, "config.yaml"))
	want := []string{
		filepath.Join(profileHome, "profile-skills"),
		filepath.Join(profileHome, "skills"),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("external_dirs =\n%v\nwant\n%v", got, want)
	}
}

// TestHermesOverlayEnvPinsHomeAfterDotenvOverride is the review's blocker 1: a
// source .env that sets HERMES_HOME must not relocate the home past the overlay.
// Hermes loads <HERMES_HOME>/.env with override=True right after profile
// resolution; we replay that last-wins/override order over the derived overlay
// .env and prove the effective HERMES_HOME stays the overlay — so bound-skill
// discovery and task-local memory keep using it — while source creds survive.
func TestHermesOverlayEnvPinsHomeAfterDotenvOverride(t *testing.T) {
	t.Parallel()
	sourceHome := t.TempDir()
	mustWrite(t, filepath.Join(sourceHome, ".env"),
		"ANTHROPIC_API_KEY=sk-source\nexport HERMES_HOME=/home/u/.hermes/profiles/coder\n")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if _, err := prepareHermesHome(hermesHome, sourceHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	envPath := filepath.Join(hermesHome, ".env")
	if fi, err := os.Stat(envPath); err != nil {
		t.Fatalf(".env missing: %v", err)
	} else if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf(".env perms = %o, want 600 (holds credentials)", perm)
	}

	env := applyDotenvOverride(t, envPath)
	if env["HERMES_HOME"] != hermesHome {
		t.Errorf("after override=True dotenv load HERMES_HOME = %q, want the overlay %q", env["HERMES_HOME"], hermesHome)
	}
	if env["ANTHROPIC_API_KEY"] != "sk-source" {
		t.Errorf("source credential dropped: ANTHROPIC_API_KEY = %q", env["ANTHROPIC_API_KEY"])
	}
	// HERMES_HOME pinned to the overlay ⇒ skill discovery and memory use the
	// overlay's own dirs.
	if _, err := os.Stat(filepath.Join(hermesHome, "skills", "review-helper", "SKILL.md")); err != nil {
		t.Errorf("bound skill not in overlay skills dir: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(hermesHome, "memories")); err != nil || !fi.IsDir() {
		t.Errorf("overlay memories dir missing: %v", err)
	}
}

// TestHermesOverlayEnvCreatedWhenSourceHasNone ensures a minimal overlay .env is
// written even when the source has none, so Hermes' project-.env fallback (loaded
// with override=True only when no user .env loaded) can't relocate the home.
func TestHermesOverlayEnvCreatedWhenSourceHasNone(t *testing.T) {
	t.Parallel()
	sourceHome := t.TempDir() // no .env present
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if _, err := prepareHermesHome(hermesHome, sourceHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}
	env := applyDotenvOverride(t, filepath.Join(hermesHome, ".env"))
	if env["HERMES_HOME"] != hermesHome {
		t.Errorf("overlay .env must exist and pin HERMES_HOME even with no source .env; got %q", env["HERMES_HOME"])
	}
}

// applyDotenvOverride replays python-dotenv's single-file override=True load over
// a .env: KEY=VALUE lines in order, last assignment wins, surrounding quotes and
// a leading `export` stripped. It mirrors how Hermes loads <HERMES_HOME>/.env.
func applyDotenvOverride(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read overlay .env: %v", err)
	}
	env := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if rest := strings.TrimPrefix(s, "export"); rest != s && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
			s = strings.TrimSpace(rest)
		}
		eq := strings.IndexByte(s, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(s[:eq])
		val := strings.TrimSpace(s[eq+1:])
		if len(val) >= 2 && (val[0] == '\'' || val[0] == '"') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		env[key] = val // last wins (override=True)
	}
	return env
}

// TestHermesRootFromHomeResolvesSymlinks is the review's blocker 2: a HERMES_HOME
// symlinked into <native>/profiles/<x> must root at native (so -p default
// re-roots to native and -p <sibling> is a native sibling), which requires
// resolving symlinks for the containment decision — lexical containment alone
// would treat the symlink path as its own root.
func TestHermesRootFromHomeResolvesSymlinks(t *testing.T) {
	t.Parallel()
	native := t.TempDir()
	coder := filepath.Join(native, "profiles", "coder")
	if err := os.MkdirAll(coder, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "coder-home")
	if err := os.Symlink(coder, link); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}

	root := hermesRootFromHomeFor(link, native)
	if root != native {
		t.Fatalf("symlinked profile home: root = %q, want native %q", root, native)
	}
	if home, _, err := hermesProfileDir(root, "default"); err != nil || home != native {
		t.Fatalf("-p default: home=%q err=%v, want %q", home, err, native)
	}
	sibling := filepath.Join(native, "profiles", "research")
	if home, _, err := hermesProfileDir(root, "research"); err != nil || home != sibling {
		t.Fatalf("-p sibling: home=%q err=%v, want %q", home, err, sibling)
	}
}

// TestPlatformDefaultHermesHome covers the Windows branch off a Windows host,
// including the LOCALAPPDATA-missing fallback to %USERPROFILE%\AppData\Local.
func TestPlatformDefaultHermesHome(t *testing.T) {
	t.Parallel()
	la := filepath.Join(string(filepath.Separator)+"Users", "me", "AppData", "Local")
	home := filepath.Join(string(filepath.Separator)+"home", "me")

	if got, want := platformDefaultHermesHomeFor("windows", la, home), filepath.Join(la, "hermes"); got != want {
		t.Errorf("windows: got %q, want %q", got, want)
	}
	// No LOCALAPPDATA → %USERPROFILE%\AppData\Local\hermes, matching Hermes.
	if got, want := platformDefaultHermesHomeFor("windows", "", home), filepath.Join(home, "AppData", "Local", "hermes"); got != want {
		t.Errorf("windows no LOCALAPPDATA: got %q, want %q", got, want)
	}
	if got, want := platformDefaultHermesHomeFor("linux", la, home), filepath.Join(home, ".hermes"); got != want {
		t.Errorf("linux: got %q, want %q", got, want)
	}
}

// TestHermesOverlayDoesNotMirrorStickyProfile is the regression for the sticky
// active_profile bypass: active_profile and profiles/ from the shared home must
// NOT be mirrored into the overlay, or Hermes would follow the sticky profile at
// startup and redirect HERMES_HOME past the overlay.
func TestHermesOverlayDoesNotMirrorStickyProfile(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), "model: hermes-4\n")
	mustWrite(t, filepath.Join(sharedHome, "active_profile"), "coder")
	mustWrite(t, filepath.Join(sharedHome, "profiles", "coder", "config.yaml"), "model: coder\n")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}
	for _, name := range []string{"active_profile", "profiles"} {
		if _, err := os.Lstat(filepath.Join(hermesHome, name)); !os.IsNotExist(err) {
			t.Errorf("%s must not be mirrored into the overlay (sticky-profile bypass)", name)
		}
	}
}

// TestPrepareHermesHomeFailsOnMissingNamedProfile asserts an explicitly named
// profile whose home doesn't exist fails closed rather than silently seeding
// from an empty dir (which would drop the user's auth/config), matching Hermes'
// own sys.exit(1).
func TestPrepareHermesHomeFailsOnMissingNamedProfile(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	missingProfileHome := filepath.Join(base, "profiles", "does-not-exist")
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if _, err := prepareHermesHome(hermesHome, missingProfileHome, true, skills, nil, "", "", testLogger()); err == nil {
		t.Fatal("expected a missing named profile home to fail closed")
	}
}

// TestPrepareHermesNoSkillsLeavesHomeUnset is the regression for the review's
// top blocker: a Hermes task with no bound skills must NOT get a redirected
// HERMES_HOME.
func TestPrepareHermesNoSkillsLeavesHomeUnset(t *testing.T) {
	t.Parallel()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-hermes-noskill",
		TaskID:         "aaaa1111-2222-3333-4444-555566667777",
		Provider:       "hermes",
		Task:           TaskContextForEnv{IssueID: "no-skill"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if env.HermesHome != "" {
		t.Errorf("skill-less Hermes task must not redirect HERMES_HOME, got %q", env.HermesHome)
	}
	if _, err := os.Stat(filepath.Join(env.RootDir, "hermes-home")); !os.IsNotExist(err) {
		t.Error("no hermes-home overlay should be created for a skill-less task")
	}
}

// TestReuseHermesTearsDownWhenSkillsRemoved covers the resume path: a task that
// had a skill and lost its last one must drop the redirect and remove the
// overlay.
func TestReuseHermesTearsDownWhenSkillsRemoved(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), "model: hermes-4\n")

	withSkill := TaskContextForEnv{
		IssueID:     "hermes-resume",
		AgentSkills: []SkillContextForEnv{{Name: "Review Helper", Content: "Help review."}},
	}
	env, err := Prepare(PrepareParams{
		WorkspacesRoot:   t.TempDir(),
		WorkspaceID:      "ws-hermes-resume",
		TaskID:           "bbbb1111-2222-3333-4444-555566667777",
		Provider:         "hermes",
		HermesSourceHome: sharedHome,
		Task:             withSkill,
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if env.HermesHome == "" {
		t.Fatal("expected HERMES_HOME redirect for a task with a bound skill")
	}
	overlayDir := filepath.Join(env.RootDir, "hermes-home")

	if reused := Reuse(ReuseParams{WorkDir: env.WorkDir, Provider: "hermes", HermesSourceHome: sharedHome, Task: withSkill}, testLogger()); reused == nil {
		t.Fatal("Reuse with skill returned nil")
	} else if reused.HermesHome == "" {
		t.Error("resume with a bound skill should keep the redirect")
	}

	noSkill := TaskContextForEnv{IssueID: "hermes-resume"}
	reused := Reuse(ReuseParams{WorkDir: env.WorkDir, Provider: "hermes", HermesSourceHome: sharedHome, Task: noSkill}, testLogger())
	if reused == nil {
		t.Fatal("Reuse without skill returned nil")
	}
	if reused.HermesHome != "" {
		t.Errorf("removing the last skill should clear HERMES_HOME, got %q", reused.HermesHome)
	}
	if _, err := os.Stat(overlayDir); !os.IsNotExist(err) {
		t.Error("stale hermes-home overlay should be removed on teardown")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
