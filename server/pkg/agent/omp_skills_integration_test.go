//go:build agentintegration

package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestOmpSkillDiscoveryWithoutModelCall(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to run real-agent smoke tests")
	}
	binary := os.Getenv("MULTICA_OMP_SMOKE_EXECUTABLE")
	if binary == "" {
		t.Skip("set MULTICA_OMP_SMOKE_EXECUTABLE to the OMP binary to verify")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PI_CONFIG_DIR", ".omp")
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, ".omp", "agent"))
	t.Setenv("ANTHROPIC_API_KEY", "unused-no-model-call")
	profileDir := func(profile string) string { return filepath.Join(home, ".omp", "profiles", profile, "agent") }
	writeSkill := func(root, name string) string {
		t.Helper()
		path := filepath.Join(root, name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("---\nname: "+name+"\ndescription: Smoke fixture\n---\nFixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	paths := map[string]string{}
	for _, name := range []string{"alpha", "ignored", "disabled", "filtered"} {
		paths[name] = writeSkill(filepath.Join(profileDir("a"), "skills"), name)
	}
	paths["beta"] = writeSkill(filepath.Join(profileDir("b"), "skills"), "beta")
	paths["shared"] = writeSkill(filepath.Join(home, ".agents", "skills"), "shared")
	custom := filepath.Join(home, "custom-skills")
	paths["custom"] = writeSkill(custom, "custom")
	for _, tc := range []struct {
		name, profile string
		skills        map[string]any
		extraArgs     []string
		want          []string
	}{
		{"source-off", "a", map[string]any{"enableAgentsUser": false}, nil, []string{"alpha"}},
		{"source-on", "a", map[string]any{"enableAgentsUser": true}, nil, []string{"alpha", "shared"}},
		{"allowlist", "a", map[string]any{"enableAgentsUser": true, "includeSkills": []string{"alpha"}}, nil, []string{"alpha"}},
		{"commands-disabled", "a", map[string]any{"enableSkillCommands": false}, nil, []string{"alpha"}},
		{"skills-disabled", "a", map[string]any{"enabled": false}, nil, []string{}},
		{"no-skills-flag", "a", nil, []string{"--no-skills"}, []string{}},
		{"custom-profile-b", "b", map[string]any{"includeSkills": []string{"beta", "custom"}, "customDirectories": []string{custom}}, nil, []string{"beta", "custom"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			skillSettings := map[string]any{
				"enabled": true, "enableSkillCommands": true,
				"enablePiUser": true, "enablePiProject": false,
				"enableAgentsUser": false, "enableAgentsProject": false,
				"includeSkills": []string{"alpha", "ignored", "disabled", "shared"},
				"ignoredSkills": []string{"ignored"},
			}
			for key, value := range tc.skills {
				skillSettings[key] = value
			}
			config, _ := json.Marshal(map[string]any{
				"enabledProviders":   []string{"agents"},
				"disabledExtensions": []string{"skill:disabled"},
				"skills":             skillSettings,
			})
			configPath := filepath.Join(profileDir(tc.profile), "config.yml")
			if err := os.WriteFile(configPath, config, 0o600); err != nil {
				t.Fatal(err)
			}
			prefix := []string{"--profile", tc.profile, "--no-extensions", "--no-lsp", "--no-pty", "--model", "anthropic/claude-sonnet-4-5"}
			prefix = append(prefix, tc.extraArgs...)
			skills, err := DiscoverOmpSkills(t.Context(), NewCommand(binary, prefix))
			if err != nil {
				t.Fatal(err)
			}
			names := make([]string, 0, len(skills))
			for _, skill := range skills {
				names = append(names, skill.Name)
				if skill.Path != paths[skill.Name] {
					t.Errorf("%s path=%q, want %q", skill.Name, skill.Path, paths[skill.Name])
				}
			}
			sort.Strings(names)
			if !reflect.DeepEqual(names, tc.want) {
				t.Fatalf("roster=%v, want %v", names, tc.want)
			}
			persisted, _ := os.ReadFile(configPath)
			if string(persisted) != string(config) {
				t.Fatal("discovery changed persisted profile settings")
			}
		})
	}
}
