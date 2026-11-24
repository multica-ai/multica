package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodeArtsProtocolFamilyDispatchesToIndependentBackend(t *testing.T) {
	if !IsSupportedType("codearts") {
		t.Fatal("codearts must be a supported protocol family")
	}
	if IsBuiltinRuntime("codearts") {
		t.Fatal("codearts must not be registered as a built-in runtime identity")
	}
	backend, err := New("codearts", Config{})
	if err != nil {
		t.Fatal(err)
	}
	codearts, ok := backend.(*codeartsBackend)
	if !ok {
		t.Fatalf("New(codearts) = %T, want *codeartsBackend", backend)
	}
	if _, shared := backend.(*opencodeBackend); shared {
		t.Fatal("CodeArts unexpectedly resolved to the OpenCode backend")
	}
	if codearts.cfg.Logger == nil {
		t.Fatal("CodeArts backend did not receive its config")
	}
}

func TestCodeArtsExecuteUsesNativeRunFlagsAndStdin(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	stdinPath := filepath.Join(dir, "stdin.txt")
	prompt := "first line\nsecond line"

	backend, err := ResolveBackend("codearts", Config{
		ExecutablePath: self,
		LaunchPrefix: []string{
			"wrapper", "--format", "text", "--auto", "--dir", "wrong-root",
		},
		Logger: slog.Default(),
		Env: map[string]string{
			opencodeStdinHelperEnv:      "1",
			opencodeStdinHelperArgvFile: argvPath,
			opencodeStdinHelperInFile:   stdinPath,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(t.Context(), prompt, ExecOptions{
		Cwd:             dir,
		Timeout:         30 * time.Second,
		Model:           "huaweicloud-maas/deepseek-v3.2",
		ThinkingLevel:   "high",
		ResumeSessionID: "ses_existing",
		CustomArgs: []string{
			"--dangerously-skip-permissions", "--auto", "--sandbox",
			"--dir", "wrong", "--variant", "high", "--title", "Multica task",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" || result.Output != "ok" || result.SessionID != "ses_fake" {
		t.Fatalf("result = %+v", result)
	}

	argvRaw, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(string(argvRaw), "\n")
	want := []string{"wrapper", "run", "--format", "json", "--auto", "--model", "huaweicloud-maas/deepseek-v3.2", "--session", "ses_existing", "--title", "Multica task"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %#v, want %#v", args, want)
	}
	stdinRaw, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stdinRaw) != prompt {
		t.Fatalf("stdin = %q, want %q", stdinRaw, prompt)
	}
}

func TestCodeArtsRejectsNonJSONSuccessOutput(t *testing.T) {
	backend := &codeartsBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 8)
	result := backend.processEvents(strings.NewReader("\x1b[31mError: CODEARTS_CLI_AK is not configured\x1b[0m\n"), ch)
	if result.status != "failed" {
		t.Fatalf("status = %q, want failed", result.status)
	}
	if !strings.Contains(result.errMsg, "codearts returned no parseable JSON events") ||
		!strings.Contains(result.errMsg, "CODEARTS_CLI_AK is not configured") {
		t.Fatalf("error = %q", result.errMsg)
	}
	if strings.ContainsRune(result.errMsg, '\x1b') {
		t.Fatalf("ANSI escape leaked into error: %q", result.errMsg)
	}
}

func TestCodeArtsStreamErrorsUseCodeArtsLabel(t *testing.T) {
	backend := &codeartsBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 8)
	result := backend.processEvents(strings.NewReader(`{"type":"step_start","sessionID":"ses","part":{}}`+"\n"), ch)
	if result.status != "failed" || !strings.HasPrefix(result.errMsg, "codearts stream ended") {
		t.Fatalf("result = %+v", result)
	}
}

func TestCodeArtsEnvironmentMatchesLauncherAndAllowsOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	oldTLS, hadTLS := os.LookupEnv("NODE_TLS_REJECT_UNAUTHORIZED")
	if err := os.Unsetenv("NODE_TLS_REJECT_UNAUTHORIZED"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadTLS {
			_ = os.Setenv("NODE_TLS_REJECT_UNAUTHORIZED", oldTLS)
		} else {
			_ = os.Unsetenv("NODE_TLS_REJECT_UNAUTHORIZED")
		}
	})
	env := buildCodeArtsEnv(map[string]string{"PLUGIN_ENV": "custom"})
	values := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	if values["SCENARIO"] != "codeartsdoer" || values["PLUGIN_ENV"] != "custom" {
		t.Fatalf("unexpected CodeArts env: SCENARIO=%q PLUGIN_ENV=%q", values["SCENARIO"], values["PLUGIN_ENV"])
	}
	wantConfig := filepath.Join(home, ".codeartsdoer", "codearts_cli.json")
	if values["OPENCODE_CONFIG"] != wantConfig {
		t.Fatalf("OPENCODE_CONFIG = %q, want %q", values["OPENCODE_CONFIG"], wantConfig)
	}
	if _, ok := values["NODE_TLS_REJECT_UNAUTHORIZED"]; ok {
		t.Fatal("CodeArts launcher must not disable TLS verification by default")
	}

	// Disabling certificate verification remains possible only as an explicit
	// per-agent custom_env opt-in; the launcher must never do it by default.
	insecureValues := envValues(buildCodeArtsEnv(map[string]string{"NODE_TLS_REJECT_UNAUTHORIZED": "0"}))
	if insecureValues["NODE_TLS_REJECT_UNAUTHORIZED"] != "0" {
		t.Fatalf("explicit NODE_TLS_REJECT_UNAUTHORIZED override was not preserved: %q", insecureValues["NODE_TLS_REJECT_UNAUTHORIZED"])
	}
}

