package execenv

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
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

// TestPrepareHermesHomeOverlay verifies the issue-task projection: only the
// explicitly supported runtime snapshots enter the task home, no task entry
// points back at owner state, and only bound skills are discoverable.
func TestPrepareHermesHomeOverlay(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "auth.json"), `{"token":"secret"}`)
	mustWrite(t, filepath.Join(sharedHome, ".env"), "ANTHROPIC_API_KEY=abc\nOWNER_NOTE=private")
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), "model: hermes-4\n")
	mustWrite(t, filepath.Join(sharedHome, "plugins", "custom-provider", "plugin.py"), "# provider plugin")
	mustWrite(t, filepath.Join(sharedHome, "oauth_state.json"), `{"nous":"tok"}`)
	mustWrite(t, filepath.Join(sharedHome, "skills", "personal-notes", "SKILL.md"), "My personal notes skill.")
	mustWrite(t, filepath.Join(sharedHome, "owner-private.txt"), "must not enter task home")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "Help review code."}}
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	for _, name := range []string{"auth.json", "plugins", "oauth_state.json"} {
		fi, err := os.Lstat(filepath.Join(hermesHome, name))
		if err != nil {
			t.Fatalf("%s not projected into overlay: %v", name, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s must be task-private, not a symlink into owner state", name)
		}
	}
	if _, err := os.Lstat(filepath.Join(hermesHome, "owner-private.txt")); !os.IsNotExist(err) {
		t.Fatalf("unknown owner entry entered task home: %v", err)
	}

	// Owner mutation after preparation must not change the task snapshot, and
	// task mutation must not propagate back to the owner home.
	mustWrite(t, filepath.Join(sharedHome, "auth.json"), `{"token":"rotated"}`)
	if data, err := os.ReadFile(filepath.Join(hermesHome, "auth.json")); err != nil || string(data) != `{"token":"secret"}` {
		t.Fatalf("task auth snapshot tracked owner mutation: bytes=%q err=%v", data, err)
	}
	mustWrite(t, filepath.Join(hermesHome, "oauth_state.json"), `{"nous":"task-local"}`)
	if data, err := os.ReadFile(filepath.Join(sharedHome, "oauth_state.json")); err != nil || string(data) != `{"nous":"tok"}` {
		t.Fatalf("task OAuth mutation reached owner state: bytes=%q err=%v", data, err)
	}
	mustWrite(t, filepath.Join(sharedHome, "plugins", "custom-provider", "plugin.py"), "# owner update")
	if data, err := os.ReadFile(filepath.Join(hermesHome, "plugins", "custom-provider", "plugin.py")); err != nil || string(data) != "# provider plugin" {
		t.Fatalf("task plugin snapshot tracked owner mutation: bytes=%q err=%v", data, err)
	}

	// .env is overlay-owned/derived (not symlinked): it keeps only explicitly
	// allowed Hermes runtime settings and pins HERMES_HOME to the overlay so
	// Hermes' override=True dotenv load can't relocate the home past it.
	envPath := filepath.Join(hermesHome, ".env")
	if fi, err := os.Lstat(envPath); err != nil {
		t.Fatalf(".env missing from overlay: %v", err)
	} else if fi.Mode()&os.ModeSymlink != 0 {
		t.Error(".env should be a derived copy, not a symlink into the shared home")
	}
	if data, _ := os.ReadFile(envPath); !strings.Contains(string(data), "ANTHROPIC_API_KEY=abc") {
		t.Error("derived .env dropped an allowed source credential")
	} else if strings.Contains(string(data), "OWNER_NOTE") {
		t.Error("derived .env projected an unrelated owner variable")
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
	if got := hermesExternalDirs(t, cfgPath); len(got) != 0 {
		t.Errorf("external_dirs = %v, want no owner paths", got)
	}

	if body, err := os.ReadFile(filepath.Join(hermesHome, "skills", "review-helper", "SKILL.md")); err != nil {
		t.Fatalf("bound skill not written: %v", err)
	} else if !strings.Contains(string(body), "Help review code.") {
		t.Error("bound SKILL.md missing content")
	}
	if _, err := os.Stat(filepath.Join(hermesHome, "skills", "personal-notes")); !os.IsNotExist(err) {
		t.Error("owner global skill must not enter the task-local skills directory")
	}
}

func TestHermesOverlayRejectsUnsafeOwnerSnapshotEntries(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"auth.json", "oauth_state.json", "plugins"} {
		t.Run(name, func(t *testing.T) {
			sharedHome := t.TempDir()
			target := filepath.Join(t.TempDir(), "owner-secret")
			mustWrite(t, target, "secret")
			if err := os.Symlink(target, filepath.Join(sharedHome, name)); err != nil {
				t.Fatalf("symlink %s: %v", name, err)
			}
			hermesHome := filepath.Join(t.TempDir(), "hermes-home")
			err := prepareHermesHome(hermesHome, sharedHome, false,
				[]SkillContextForEnv{{Name: "Review Helper", Content: "x"}}, nil, testLogger())
			if err == nil {
				t.Fatalf("unsafe owner %s symlink was accepted", name)
			}
		})
	}
}

