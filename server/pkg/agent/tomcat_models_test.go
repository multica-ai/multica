package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// Re-execute this test binary rather than resolving an installed agent CLI.
func TestTomcatModelsHelperProcess(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		return
	}
	args := os.Args[separator+1:]
	if len(args) != 5 || args[2] != "profile with spaces" || args[3] != "serve" || args[4] != "models" {
		os.Exit(91)
	}
	root, mode := args[0], args[1]
	trace, _ := json.Marshal(args)
	if os.WriteFile(filepath.Join(root, "args.json"), trace, 0600) != nil {
		os.Exit(92)
	}
	if mode == "hang" {
		self, _ := os.Executable()
		child := exec.Command(self, "-test.run=^TestTomcatHelperProcess$")
		child.Env = append(os.Environ(), "MULTICA_TOMCAT_TEST_HELPER=child", "TOMCAT_TEST_ROOT="+root)
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if child.Start() != nil {
			os.Exit(93)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	stdout, err := os.ReadFile(filepath.Join(root, "stdout"))
	if err != nil {
		os.Exit(94)
	}
	_, _ = os.Stdout.Write(stdout)
	if stderr, err := os.ReadFile(filepath.Join(root, "stderr")); err == nil {
		_, _ = os.Stderr.Write(stderr)
	}
	if mode == "fail" {
		os.Exit(2)
	}
	os.Exit(0)
}

func tomcatModelsFixture(t *testing.T, mode, stdout string) (Command, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "stdout"), []byte(stdout), 0600); err != nil {
		t.Fatal(err)
	}
	return NewCommand(self, []string{"-test.run=^TestTomcatModelsHelperProcess$", "--", root, mode, "profile with spaces"}), root
}

