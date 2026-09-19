package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestTaskClaudePluginHelper(t *testing.T) {
	if len(os.Args) > 1 && os.Args[len(os.Args)-1] == "fixture-prepare" {
		if err := execenv.RunPreparationHelper(os.Stdin, os.Stdout, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	if os.Getenv("MULTICA_TEST_PLUGIN_QUERY") != "1" {
		return
	}
	cwd, _ := os.Getwd()
	want := os.Getenv("MULTICA_TEST_PLUGIN_CWD")
	actualDir, actualErr := os.Stat(cwd)
	wantDir, wantErr := os.Stat(want)
	if actualErr != nil || wantErr != nil || !os.SameFile(actualDir, wantDir) || !strings.Contains(strings.Join(os.Args, " "), "plugin list --json") {
		_, _ = fmt.Fprintf(os.Stderr, "unexpected plugin query context: cwd=%q, want=%q, args=%v", cwd, want, os.Args)
		os.Exit(3)
	}
	_, _ = io.WriteString(os.Stdout, os.Getenv("MULTICA_TEST_PLUGIN_INVENTORY"))
	os.Exit(0)
}

func TestTaskClaudePluginPolicyPrepareReuse(t *testing.T) {
	t.Parallel()
	for _, isolated := range []bool{false, true} {
		t.Run(map[bool]string{false: "in-process", true: "isolated"}[isolated], func(t *testing.T) {
			home, source := t.TempDir(), t.TempDir()
			for name, content := range map[string]string{
				".claude-plugin/plugin.json": `{"name":"paper"}`,
				"skills/hidden/SKILL.md":     "hidden", "skills/visible/SKILL.md": "visible",
			} {
				path := filepath.Join(source, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			if err := os.MkdirAll(filepath.Join(home, ".claude/plugins"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, ".claude/plugins/known_marketplaces.json"), []byte(`{"market":{"source":{"source":"github","repo":"fixture/market"}}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			d := &Daemon{logger: logger}
			if isolated {
				d.executionEnvironmentCommand = func() ([]string, error) {
					return []string{os.Args[0], "-test.run=^TestTaskClaudePluginHelper$", "--", "fixture-prepare"}, nil
				}
			}
			task := execenv.TaskContextForEnv{IssueID: "plugin-task", DisabledRuntimeSkills: []execenv.RuntimeSkillRefForEnv{{Root: "plugin", Plugin: "paper@market", Key: "paper:hidden"}}}
			env, err := d.prepareExecutionEnvironment(context.Background(), execenv.PrepareParams{WorkspacesRoot: t.TempDir(), WorkspaceID: "ws", TaskID: "task", Provider: "claude", Task: task})
			if err != nil {
				t.Fatal(err)
			}
			defer env.Cleanup(true)
			for _, mode := range []string{"enabled", "project-disabled", "globally-disabled", "uninstalled", "enabled"} {
				if err := env.CleanupClaudePluginCopies(); err != nil {
					t.Fatal(err)
				}
				if env, err = d.reuseExecutionEnvironment(context.Background(), execenv.ReuseParams{WorkDir: env.WorkDir, Provider: "claude", Task: task}); err != nil || env == nil {
					t.Fatalf("reuse: %v", err)
				}
				inventory := []map[string]any{}
				if mode != "uninstalled" {
					inventory = append(inventory, map[string]any{"id": "paper@market", "scope": "user", "enabled": mode == "enabled", "installPath": source})
				}
				raw, _ := json.Marshal(inventory)
				cfg := agent.Config{ExecutablePath: os.Args[0], LaunchPrefix: []string{"-test.run=^TestTaskClaudePluginHelper$", "--"}, Logger: logger, Env: map[string]string{
					"HOME": home, "USERPROFILE": home, "CLAUDE_CONFIG_DIR": filepath.Join(home, ".claude"),
					"MULTICA_TEST_PLUGIN_QUERY": "1", "MULTICA_TEST_PLUGIN_CWD": env.WorkDir, "MULTICA_TEST_PLUGIN_INVENTORY": string(raw),
				}}
				if err := d.prepareTaskClaudePlugins(context.Background(), cfg, agent.ExecOptions{Cwd: env.WorkDir, ClaudeSettingsPath: env.ClaudeSettingsPath}, env, task.DisabledRuntimeSkills); err != nil {
					t.Fatalf("%s: %v", mode, err)
				}
				if mode != "enabled" {
					if len(env.ClaudePluginDirs) != 0 {
						t.Fatalf("%s was re-enabled", mode)
					}
					continue
				}
				if len(env.ClaudePluginDirs) != 1 {
					t.Fatalf("copies: %v", env.ClaudePluginDirs)
				}
				if _, err := os.Stat(filepath.Join(env.ClaudePluginDirs[0], "skills/hidden/SKILL.md")); !os.IsNotExist(err) {
					t.Fatalf("hidden skill copied: %v", err)
				}
				if _, err := os.Stat(filepath.Join(env.ClaudePluginDirs[0], "skills/visible/SKILL.md")); err != nil {
					t.Fatal(err)
				}
			}
			if err := env.CleanupClaudePluginCopies(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTaskClaudePluginPolicySkipsUnrelatedTasks(t *testing.T) {
	t.Parallel()
	d := &Daemon{}
	if err := d.prepareTaskClaudePlugins(context.Background(), agent.Config{ExecutablePath: filepath.Join(t.TempDir(), "missing")}, agent.ExecOptions{}, &execenv.Environment{}, []execenv.RuntimeSkillRefForEnv{{Root: "user", Key: "ordinary"}}); err != nil {
		t.Fatal(err)
	}
}