func TestDiscoverCodeArtsModels(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(codeartsModelHelperEnv, "1")
	argvPath := filepath.Join(t.TempDir(), "model-argv.txt")
	t.Setenv(codeartsModelHelperArgvFile, argvPath)
	models, err := discoverCodeArtsModels(context.Background(), Command{Path: self, Prefix: []string{"wrapper"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "huaweicloud-maas/deepseek-v3.2" || models[0].Provider != "huaweicloud-maas" {
		t.Fatalf("models = %+v", models)
	}
	if models[0].Thinking != nil {
		t.Fatalf("CodeArts model must not advertise OpenCode variants: %+v", models[0])
	}
	argvRaw, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Split(string(argvRaw), "\n"), []string{"wrapper", "models", "--verbose"}; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("model discovery argv = %#v, want %#v", got, want)
	}
}

func TestDetectVersionUsesCodeArtsCommandPrefix(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(codeartsModelHelperEnv, "1")
	argvPath := filepath.Join(t.TempDir(), "version-argv.txt")
	t.Setenv(codeartsModelHelperArgvFile, argvPath)

	version, err := DetectVersion(context.Background(), Command{Path: self, Prefix: []string{"wrapper"}})
	if err != nil {
		t.Fatal(err)
	}
	if version != "CodeArts Agent 1.2.3" {
		t.Fatalf("version = %q, want CodeArts Agent 1.2.3", version)
	}
	argvRaw, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Split(string(argvRaw), "\n"), []string{"wrapper", "--version"}; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("version argv = %#v, want %#v", got, want)
	}
}

func TestCodeArtsListModelsCacheSeparatesCommandPrefixes(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(codeartsModelHelperEnv, "1")
	commands := []Command{
		{Path: self, Prefix: []string{"profile-a"}},
		{Path: self, Prefix: []string{"profile-b"}},
	}
	modelCacheMu.Lock()
	for _, command := range commands {
		delete(modelCache, discoveryCacheKey("codearts", command))
	}
	modelCacheMu.Unlock()
	t.Cleanup(func() {
		modelCacheMu.Lock()
		defer modelCacheMu.Unlock()
		for _, command := range commands {
			delete(modelCache, discoveryCacheKey("codearts", command))
		}
	})

	for i, command := range commands {
		catalog, err := ListModels(context.Background(), "codearts", command)
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("test/profile-%c", 'a'+rune(i))
		if len(catalog.Models) != 1 || catalog.Models[0].ID != want {
			t.Fatalf("prefix %q catalog = %+v, want model %q", command.Prefix[0], catalog, want)
		}
	}
}

func envValues(env []string) map[string]string {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}

func TestResolveCodeArtsNativeFromShim(t *testing.T) {
	shim := filepath.Join("C:\\", "Users", "test", ".codeartsdoer", "installers", "codearts.cmd")
	native := filepath.Join(filepath.Dir(shim), "bin", "codearts.exe")
	if got := resolveCodeArtsNativeFromShim(shim, fakeStat(native)); got != native {
		t.Fatalf("got %q, want %q", got, native)
	}
	if got := resolveCodeArtsNativeFromShim(filepath.Join(filepath.Dir(shim), "other.cmd"), fakeStat(native)); got != "" {
		t.Fatalf("non-CodeArts shim resolved to %q", got)
	}
}