// TestHermesOverlayProjectsOnlyAllowlistedOwnerCredentials is the P1 regression:
// an issue task must not inherit arbitrary owner dotenv state. Only explicit
// Hermes provider authentication/public settings may enter the task overlay.
func TestHermesOverlayProjectsOnlyAllowlistedOwnerCredentials(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, ".env"), strings.Join([]string{
		"ANTHROPIC_API_KEY=sk-anthropic",
		"ANTHROPIC_BASE_URL=https://anthropic.example.test",
		"ANTHROPIC_TOKEN=anthropic-token",
		"DEEPSEEK_API_KEY=sk-deepseek",
		"GEMINI_BASE_URL=https://gemini.example.test",
		"OPENAI_API_KEY=sk-openai",
		"OPENAI_BASE_URL=https://openai.example.test/v1",
		"OPENROUTER_API_KEY=sk-openrouter",
		"GOOGLE_API_KEY=google-key",
		"HERMES_YOLO_MODE=0",
		"OWNER_NOTE=do-not-project",
		"DATABASE_URL=postgres://owner-secret",
		"AWS_SECRET_ACCESS_KEY=owner-aws-secret",
		"UNLISTED_API_KEY=owner-unlisted-secret",
		"HERMES_HOME=/owner/home",
	}, "\n"))

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	got := applyDotenvOverride(t, filepath.Join(hermesHome, ".env"))
	want := map[string]string{
		"ANTHROPIC_API_KEY":  "sk-anthropic",
		"ANTHROPIC_BASE_URL": "https://anthropic.example.test",
		"ANTHROPIC_TOKEN":    "anthropic-token",
		"DEEPSEEK_API_KEY":   "sk-deepseek",
		"GEMINI_BASE_URL":    "https://gemini.example.test",
		"OPENAI_API_KEY":     "sk-openai",
		"OPENAI_BASE_URL":    "https://openai.example.test/v1",
		"OPENROUTER_API_KEY": "sk-openrouter",
		"GOOGLE_API_KEY":     "google-key",
		"HERMES_HOME":        hermesHome,
	}
	if len(got) != len(want) {
		t.Fatalf("overlay dotenv keys = %v, want only %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %q, want %q", key, got[key], value)
		}
	}
}

// TestHermesOverlayConfigProjectsOnlyAllowlistedFields is the config half of
// the P1 regression: owner credentials and unknown config must not cross into an
// issue task, while the model/provider and external skill roots Hermes needs do.
func TestHermesOverlayConfigProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), strings.Join([]string{
		"model:",
		"  default: anthropic/claude-sonnet",
		"  model: claude-sonnet",
		"  provider: anthropic",
		"  base_url: https://anthropic.example.test",
		"  api_mode: messages",
		"  openai_runtime: false",
		"  auth_mode: api-key",
		"  api_key: sk-model-owner-secret",
		"  owner_private: do-not-project",
		"  entra:",
		"    scope: https://cognitiveservices.azure.com/.default",
		"    client_secret: entra-owner-secret",
		"provider: legacy-anthropic",
		"api_key: sk-inline-owner-secret",
		"base_url: https://inline-owner.example.test",
		"unknown_top_level: owner-private",
		"skills:",
		"  external_dirs:",
		"    - team-skills",
		"  owner_private: do-not-project",
		"memory:",
		"  provider: supermemory",
		"  api_key: memory-owner-secret",
		"  memory_enabled: true",
	}, "\n"))

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(hermesHome, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse derived config: %v\n%s", err, data)
	}
	if len(got) != 4 {
		t.Fatalf("derived config keys = %v, want model/provider/skills/memory only", got)
	}
	if got["provider"] != "legacy-anthropic" {
		t.Errorf("allowed legacy provider was not preserved: %v", got)
	}
	modelConfig, ok := got["model"].(map[string]any)
	if !ok {
		t.Fatalf("model config = %#v, want projected mapping", got["model"])
	}
	wantModel := map[string]any{
		"default":        "anthropic/claude-sonnet",
		"model":          "claude-sonnet",
		"provider":       "anthropic",
		"base_url":       "https://anthropic.example.test",
		"api_mode":       "messages",
		"openai_runtime": false,
		"auth_mode":      "api-key",
	}
	for key, value := range wantModel {
		if modelConfig[key] != value {
			t.Errorf("model.%s = %#v, want %#v", key, modelConfig[key], value)
		}
	}
	for _, forbidden := range []string{"api_key", "owner_private"} {
		if _, ok := modelConfig[forbidden]; ok {
			t.Errorf("forbidden model field %q entered overlay: %v", forbidden, modelConfig)
		}
	}
	entraConfig, ok := modelConfig["entra"].(map[string]any)
	if !ok || len(entraConfig) != 1 || entraConfig["scope"] != "https://cognitiveservices.azure.com/.default" {
		t.Errorf("model.entra = %#v, want only public scope", modelConfig["entra"])
	}
	for _, forbidden := range []string{"api_key", "base_url", "unknown_top_level"} {
		if _, ok := got[forbidden]; ok {
			t.Errorf("forbidden config field %q entered overlay: %v", forbidden, got)
		}
	}
	skillsConfig, ok := got["skills"].(map[string]any)
	if !ok || len(skillsConfig) != 1 {
		t.Fatalf("skills config = %#v, want only external_dirs", got["skills"])
	}
	memoryConfig, ok := got["memory"].(map[string]any)
	if !ok || len(memoryConfig) != 1 || memoryConfig["provider"] != "" {
		t.Fatalf("memory config = %#v, want only disabled provider", got["memory"])
	}
	if dirs := hermesExternalDirs(t, filepath.Join(hermesHome, "config.yaml")); len(dirs) != 0 {
		t.Errorf("external_dirs = %v, want no owner paths", dirs)
	}
}