func TestTomcatModelsPreserveIDsCommandIdentityAndFreshCatalog(t *testing.T) {
	const payload = `{"models":[{"id":"alias-one","provider":"team","keyPresent":true,"modelName":"same-wire"},{"id":"team/alias-two","provider":"team","keyPresent":true,"modelName":"same-wire"},{"id":"missing-key","provider":"other","keyPresent":false}],"defaultModelId":"alias-one","utilityModelId":"team/alias-two","warnings":["retired override skipped"],"futureMetadata":{}}`
	cmd, root := tomcatModelsFixture(t, "ok", payload)
	prefix := append([]string(nil), cmd.Prefix...)
	catalog, err := ListModels(t.Context(), "tomcat", cmd)
	want := []Model{{ID: "alias-one", Label: "alias-one", Provider: "team", Default: true}, {ID: "team/alias-two", Label: "team/alias-two", Provider: "team"}}
	if err != nil || !reflect.DeepEqual(catalog.Models, want) || catalog.Fallback || len(catalog.Unavailable) != 0 {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}
	if !reflect.DeepEqual(cmd.Prefix, prefix) {
		t.Fatal("discovery mutated the command prefix")
	}
	trace, err := os.ReadFile(filepath.Join(root, "args.json"))
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	if json.Unmarshal(trace, &args) != nil || !reflect.DeepEqual(args, []string{root, "ok", "profile with spaces", "serve", "models"}) {
		t.Fatalf("wrong discovery argv: %s", trace)
	}
	// The same command must observe saved changes immediately, not a 60s memo.
	updated := `{"models":[{"id":"new-id","provider":"new-provider","keyPresent":true}]}`
	if err := os.WriteFile(filepath.Join(root, "stdout"), []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	fresh, err := ListModels(t.Context(), "tomcat", cmd)
	if err != nil || len(fresh.Models) != 1 || fresh.Models[0].ID != "new-id" {
		t.Fatalf("cached catalog after refresh: %+v, %v", fresh, err)
	}
	other, _ := tomcatModelsFixture(t, "ok", payload)
	isolated, err := ListModels(t.Context(), "tomcat", other)
	if err != nil || !reflect.DeepEqual(isolated.Models, want) {
		t.Fatalf("another profile inherited the first catalog: %+v, %v", isolated, err)
	}
	// Default-name completion must retain the exact same launch prefix.
	name := "tomcat"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary, err := os.ReadFile(cmd.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), binary, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	cmd.Path = ""
	completed, err := ListModels(t.Context(), "tomcat", cmd)
	if err != nil || !reflect.DeepEqual(completed.Models, fresh.Models) {
		t.Fatalf("default-name completion lost the profile: %+v, %v", completed, err)
	}
}

func TestTomcatModelsExposeRuntimeTiersAndDefaultWithoutGuessing(t *testing.T) {
	cmd, _ := tomcatModelsFixture(t, "ok", `{"defaultModelId":"qwen-relay","models":[{"id":"qwen-relay","provider":"relay","keyPresent":true,"supportedReasoningLevels":["low","high","xhigh"]},{"id":"toggle-only","provider":"relay","keyPresent":true,"supportedReasoningLevels":[]},{"id":"no-key","provider":"relay","keyPresent":false,"supportedReasoningLevels":["max"]}]}`)
	catalog, err := ListModels(t.Context(), "tomcat", cmd)
	if err != nil {
		t.Fatal(err)
	}
	want := []Model{
		{ID: "qwen-relay", Label: "qwen-relay", Provider: "relay", Default: true, Thinking: &ModelThinking{SupportedLevels: []ThinkingLevel{{Value: "low", Label: "Low"}, {Value: "high", Label: "High"}, {Value: "xhigh", Label: "Extra high"}}}},
		{ID: "toggle-only", Label: "toggle-only", Provider: "relay"},
	}
	if !reflect.DeepEqual(catalog.Models, want) {
		t.Fatalf("catalog=%+v, want %+v", catalog.Models, want)
	}
	if !ThinkingControlSupported("tomcat") || !IsKnownThinkingValue("tomcat", "high") {
		t.Fatal("Tomcat dynamic effort capability is not registered")
	}
}

func TestTomcatModelsRejectInvalidOrUnavailableCatalogs(t *testing.T) {
	for name, stdout := range map[string]string{
		"empty": "", "text": "PRIVATE_STDOUT_SENTINEL", "null": "null", "array": "[]",
		"missing models": `{}`, "null models": `{"models":null}`, "wrong models type": `{"models":{}}`,
		"missing id":        `{"models":[{"provider":"p","keyPresent":true}]}`,
		"blank id":          `{"models":[{"id":" ","provider":"p","keyPresent":true}]}`,
		"wrong id type":     `{"models":[{"id":1,"provider":"p","keyPresent":true}]}`,
		"missing provider":  `{"models":[{"id":"m","keyPresent":true}]}`,
		"missing key state": `{"models":[{"id":"m","provider":"p"}]}`,
		"null key state":    `{"models":[{"id":"m","provider":"p","keyPresent":null}]}`,
		"wrong key state":   `{"models":[{"id":"m","provider":"p","keyPresent":"true"}]}`,
		"duplicate":         `{"models":[{"id":"m","provider":"p","keyPresent":true},{"id":"m","provider":"p","keyPresent":false}]}`,
		"trailing data":     `{"models":[]} PRIVATE_STDOUT_SENTINEL`,
		"empty models":      `{"models":[]}`,
		"no credentials":    `{"models":[{"id":"m","provider":"p","keyPresent":false}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			cmd, _ := tomcatModelsFixture(t, "ok", stdout)
			catalog, err := ListModels(t.Context(), "tomcat", cmd)
			if err == nil || len(catalog.Models) != 0 || catalog.Fallback || len(catalog.Unavailable) != 0 {
				t.Fatalf("invalid catalog became a success: %+v, %v", catalog, err)
			}
			if strings.Contains(err.Error(), "PRIVATE_STDOUT_SENTINEL") {
				t.Fatalf("stdout leaked into discovery error: %v", err)
			}
			if (name == "empty models" || name == "no credentials") && !strings.Contains(err.Error(), "saved credentials") {
				t.Fatalf("missing credential remedy: %v", err)
			}
		})
	}
}

func TestTomcatModelsReportBoundedStderrAndMissingCommands(t *testing.T) {
	cmd, root := tomcatModelsFixture(t, "fail", "PRIVATE_STDOUT_SENTINEL")
	stderr := strings.Repeat("noise\n", probeStderrSampleBytes) + "error: unrecognized subcommand 'models'"
	if err := os.WriteFile(filepath.Join(root, "stderr"), []byte(stderr), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := ListModels(t.Context(), "tomcat", cmd)
	if err == nil || !strings.Contains(err.Error(), "unrecognized subcommand 'models'") || !strings.Contains(err.Error(), "update") || strings.Contains(err.Error(), "PRIVATE_STDOUT_SENTINEL") || len(err.Error()) > probeStderrSampleBytes+512 {
		t.Fatalf("unsafe or unhelpful command error: %v", err)
	}
	// PATH is private: the empty command case must not find an installed CLI.
	t.Setenv("PATH", t.TempDir())
	for _, missing := range []Command{NewCommand(filepath.Join(t.TempDir(), "missing-tomcat"), nil), NewCommand("", []string{"profile-prefix"})} {
		if catalog, err := ListModels(t.Context(), "tomcat", missing); err == nil || len(catalog.Models) != 0 {
			t.Fatalf("missing command became an empty success: %+v, %v", catalog, err)
		}
	}
}

func TestTomcatModelsCancellationAndDeadlineReapDescendants(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "deadline"}[deadline], func(t *testing.T) {
			cmd, root := tomcatModelsFixture(t, "hang", "")
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := ListModels(ctx, "tomcat", cmd); done <- err }()
			path := filepath.Join(root, "ticks")
			ready := time.Now().Add(4 * time.Second)
			for {
				if bytes, _ := os.ReadFile(path); len(bytes) > 0 {
					break
				}
				if time.Now().After(ready) {
					t.Fatal("helper descendant did not start")
				}
				time.Sleep(10 * time.Millisecond)
			}
			want := context.DeadlineExceeded
			if !deadline {
				want = context.Canceled
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, want) {
					t.Fatalf("expected %v, got %v", want, err)
				}
			case <-time.After(8 * time.Second):
				t.Fatal("discovery did not return after cancellation")
			}
			before, _ := os.ReadFile(path)
			time.Sleep(120 * time.Millisecond)
			after, _ := os.ReadFile(path)
			if string(before) != string(after) {
				t.Fatal("discovery left its descendant running")
			}
		})
	}
}
