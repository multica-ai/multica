package execenv

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHermesConversationHomeOwnsDatabase(t *testing.T) {
	shared := t.TempDir()
	store := filepath.Join(t.TempDir(), "conversation")
	first := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "review", Content: "Review code"}}
	prepared, err := prepareHermesHome(first, shared, false, skills, nil, "", store, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	first = prepared.Home
	// Hermes' gateway requires the resolved DB parent to equal its resolved home.
	mustWrite(t, filepath.Join(first, "state.db"), "persisted transcript")
	home, err := filepath.EvalSymlinks(first)
	if err != nil {
		t.Fatal(err)
	}
	db, err := filepath.EvalSymlinks(filepath.Join(first, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(db) != home {
		t.Fatalf("database parent %s is outside Hermes home %s (mount %+v)", filepath.Dir(db), home, prepared)
	}
}

func TestHermesTaskDisablesGatewayIngress(t *testing.T) {
	shared, home := t.TempDir(), filepath.Join(t.TempDir(), "hermes-home")
	mustWrite(t, filepath.Join(shared, "config.yaml"), "model: {provider: custom, default: fixture-model}\nplatforms:\n  api_server: {enabled: true, port: 8642}\n  discord: {enabled: true}\n  custom-chat: {enabled: true}\n")
	for _, name := range []string{"gateway.pid", "gateway.lock", "gateway_state.json", "gateway.sock.path", "gateway.json", "cron/jobs.json", "sessions/sessions.json", "logs/gateway.log"} {
		mustWrite(t, filepath.Join(shared, name), "host-only")
	}
	if _, err := prepareHermesHome(home, shared, false, nil, nil, "", "", testLogger()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gateway.pid", "gateway.lock", "gateway_state.json", "gateway.sock.path", "gateway.json", "cron/jobs.json", "sessions/sessions.json", "logs/gateway.log"} {
		if _, err := os.Stat(filepath.Join(home, name)); !os.IsNotExist(err) {
			t.Errorf("task inherited host runtime entry %s (err %v)", name, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Model     struct{ Default string }
		Platforms map[string]struct{ Enabled bool }
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Model.Default != "fixture-model" {
		t.Fatal("model configuration was lost")
	}
	for _, name := range []string{"api_server", "discord", "telegram", "custom-chat"} {
		platform, ok := config.Platforms[name]
		if !ok || platform.Enabled {
			t.Errorf("platform %s not explicitly disabled", name)
		}
	}
	// Reuse keeps task-owned gateway state, while the host remains unchanged.
	mustWrite(t, filepath.Join(home, "gateway_state.json"), "task-owned")
	if _, err := prepareHermesHome(home, shared, false, nil, nil, "", "", testLogger()); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		filepath.Join(home, "gateway_state.json"):   "task-owned",
		filepath.Join(shared, "gateway_state.json"): "host-only",
	} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("runtime state %s = %q, %v; want %q", path, got, err, want)
		}
	}
}

func TestHermesLegacySessionLinkMigration(t *testing.T) {
	requireSymlinks(t)
	shared, scratch, store := t.TempDir(), t.TempDir(), t.TempDir()
	if _, err := prepareHermesHome(scratch, shared, false, nil, nil, "", "", testLogger()); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(store, "state.db"), "original transcript")
	mustWrite(t, filepath.Join(store, "state.db-wal"), "original WAL")
	if err := os.Symlink(filepath.Join(store, "state.db"), filepath.Join(scratch, "state.db")); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareHermesHome(scratch, shared, false, nil, nil, "", store, testLogger())
	if err != nil || !prepared.HistoryPresent || prepared.Home != store {
		t.Fatalf("legacy migration = %+v, %v", prepared, err)
	}
	for name, want := range map[string]string{"state.db": "original transcript", "state.db-wal": "original WAL"} {
		got, err := os.ReadFile(filepath.Join(store, name))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
	if fi, err := os.Lstat(scratch); err != nil || !fi.IsDir() {
		t.Fatalf("rollback task overlay must remain an ordinary directory: %v", err)
	}
}

func TestHermesDisablesNestedGatewayPlatforms(t *testing.T) {
	shared, home := t.TempDir(), t.TempDir()
	mustWrite(t, filepath.Join(shared, "config.yaml"), "template: &adapter {enabled: true, port: 8642}\ngateway:\n  api_server: *adapter\n  platforms:\n    custom-chat: {enabled: true}\nplatforms:\n  discord: *adapter\n")
	if err := writeDerivedHermesConfig(shared, home, nil, testLogger()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Template struct{ Enabled bool }
		Gateway  struct {
			APIServer struct{ Enabled bool } `yaml:"api_server"`
		}
		Platforms map[string]struct{ Enabled bool }
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if !parsed.Template.Enabled || parsed.Gateway.APIServer.Enabled || parsed.Platforms["discord"].Enabled {
		t.Fatal("nested disable changed the template or left an adapter enabled")
	}
	if platform, ok := parsed.Platforms["custom-chat"]; !ok || platform.Enabled {
		t.Fatal("nested custom platform was not explicitly disabled")
	}
}