// TestHermesOverlayConfigParseFailureDoesNotCopyOwnerBytes ensures malformed
// owner config never falls back to a verbatim task-local copy containing secrets.
func TestHermesOverlayConfigParseFailureDoesNotCopyOwnerBytes(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"),
		"api_key: sk-owner-secret\nunknown: [unterminated\n")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	configPath := filepath.Join(hermesHome, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-owner-secret") || strings.Contains(string(data), "unterminated") {
		t.Fatalf("malformed owner config bytes entered overlay:\n%s", data)
	}
	if provider, ok := hermesMemoryProvider(t, configPath); !ok || provider != "" {
		t.Fatalf("memory.provider = %q, present=%v; want explicit disabled provider", provider, ok)
	}
	if dirs := hermesExternalDirs(t, configPath); len(dirs) != 0 {
		t.Errorf("external_dirs = %v, want no owner paths", dirs)
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
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	got, ok := hermesMemoryProvider(t, filepath.Join(hermesHome, "config.yaml"))
	if !ok {
		t.Fatal("memory.provider should be present and explicitly disabled")
	}
	if got != "" {
		t.Errorf("memory.provider = %q, want \"\" (external backend disabled)", got)
	}
	data, err := os.ReadFile(filepath.Join(hermesHome, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse derived config: %v", err)
	}
	memoryConfig, ok := config["memory"].(map[string]any)
	if !ok || len(memoryConfig) != 1 {
		t.Errorf("memory config = %#v, want only disabled provider", config["memory"])
	} else if _, exists := memoryConfig["memory_enabled"]; exists {
		t.Error("unknown memory_enabled field should not enter the task overlay")
	}
}

// TestHermesDerivedConfigDropsOwnerExternalDirs ensures owner-configured paths
// cannot expose arbitrary daemon-owner files to an issue-derived task.
func TestHermesDerivedConfigDropsOwnerExternalDirs(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"),
		"model: hermes-4\nskills:\n  external_dirs:\n    - team-skills\n    - /opt/shared/skills\n")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	if got := hermesExternalDirs(t, filepath.Join(hermesHome, "config.yaml")); len(got) != 0 {
		t.Errorf("owner external_dirs entered task config: %v", got)
	}
}

// TestHermesExternalDirsDoNotExpandIntoOwnerPaths verifies custom environment
// expansion cannot smuggle owner-controlled directories into task config.
func TestHermesExternalDirsDoNotExpandIntoOwnerPaths(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"),
		"skills:\n  external_dirs:\n    - ${TEAM_SKILLS}/reviews\n    - ${MYSTERY_VAR}/x\n")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	env := map[string]string{"TEAM_SKILLS": "/srv/team"} // MYSTERY_VAR set nowhere
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, env, testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	if got := hermesExternalDirs(t, filepath.Join(hermesHome, "config.yaml")); len(got) != 0 {
		t.Errorf("expanded owner external_dirs entered task config: %v", got)
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
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, testLogger()); err != nil {
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
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, testLogger()); err != nil {
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
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, testLogger()); err != nil {
		t.Fatalf("prepareHermesHome (reuse) failed: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(memDir, "MEMORY.md")); string(data) != "TASK MEMORY" {
		t.Errorf("reuse should preserve task-local memory, got: %q", data)
	}
}

