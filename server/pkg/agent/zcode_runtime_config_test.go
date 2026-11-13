package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestZcodeDesktopProviderRepositoryIsPrivateAndDoesNotModifyUserConfig(t *testing.T) {
	base := t.TempDir()
	executable := filepath.Join(base, "ZCode.app", "Contents", "Resources", "glm", "zcode.cjs")
	builtin := filepath.Join(filepath.Dir(executable), "..", "config", "provider", "zcode-builtin.json")
	if err := os.MkdirAll(filepath.Dir(builtin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(builtin, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	configRoot := filepath.Join(base, ".zcode", "v2")
	if err := os.MkdirAll(configRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	source := []byte(`{"provider":{
		"builtin:bigmodel-coding-plan":{"enabled":true,"kind":"anthropic","options":{"apiKey":"test-secret","baseURL":"https://example.invalid"},"models":{"m":{"limit":{"context":12000}}}},
		"builtin:bigmodel-start-plan":{"enabled":true,"options":{"apiKey":"identity-token"},"models":{"m":{}}},
		"disabled":{"enabled":false,"options":{"apiKey":"disabled-secret"},"models":{"m":{}}}
	}}`)
	file := filepath.Join(configRoot, "config.json")
	if err := os.WriteFile(file, source, 0o600); err != nil {
		t.Fatal(err)
	}
	native := []byte(`{"schemaVersion":1,"config":{"providerConfigRules":{"providerRules":[{"providerId":"existing"}]},"modelConfigRules":{"providerModelRules":[]}}}`)
	nativePath := filepath.Join(configRoot, "provider_config.json")
	if err := os.WriteFile(nativePath, native, 0o600); err != nil {
		t.Fatal(err)
	}
	env, cleanup, err := prepareZcodeRuntimeEnv(executable, []string{"ZCODE_DATA_BASE_DIR=" + base})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	snapshot := zcodeEnvValue(env, zcodePersonalConfigEnv)
	if snapshot == "" || snapshot == nativePath {
		t.Fatal("expected a separate process-owned snapshot")
	}
	info, err := os.Stat(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("provider snapshot mode = %v", info.Mode())
	}
	data, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Config struct {
			Providers struct {
				Rules []json.RawMessage `json:"providerRules"`
			} `json:"providerConfigRules"`
		} `json:"config"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Config.Providers.Rules) != 2 || !strings.Contains(string(data), "test-secret") || strings.Contains(string(data), "identity-token") || strings.Contains(string(data), "disabled-secret") {
		t.Fatal("snapshot must preserve native providers and import only enabled API connections")
	}
	if after, _ := os.ReadFile(file); string(after) != string(source) {
		t.Fatal("Desktop source was modified")
	}
	if after, _ := os.ReadFile(nativePath); string(after) != string(native) {
		t.Fatal("native repository was modified")
	}
	cleanup()
	if _, err := os.Stat(snapshot); !os.IsNotExist(err) {
		t.Fatal("provider snapshot was not removed")
	}
}

func TestZcodeExplicitProviderRepositoryRemainsAuthoritative(t *testing.T) {
	env := []string{zcodePersonalConfigEnv + "=/explicit/providers.json", "ZCODE_BUILTIN_PROVIDER_CONFIG_FILE=/explicit/builtin.json"}
	got, cleanup, err := prepareZcodeRuntimeEnv("/app/zcode.cjs", env)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if strings.Join(got, "\n") != strings.Join(env, "\n") {
		t.Fatal("explicit repository was replaced")
	}
}

// setZcodeTestTempDir points os.TempDir (TMPDIR on Unix, TMP/TEMP on
// Windows) at a fresh directory, so snapshots land where the sweep looks.
func setZcodeTestTempDir(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("TMPDIR", base)
	t.Setenv("TMP", base)
	t.Setenv("TEMP", base)
	return base
}

// writeZcodeSnapshotDir lays out a provider snapshot directory as
// prepareZcodeRuntimeEnv does, with or without the owner file, and dates it
// age into the past.
func writeZcodeSnapshotDir(t *testing.T, base, name string, owner bool, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(base, zcodeProvidersTempPrefix+name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "providers.json"), []byte(`{"apiKey":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if owner {
		if err := os.WriteFile(filepath.Join(dir, zcodeSnapshotOwnerFile), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Now().Add(-age)
	if err := os.Chtimes(dir, at, at); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCleanupStaleZcodeProviderSnapshotsRemovesLeftovers(t *testing.T) {
	base := setZcodeTestTempDir(t)
	// A crash leftover: the owner file is there, but its lock died with the
	// process that wrote it.
	leftover := writeZcodeSnapshotDir(t, base, "orphan123", true, 2*zcodeSnapshotMinAge)
	// Unrelated temp dir must survive.
	keep := filepath.Join(base, "unrelated-temp")
	if err := os.MkdirAll(keep, 0o700); err != nil {
		t.Fatal(err)
	}
	removed := CleanupStaleZcodeProviderSnapshots()
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatalf("leftover still present: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated dir was removed: %v", err)
	}
}

// Another daemon starting on the same machine must not delete the provider
// snapshot of a ZCode run still in progress (MUL-8546 preflight).
func TestCleanupStaleZcodeProviderSnapshotsKeepsSnapshotInUse(t *testing.T) {
	base := setZcodeTestTempDir(t)
	install := t.TempDir()
	executable := filepath.Join(install, "glm", "zcode.cjs")
	builtin := filepath.Join(filepath.Dir(executable), "..", "config", "provider", "zcode-builtin.json")
	if err := os.MkdirAll(filepath.Dir(builtin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(builtin, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	env, cleanup, err := prepareZcodeRuntimeEnv(executable, []string{"ZCODE_DATA_BASE_DIR=" + install})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	snapshot := zcodeEnvValue(env, zcodePersonalConfigEnv)
	if filepath.Dir(filepath.Dir(snapshot)) != base {
		t.Fatalf("snapshot %q is not under the test temp dir %q", snapshot, base)
	}
	// Age the snapshot well past both thresholds: only its owner lock keeps it.
	old := time.Now().Add(-2 * zcodeUnownedSnapshotMaxAge)
	if err := os.Chtimes(filepath.Dir(snapshot), old, old); err != nil {
		t.Fatal(err)
	}

	if removed := CleanupStaleZcodeProviderSnapshots(); removed != 0 {
		t.Fatalf("removed = %d, want 0 while the snapshot is in use", removed)
	}
	if _, err := os.ReadFile(snapshot); err != nil {
		t.Fatalf("snapshot in use was removed: %v", err)
	}

	cleanup()
	if _, err := os.Stat(filepath.Dir(snapshot)); !os.IsNotExist(err) {
		t.Fatalf("cleanup left the snapshot directory: %v", err)
	}
}

func TestZcodeProviderSnapshotSweepRules(t *testing.T) {
	base := t.TempDir()
	cases := []struct {
		name   string
		owner  bool
		locked bool
		age    time.Duration
		remove bool
	}{
		{name: "owner gone", owner: true, age: 2 * zcodeSnapshotMinAge, remove: true},
		{name: "owner gone but just created", owner: true, age: zcodeSnapshotMinAge / 2, remove: false},
		{name: "owner alive", owner: true, locked: true, age: 2 * zcodeUnownedSnapshotMaxAge, remove: false},
		{name: "unowned and recent", age: zcodeUnownedSnapshotMaxAge / 2, remove: false},
		{name: "unowned and older than a day", age: zcodeUnownedSnapshotMaxAge + time.Hour, remove: true},
	}
	dirs := make([]string, len(cases))
	for i, tc := range cases {
		dirs[i] = writeZcodeSnapshotDir(t, base, strings.ReplaceAll(tc.name, " ", "-"), tc.owner, tc.age)
		if tc.locked {
			lock, ok, err := tryLockPiSessionFile(filepath.Join(dirs[i], zcodeSnapshotOwnerFile))
			if err != nil || !ok {
				t.Fatalf("%s: lock owner file: ok=%v err=%v", tc.name, ok, err)
			}
			t.Cleanup(func() { releasePiSessionFileLock(lock) })
			// Locking may create a sidecar on Windows; date the dir again.
			at := time.Now().Add(-tc.age)
			if err := os.Chtimes(dirs[i], at, at); err != nil {
				t.Fatal(err)
			}
		}
	}

	want := 0
	for _, tc := range cases {
		if tc.remove {
			want++
		}
	}
	if removed := cleanupStaleZcodeProviderSnapshots(base, time.Now()); removed != want {
		t.Fatalf("removed = %d, want %d", removed, want)
	}
	for i, tc := range cases {
		_, err := os.Stat(dirs[i])
		if gone := os.IsNotExist(err); gone != tc.remove {
			t.Errorf("%s: removed = %v, want %v (stat err %v)", tc.name, gone, tc.remove, err)
		}
	}
}

func TestRequireNodeForZcodeScript(t *testing.T) {
	if err := requireNodeForZcodeScript("/usr/local/bin/zcode"); err != nil {
		t.Fatalf("native path should pass: %v", err)
	}
	origPath := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", origPath) })
	t.Setenv("PATH", t.TempDir()) // empty of node
	err := requireNodeForZcodeScript("/Applications/ZCode.app/Contents/Resources/glm/zcode.cjs")
	if err == nil {
		t.Fatal("expected error when node is missing")
	}
	if !strings.Contains(err.Error(), "Node.js") || !strings.Contains(err.Error(), "node was not found") {
		t.Fatalf("error = %q, want clear Node.js message", err)
	}
}
