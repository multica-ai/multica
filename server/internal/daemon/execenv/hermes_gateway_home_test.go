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
	t.Setenv(hermesGatewayStateEnv, "isolated")
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

func TestHermesTaskSharesContainerGateway(t *testing.T) {
	requireSymlinks(t)
	t.Setenv(hermesGatewayStateEnv, "shared")
	shared, home := t.TempDir(), filepath.Join(t.TempDir(), "hermes-home")
	for _, name := range []string{"gateway.lock", "gateway_state.json", "cron/jobs.json", "sessions/sessions.json", "logs/gateway.log"} {
		mustWrite(t, filepath.Join(shared, name), "container gateway")
	}
	for range 2 { // Reuse must keep the attachment too.
		if _, err := prepareHermesHome(home, shared, false, nil, nil, "", "", testLogger()); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"gateway.lock", "gateway_state.json", "cron", "sessions", "logs"} {
			target, err := os.Readlink(filepath.Join(home, name))
			if err != nil || target != filepath.Join(shared, name) {
				t.Errorf("%s = link to %q, %v; want link to the shared home", name, target, err)
			}
		}
		if _, err := os.Lstat(filepath.Join(home, hermesGatewayStateMarker)); !os.IsNotExist(err) {
			t.Errorf("shared gateway mode wrote the isolation marker (err %v)", err)
		}
	}
}

func TestHermesGatewayStateMode(t *testing.T) {
	dir := t.TempDir()
	comm, basedir := filepath.Join(dir, "comm"), filepath.Join(dir, "basedir")
	mustWrite(t, comm, "s6-svscan\n")
	if err := os.Mkdir(basedir, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "init"), "systemd\n")
	for _, tc := range []struct {
		name, mode, comm, basedir string
		want                      bool
	}{
		{"s6 container", "", comm, basedir, false},
		{"other init", "", filepath.Join(dir, "init"), basedir, true},
		{"no s6 basedir", "", comm, filepath.Join(dir, "missing"), true},
		{"basedir is a file", "", comm, comm, true},
		{"unreadable init", "", filepath.Join(dir, "missing"), basedir, true},
		{"forced isolated", "isolated", comm, basedir, true},
		{"forced shared", "shared", filepath.Join(dir, "missing"), "", false},
		{"unknown override", "bogus", comm, basedir, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hermesGatewayStateIsolated(tc.mode, tc.comm, tc.basedir); got != tc.want {
				t.Fatalf("isolated = %v, want %v", got, tc.want)
			}
		})
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

func TestHermesSharedGatewayResumesFromOwnerStore(t *testing.T) {
	requireSymlinks(t)
	for _, tc := range []struct {
		name, mode, ownerDB string
		want                bool
	}{
		{"shared with owner transcript", "shared", "owner transcript", true},
		{"shared with empty owner database", "shared", "", false},
		{"shared without owner database", "shared", "-", false},
		{"isolated ignores owner", "isolated", "owner transcript", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(hermesGatewayStateEnv, tc.mode)
			shared, owner := t.TempDir(), t.TempDir()
			store := filepath.Join(t.TempDir(), "conversation")
			home := filepath.Join(t.TempDir(), "hermes-home")
			mustWrite(t, filepath.Join(shared, "gateway.lock"), `{"pid":193,"kind":"hermes-gateway","argv":["hermes","gateway","run"],"hermes_home":"`+filepath.ToSlash(owner)+`"}`)
			if tc.ownerDB != "-" {
				mustWrite(t, filepath.Join(owner, "state.db"), tc.ownerDB)
			}
			prepared, err := prepareHermesHome(home, shared, false, nil, nil, "", store, testLogger())
			if err != nil {
				t.Fatal(err)
			}
			if prepared.HistoryPresent != tc.want {
				t.Fatalf("HistoryPresent = %v, want %v (mount %+v)", prepared.HistoryPresent, tc.want, prepared)
			}
			if _, err := os.Lstat(filepath.Join(store, "state.db")); !os.IsNotExist(err) {
				t.Errorf("task store gained a database (err %v)", err)
			}
			if got, err := os.ReadFile(filepath.Join(owner, "state.db")); tc.ownerDB != "-" && (err != nil || string(got) != tc.ownerDB) {
				t.Errorf("owner database = %q, %v; want untouched", got, err)
			}
		})
	}
}

func TestHermesGatewayStateModeSwitch(t *testing.T) {
	requireSymlinks(t)
	shared, home := t.TempDir(), filepath.Join(t.TempDir(), "hermes-home")
	names := []string{"gateway.lock", "gateway_state.json", "cron", "sessions", "logs"}
	for _, name := range []string{"gateway.lock", "gateway_state.json", "cron/jobs.json", "sessions/sessions.json", "logs/gateway.log"} {
		mustWrite(t, filepath.Join(shared, name), "container gateway")
	}
	prepare := func(mode string) {
		t.Helper()
		t.Setenv(hermesGatewayStateEnv, mode)
		if _, err := prepareHermesHome(home, shared, false, nil, nil, "", "", testLogger()); err != nil {
			t.Fatal(err)
		}
	}
	assertDetached := func() {
		t.Helper()
		for _, name := range names {
			if fi, err := os.Lstat(filepath.Join(home, name)); err == nil && fi.Mode()&os.ModeSymlink != 0 {
				t.Errorf("%s is still a link after switching to isolated", name)
			}
		}
		_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
			if err == nil && d.Type()&os.ModeSymlink != 0 {
				// The shared home holds only gateway state, so no link may reach it.
				if target, err := filepath.EvalSymlinks(path); err == nil && isPathUnder(resolvePathBestEffort(shared), target) {
					t.Errorf("%s still points into the shared home (%s)", path, target)
				}
			}
			return nil
		})
		if fi, err := os.Lstat(filepath.Join(home, hermesGatewayStateMarker)); err != nil || !fi.Mode().IsRegular() {
			t.Errorf("isolation marker missing: %v", err)
		}
	}

	prepare("isolated") // Leaves the marker behind, as a prior isolated run would.
	prepare("shared")
	for _, name := range names {
		if target, err := os.Readlink(filepath.Join(home, name)); err != nil || target != filepath.Join(shared, name) {
			t.Fatalf("shared %s = link to %q, %v", name, target, err)
		}
	}
	prepare("isolated")
	assertDetached()
	if got, err := os.ReadFile(filepath.Join(shared, "sessions", "sessions.json")); err != nil || string(got) != "container gateway" {
		t.Errorf("shared sessions damaged: %q, %v", got, err)
	}

	// The task's own sessions/ survives isolated reuse.
	mustWrite(t, filepath.Join(home, "sessions", "sessions.json"), "task-owned")
	prepare("isolated")
	assertDetached()
	if got, err := os.ReadFile(filepath.Join(home, "sessions", "sessions.json")); err != nil || string(got) != "task-owned" {
		t.Errorf("task sessions = %q, %v; want task-owned", got, err)
	}

	prepare("shared")
	for _, name := range names {
		if target, err := os.Readlink(filepath.Join(home, name)); err != nil || target != filepath.Join(shared, name) {
			t.Errorf("shared again %s = link to %q, %v", name, target, err)
		}
	}
}