// TestHermesOverlayPermissions asserts the task home is 0700 and the derived
// config containing projected owner runtime settings is 0600.
func TestHermesOverlayPermissions(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), "model: hermes-4\napi_key: sk-secret\n")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, testLogger()); err != nil {
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
}

// TestHermesPluginSnapshotRemovedWithSource asserts an allowlisted snapshot is
// removed on rebuild when its source disappears.
func TestHermesPluginSnapshotRemovedWithSource(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), "model: hermes-4\n")
	mustWrite(t, filepath.Join(sharedHome, "plugins", "p", "plugin.py"), "# plugin")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(hermesHome, "plugins")); err != nil {
		t.Fatalf("plugins not snapshotted: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(sharedHome, "plugins")); err != nil {
		t.Fatalf("remove shared plugins: %v", err)
	}
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, testLogger()); err != nil {
		t.Fatalf("prepareHermesHome (rebuild) failed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(hermesHome, "plugins")); !os.IsNotExist(err) {
		t.Error("stale plugin snapshot should be removed after source deletion")
	}
}

func TestHermesOptionalSnapshotRemovedWithSource(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	mustWrite(t, filepath.Join(sharedHome, "auth.json"), `{"token":"owner"}`)
	mustWrite(t, filepath.Join(sharedHome, "oauth_state.json"), `{"state":"owner"}`)

	if err := prepareHermesHome(hermesHome, sharedHome, false, nil, nil, testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}
	for _, name := range []string{"auth.json", "oauth_state.json"} {
		if err := os.Remove(filepath.Join(sharedHome, name)); err != nil {
			t.Fatalf("remove source %s: %v", name, err)
		}
	}
	if err := prepareHermesHome(hermesHome, sharedHome, false, nil, nil, testLogger()); err != nil {
		t.Fatalf("prepareHermesHome (rebuild) failed: %v", err)
	}
	for _, name := range []string{"auth.json", "oauth_state.json"} {
		if _, err := os.Lstat(filepath.Join(hermesHome, name)); !os.IsNotExist(err) {
			t.Errorf("stale %s snapshot survived source deletion: %v", name, err)
		}
	}
}

func TestHermesOptionalSnapshotsRejectHardLinks(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"auth.json", "oauth_state.json"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sharedHome := t.TempDir()
			source := filepath.Join(sharedHome, name)
			mustWrite(t, source, "owner credential")
			if err := os.Link(source, filepath.Join(sharedHome, name+".link")); err != nil {
				t.Skipf("hard links unavailable: %v", err)
			}

			err := prepareHermesHome(filepath.Join(t.TempDir(), "hermes-home"), sharedHome, false, nil, nil, testLogger())
			if err == nil || !strings.Contains(err.Error(), "hard link") {
				t.Fatalf("hard-linked %s error = %v, want fail-closed rejection", name, err)
			}
		})
	}
}

func TestHermesPluginSnapshotRejectsHardLinks(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	source := filepath.Join(sharedHome, "plugins", "provider", "plugin.py")
	mustWrite(t, source, "original plugin")
	if err := os.Link(source, filepath.Join(sharedHome, "plugins", "provider", "plugin-copy.py")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}

	err := snapshotHermesPlugins(sharedHome, t.TempDir(), nil)
	if err == nil || !errors.Is(err, errUnsafeHermesPluginEntry) || !strings.Contains(err.Error(), "hard link") {
		t.Fatalf("plugin hard-link error = %v, want unsafe hard-link rejection", err)
	}
}

func TestHermesPluginSnapshotRejectsSpecialEntries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFO fixture is POSIX-only")
	}
	sharedHome := t.TempDir()
	pluginDir := filepath.Join(sharedHome, "plugins", "provider")
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(pluginDir, "control.pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("FIFO unavailable: %v", err)
	}

	err := snapshotHermesPlugins(sharedHome, t.TempDir(), nil)
	if err == nil || !errors.Is(err, errUnsafeHermesPluginEntry) {
		t.Fatalf("plugin FIFO error = %v, want unsafe-entry rejection", err)
	}
}

