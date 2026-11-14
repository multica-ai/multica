package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

func TestCleanupStaleZcodeProviderSnapshotsRemovesLeftovers(t *testing.T) {
	base := t.TempDir()
	t.Setenv("TMPDIR", base)
	// os.TempDir on Unix reads TMPDIR; recreate the leftover layout.
	leftover := filepath.Join(base, zcodeProvidersTempPrefix+"orphan123")
	if err := os.MkdirAll(leftover, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(leftover, "providers.json")
	if err := os.WriteFile(secret, []byte(`{"apiKey":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
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

// zcodeFixtureRoot returns a temp dir with symlinks resolved, so paths built
// from it compare equal to the ones resolved from a wrapper (macOS /var).
func zcodeFixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func writeZcodeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// zcodeDesktopConnections is a Desktop config.json with one enabled Coding
// Plan connection. The API key marks which data dir the snapshot was read from.
func zcodeDesktopConnections(apiKey string) string {
	return `{"provider":{"builtin:bigmodel-coding-plan":{"enabled":true,"kind":"anthropic","options":{"apiKey":"` + apiKey + `"},"models":{"GLM-5.3":{}}}}}`
}

// zcodeDesktopBundle lays out a Desktop app bundle under root and returns its
// Node entry and the provider config shipped beside it.
func zcodeDesktopBundle(t *testing.T, root string) (script, builtin string) {
	t.Helper()
	resources := filepath.Join(root, "ZCode.app", "Contents", "Resources")
	script = filepath.Join(resources, "glm", "zcode.cjs")
	builtin = filepath.Join(resources, "config", "provider", "zcode-builtin.json")
	writeZcodeFixture(t, script, "")
	writeZcodeFixture(t, builtin, "{}")
	return script, builtin
}

// zcodeProviderSnapshot returns the process-owned provider repository named in
// env, or "" when prepareZcodeRuntimeEnv did not write one.
func zcodeProviderSnapshot(t *testing.T, env []string) string {
	t.Helper()
	file := zcodeEnvValue(env, zcodePersonalConfigEnv)
	if file == "" {
		return ""
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestZcodeShellWrapperStillImportsDesktopConnections(t *testing.T) {
	// The space covers a quoted path such as "Application Support".
	root := filepath.Join(zcodeFixtureRoot(t), "My Apps")
	script, builtin := zcodeDesktopBundle(t, root)
	writeZcodeFixture(t, filepath.Join(root, ".zcode", "v2", "config.json"), zcodeDesktopConnections("wrapper-secret"))
	wrapper := filepath.Join(root, "bin", "zcode")
	writeZcodeFixture(t, wrapper, "#!/usr/bin/env bash\n# launches ZCode Desktop's CLI\nexec node \""+script+"\" \"$@\"\n")

	env, cleanup, err := prepareZcodeRuntimeEnv(wrapper, []string{"ZCODE_DATA_BASE_DIR=" + root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if got := zcodeEnvValue(env, "ZCODE_BUILTIN_PROVIDER_CONFIG_FILE"); got != builtin {
		t.Fatalf("builtin provider config = %q, want %q", got, builtin)
	}
	if snapshot := zcodeProviderSnapshot(t, env); !strings.Contains(snapshot, "wrapper-secret") {
		t.Fatalf("snapshot %q does not import the enabled Desktop connection", snapshot)
	}
}

func TestZcodeSymlinkedExecutableStillImportsDesktopConnections(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need extra privileges on Windows")
	}
	root := zcodeFixtureRoot(t)
	script, builtin := zcodeDesktopBundle(t, root)
	writeZcodeFixture(t, filepath.Join(root, ".zcode", "v2", "config.json"), zcodeDesktopConnections("symlink-secret"))
	link := filepath.Join(root, "bin", "zcode")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(script, link); err != nil {
		t.Fatal(err)
	}

	env, cleanup, err := prepareZcodeRuntimeEnv(link, []string{"ZCODE_DATA_BASE_DIR=" + root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if got := zcodeEnvValue(env, "ZCODE_BUILTIN_PROVIDER_CONFIG_FILE"); got != builtin {
		t.Fatalf("builtin provider config = %q, want %q", got, builtin)
	}
	if snapshot := zcodeProviderSnapshot(t, env); !strings.Contains(snapshot, "symlink-secret") {
		t.Fatalf("snapshot %q does not import the enabled Desktop connection", snapshot)
	}
}

// The official installer writes ~/.local/bin/zcode as a wrapper around
// <install dir>/current/bin/zcode.mjs, where current is a symlink to the
// installed release and the provider config ships under agent/provider.
func TestZcodeInstallerWrapperResolvesThroughCurrentRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need extra privileges on Windows")
	}
	root := zcodeFixtureRoot(t)
	runtimeDir := filepath.Join(root, ".zcode", "runtime")
	release := filepath.Join(runtimeDir, "releases", "1.2.3")
	writeZcodeFixture(t, filepath.Join(release, "bin", "zcode.mjs"), "")
	builtin := filepath.Join(release, "agent", "provider", "zcode-builtin.json")
	writeZcodeFixture(t, builtin, "{}")
	current := filepath.Join(runtimeDir, "current")
	if err := os.Symlink(release, current); err != nil {
		t.Fatal(err)
	}
	writeZcodeFixture(t, filepath.Join(root, ".zcode", "v2", "config.json"), zcodeDesktopConnections("installer-secret"))
	wrapper := filepath.Join(root, ".local", "bin", "zcode")
	writeZcodeFixture(t, wrapper, "#!/usr/bin/env sh\nexec node \""+current+"/bin/zcode.mjs\" \"$@\"\n")

	env, cleanup, err := prepareZcodeRuntimeEnv(wrapper, []string{"ZCODE_DATA_BASE_DIR=" + root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if got := zcodeEnvValue(env, "ZCODE_BUILTIN_PROVIDER_CONFIG_FILE"); got != builtin {
		t.Fatalf("builtin provider config = %q, want %q", got, builtin)
	}
	if snapshot := zcodeProviderSnapshot(t, env); !strings.Contains(snapshot, "installer-secret") {
		t.Fatalf("snapshot %q does not import the enabled Desktop connection", snapshot)
	}
}

func TestZcodeUnresolvableExecutableLeavesEnvUntouched(t *testing.T) {
	root := zcodeFixtureRoot(t)
	script, _ := zcodeDesktopBundle(t, root)
	writeZcodeFixture(t, filepath.Join(root, ".zcode", "v2", "config.json"), zcodeDesktopConnections("unused-secret"))
	cases := map[string]string{
		"native binary":         "\x7fELF\x02\x01\x01 " + script + "\n",
		"missing script":        "#!/bin/sh\nexec node \"" + filepath.Join(root, "gone", "zcode.cjs") + "\" \"$@\"\n",
		"relative script":       "#!/bin/sh\nexec node ZCode.app/Contents/Resources/glm/zcode.cjs \"$@\"\n",
		"script only mentioned": "#!/bin/sh\n# replaces " + script + "\nexec /opt/zcode/zcode-native \"$@\"\n",
		"too large for a shim":  "#!/bin/sh\n" + strings.Repeat("# padding\n", 8<<10) + "exec node \"" + script + "\" \"$@\"\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			executable := filepath.Join(t.TempDir(), "zcode")
			writeZcodeFixture(t, executable, content)
			in := []string{"ZCODE_DATA_BASE_DIR=" + root}
			env, cleanup, err := prepareZcodeRuntimeEnv(executable, in)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cleanup)
			if strings.Join(env, "\n") != strings.Join(in, "\n") {
				t.Fatalf("env = %q, want it unchanged", env)
			}
		})
	}
}

func TestZcodeDesktopDataDirFollowsSettingFile(t *testing.T) {
	root := zcodeFixtureRoot(t)
	script, _ := zcodeDesktopBundle(t, root)
	home, relocated, explicit := filepath.Join(root, "home"), filepath.Join(root, "relocated"), filepath.Join(root, "explicit")
	for dir, apiKey := range map[string]string{home: "home-secret", relocated: "relocated-secret", explicit: "explicit-secret"} {
		writeZcodeFixture(t, filepath.Join(dir, ".zcode", "v2", "config.json"), zcodeDesktopConnections(apiKey))
	}
	relocatedSetting, err := json.Marshal(map[string]string{"dataBaseDir": relocated})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// Desktop keeps this bootstrap pointer under the real home even after the
	// data dir moves.
	setting := filepath.Join(home, ".zcode", "v2", "setting.json")
	cases := []struct {
		name    string
		setting string // "" leaves the file absent
		env     []string
		want    string
	}{
		{"relocated in Desktop settings", string(relocatedSetting), nil, "relocated-secret"},
		{"explicit env wins", string(relocatedSetting), []string{"ZCODE_DATA_BASE_DIR=" + explicit}, "explicit-secret"},
		{"no setting file", "", nil, "home-secret"},
		{"blank dataBaseDir", `{"dataBaseDir":"  "}`, nil, "home-secret"},
		{"no dataBaseDir", `{"theme":"dark"}`, nil, "home-secret"},
		{"malformed setting file", `{"dataBaseDir":`, nil, "home-secret"},
		{"dataBaseDir is not a string", `{"dataBaseDir":42}`, nil, "home-secret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setting == "" {
				if err := os.Remove(setting); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			} else {
				writeZcodeFixture(t, setting, tc.setting)
			}
			env, cleanup, err := prepareZcodeRuntimeEnv(script, tc.env)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cleanup)
			snapshot := zcodeProviderSnapshot(t, env)
			if !strings.Contains(snapshot, tc.want) {
				t.Fatalf("snapshot %q was not read from the data dir holding %q", snapshot, tc.want)
			}
			if got := zcodeEnvValue(env, "ZCODE_DATA_BASE_DIR"); got != zcodeEnvValue(tc.env, "ZCODE_DATA_BASE_DIR") {
				t.Fatalf("ZCODE_DATA_BASE_DIR = %q, want the caller's value left alone", got)
			}
		})
	}
}
