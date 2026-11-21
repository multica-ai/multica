package daemon

import (
	"context"
	"encoding/json"

	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestResidentResumeContextAtStableRoot(t *testing.T) {
	t.Setenv("MULTICA_HERMES_RESIDENT_GATEWAY", "1")
	t.Setenv("HERMES_HOME", t.TempDir())
	d, _, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()
	d.activeStores = make(map[string]int)
	d.cfg.AgentTimeout = 30 * time.Second
	prior := leaderReuseTestTask("prior-run")
	env, err := execenv.Prepare(execenv.PrepareParams{
		WorkspacesRoot: d.cfg.WorkspacesRoot,
		WorkspaceID:    prior.WorkspaceID,
		TaskID:         prior.ID,
		Provider:       "hermes",
		Task:           execenv.TaskContextForEnv{AgentID: prior.AgentID, IssueID: prior.IssueID},
	}, d.logger)
	if err != nil {
		t.Fatal(err)
	}
	writeLeaderManagedEnvProvenance(t, env.WorkDir, prior.WorkspaceID, prior.IssueID, prior.AgentID)
	env.ReleaseLock()
	task := leaderReuseTestTask("resumed-run")
	task.PriorSessionID = "session-prior"
	task.PriorWorkDir = env.WorkDir
	if _, ok := shouldReusePriorWorkdir(task, nil, d.cfg.WorkspacesRoot); !ok {
		t.Fatal("fixture prior workdir is not reusable")
	}
	stable, err := execenv.ResolveRootDir(taskRootDirParams(d.cfg.WorkspacesRoot, task))
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "context.json")
	bin := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, bin, []byte("#!/bin/sh\ncase \"$*\" in *--version*) exit 0;; esac\npwd > '"+capture+".cwd'\ncp '"+filepath.Join(stable, residentTaskContextFile)+"' '"+capture+"'\nexit 1\n"))
	d.cfg.Agents["hermes"] = AgentEntry{Path: bin}
	d.runtimeIndex[task.RuntimeID] = Runtime{ID: task.RuntimeID, Provider: "hermes"}
	// The fake ACP exits after observing the handoff; no real agent is executed.
	_, runErr := d.runTask(context.Background(), task, "hermes", 0, d.logger)
	t.Logf("fake ACP result: %v", runErr)
	cwd, _ := os.ReadFile(capture + ".cwd")
	canonicalWorkDir, err := filepath.EvalSymlinks(env.WorkDir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(cwd)) != canonicalWorkDir {
		t.Fatalf("did not reuse prior root: cwd=%s prior=%s", cwd, env.WorkDir)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("resumed task handoff absent at stable root: %v", err)
	}
	var got residentTaskContext
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.TaskID != task.ID || got.Token != task.AuthToken {
		t.Fatal("resumed handoff did not contain current task credential")
	}
	for _, root := range []string{stable, env.RootDir} {
		if _, err := os.Stat(filepath.Join(root, residentTaskContextFile)); !os.IsNotExist(err) {
			t.Errorf("handoff not cleaned up: %v", err)
		}
	}
}

func TestResidentTurnEnvironmentConfig(t *testing.T) {
	for _, mode := range []string{"1", "", "true"} {
		t.Run("mode="+mode, func(t *testing.T) {
			t.Setenv("MULTICA_HERMES_RESIDENT_GATEWAY", mode)
			shared := t.TempDir()
			t.Setenv("HERMES_HOME", shared)
			d, _, cleanup := newLeaderReuseTestDaemon(t)
			defer cleanup()
			d.activeStores = make(map[string]int)
			d.cfg.AgentTimeout = 30 * time.Second
			task := leaderReuseTestTask("env-run")
			task.Agent.Skills = []SkillData{{Name: "Helper", Content: "Help."}}
			task.Agent.CustomEnv = map[string]string{
				"SERVICE_KEY": "secret-sentinel", "EMPTY_KEY": "",
				"MULTICA_TOKEN": "blocked-token", "MULTICA_FAKE": "blocked-internal",
				"PATH": "blocked-path", "home": "blocked-home",
			}
			capture := filepath.Join(t.TempDir(), "config.yaml")
			bin := filepath.Join(t.TempDir(), "hermes")
			writeTestExecutable(t, bin, []byte("#!/bin/sh\ncase \"$*\" in *--version*) exit 0;; esac\ncp \"$HERMES_HOME/config.yaml\" '"+capture+"'\nexit 1\n"))
			d.cfg.Agents["hermes"] = AgentEntry{Path: bin}
			d.runtimeIndex[task.RuntimeID] = Runtime{ID: task.RuntimeID, Provider: "hermes"}
			_, _ = d.runTask(context.Background(), task, "hermes", 0, d.logger)
			data, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			var cfg struct {
				Terminal struct {
					Names []string `yaml:"client_env_turn_passthrough"`
				}
			}
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				t.Fatal(err)
			}
			var want []string
			if mode == "1" {
				want = append(execenv.HermesTaskEnvPassthroughNames(), "EMPTY_KEY", "SERVICE_KEY")
				sort.Strings(want)
			}
			if !reflect.DeepEqual(cfg.Terminal.Names, want) {
				t.Errorf("turn names = %v, want %v", cfg.Terminal.Names, want)
			}
			for _, value := range task.Agent.CustomEnv {
				if value != "" && strings.Contains(string(data), value) {
					t.Error("custom environment value persisted in config")
				}
			}
			if strings.Contains(string(data), task.AuthToken) {
				t.Error("task token persisted in config")
			}
		})
	}
}