func TestHermesPluginSnapshotRejectsPathReplacementAfterOpen(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	source := filepath.Join(sharedHome, "plugins", "provider", "plugin.py")
	mustWrite(t, source, "original plugin")
	replacement := filepath.Join(sharedHome, "replacement.py")
	mustWrite(t, replacement, "replacement plugin")

	err := snapshotHermesPlugins(sharedHome, t.TempDir(), func(opened string) {
		if opened != source {
			return
		}
		if renameErr := os.Rename(replacement, source); renameErr != nil {
			t.Fatalf("replace opened plugin path: %v", renameErr)
		}
	})
	if err == nil || !errors.Is(err, errUnsafeHermesPluginEntry) || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("plugin replacement error = %v, want identity-change rejection", err)
	}
}

func TestHermesTaskHomeRemovesUnknownEntriesOnReuse(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	if err := prepareHermesHome(hermesHome, sharedHome, false, nil, nil, testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}
	mustWrite(t, filepath.Join(hermesHome, "daemon-owner-secret.txt"), "must be removed")
	mustWrite(t, filepath.Join(hermesHome, "unknown", "nested.txt"), "must be removed")

	if err := prepareHermesHome(hermesHome, sharedHome, false, nil, nil, testLogger()); err != nil {
		t.Fatalf("prepareHermesHome (reuse) failed: %v", err)
	}
	for _, name := range []string{"daemon-owner-secret.txt", "unknown"} {
		if _, err := os.Lstat(filepath.Join(hermesHome, name)); !os.IsNotExist(err) {
			t.Errorf("unknown task-home entry %s survived reuse: %v", name, err)
		}
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
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, testLogger()); err == nil {
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

// TestHermesExternalDirsDoNotExposeSelectedProfileHome ensures a selected
// profile cannot reintroduce its owner path through skills.external_dirs.
func TestHermesExternalDirsDoNotExposeSelectedProfileHome(t *testing.T) {
	t.Parallel()
	profileHome := t.TempDir() // stands in for <root>/profiles/coder
	mustWrite(t, filepath.Join(profileHome, "config.yaml"),
		"skills:\n  external_dirs:\n    - ${HERMES_HOME}/profile-skills\n")

	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	env := map[string]string{"HERMES_HOME": profileHome} // as the daemon sets it
	skills := []SkillContextForEnv{{Name: "Review Helper", Content: "x"}}
	if err := prepareHermesHome(hermesHome, profileHome, true, skills, env, testLogger()); err != nil {
		t.Fatalf("prepareHermesHome failed: %v", err)
	}

	if got := hermesExternalDirs(t, filepath.Join(hermesHome, "config.yaml")); len(got) != 0 {
		t.Errorf("selected profile paths entered task config: %v", got)
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
	if err := prepareHermesHome(hermesHome, sourceHome, false, skills, nil, testLogger()); err != nil {
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
	if err := prepareHermesHome(hermesHome, sourceHome, false, skills, nil, testLogger()); err != nil {
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
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, testLogger()); err != nil {
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
	if err := prepareHermesHome(hermesHome, missingProfileHome, true, skills, nil, testLogger()); err == nil {
		t.Fatal("expected a missing named profile home to fail closed")
	}
}

// TestPrepareHermesNoSkillsUsesPrivateHome proves a skill-less Issue task does
// not fall through to daemon-owner Hermes state.
func TestPrepareHermesNoSkillsUsesPrivateHome(t *testing.T) {
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

	if env.HermesHome == "" {
		t.Fatal("skill-less Hermes task must use a private HERMES_HOME")
	}
	if info, err := os.Stat(filepath.Join(env.RootDir, "hermes-home")); err != nil || !info.IsDir() {
		t.Fatalf("private hermes-home missing: %v", err)
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

	if reused := Reuse(ReuseParams{RootDir: env.RootDir, WorkDir: env.WorkDir, Provider: "hermes", HermesSourceHome: sharedHome, Task: withSkill}, testLogger()); reused == nil {
		t.Fatal("Reuse with skill returned nil")
	} else if reused.HermesHome == "" {
		t.Error("resume with a bound skill should keep the redirect")
	}

	noSkill := TaskContextForEnv{IssueID: "hermes-resume"}
	reused := Reuse(ReuseParams{RootDir: env.RootDir, WorkDir: env.WorkDir, Provider: "hermes", HermesSourceHome: sharedHome, Task: noSkill}, testLogger())
	if reused == nil {
		t.Fatal("Reuse without skill returned nil")
	}
	if reused.HermesHome == "" {
		t.Fatal("removing the last skill must retain the private HERMES_HOME")
	}
	if entries, err := os.ReadDir(filepath.Join(overlayDir, "skills")); err != nil || len(entries) != 0 {
		t.Fatalf("skill-less reused home must contain an empty task skills dir: entries=%v err=%v", entries, err)
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
