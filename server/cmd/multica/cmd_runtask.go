package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
	logger_pkg "github.com/multica-ai/multica/server/internal/logger"
)

var runTaskCmd = &cobra.Command{
	Use:   "run-task",
	Short: "Execute a single task and exit (used by K8s controller-spawned Job pods)",
	Long: `Execute exactly one task end-to-end: prepare workdir, spawn the agent CLI,
stream messages back to Multica, report completion, and exit.

The task payload (JSON-encoded Task struct) is read from --task-file. The
controller writes this file via a ConfigMap or downward API; the operator
can also point it at any file for manual one-off runs.

Auth: MULTICA_TOKEN env (recommended) or ~/.multica/config.json fallback.
Server URL: --server-url flag, MULTICA_SERVER_URL env, or multica config.

Exit codes:
  0 — task completed (success OR failure reported to Multica)
  1 — setup error (couldn't read payload, no auth, bad config)`,
	RunE: runRunTask,
}

func init() {
	runTaskCmd.Flags().String("task-file", "", "Path to a JSON file containing the full Task payload (REQUIRED)")
	runTaskCmd.Flags().String("workspaces-root", "/work", "Base directory for per-task workdirs (or MULTICA_WORKSPACES_ROOT env)")
	runTaskCmd.Flags().Int("health-port", 0, "Local helper HTTP port (0 = OS-picked)")
	runTaskCmd.Flags().Duration("agent-timeout", 2*time.Hour, "Per-task agent timeout")
}

func runRunTask(cmd *cobra.Command, _ []string) error {
	taskFile, _ := cmd.Flags().GetString("task-file")
	if taskFile == "" {
		return fmt.Errorf("--task-file is required")
	}

	raw, err := os.ReadFile(taskFile)
	if err != nil {
		return fmt.Errorf("read task file %q: %w", taskFile, err)
	}
	var task daemon.Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return fmt.Errorf("decode task payload: %w", err)
	}
	if task.ID == "" {
		return fmt.Errorf("task payload missing id")
	}
	if task.RuntimeID == "" {
		return fmt.Errorf("task payload missing runtime_id")
	}

	// Server URL: --server-url flag > MULTICA_SERVER_URL env > config file.
	serverURL := cli.FlagOrEnv(cmd, "server-url", "MULTICA_SERVER_URL", "")
	if serverURL == "" {
		profile := resolveProfile(cmd)
		if c, err := cli.LoadCLIConfigForProfile(profile); err == nil && c.ServerURL != "" {
			serverURL = c.ServerURL
		}
	}
	if serverURL == "" {
		return fmt.Errorf("server URL not set: use --server-url, MULTICA_SERVER_URL env, or multica config")
	}
	normalizedServerURL, err := daemon.NormalizeServerBaseURL(serverURL)
	if err != nil {
		return err
	}

	wsRoot, _ := cmd.Flags().GetString("workspaces-root")
	if env := strings.TrimSpace(os.Getenv("MULTICA_WORKSPACES_ROOT")); env != "" {
		wsRoot = env
	}

	healthPort, _ := cmd.Flags().GetInt("health-port")
	agentTimeout, _ := cmd.Flags().GetDuration("agent-timeout")

	agents, err := daemon.ProbeAgents()
	if err != nil {
		return fmt.Errorf("probe agents: %w", err)
	}

	cfg := daemon.Config{
		ServerBaseURL:  normalizedServerURL,
		WorkspacesRoot: wsRoot,
		HealthPort:     healthPort,
		Agents:         agents,
		AgentTimeout:   agentTimeout,
		CLIVersion:     version,
	}

	logger := logger_pkg.NewLogger("run-task")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	r, err := daemon.NewSingleTaskRunner(cfg, logger)
	if err != nil {
		return fmt.Errorf("init runner: %w", err)
	}
	defer r.Close()

	// Provider lives on the Runtime row server-side. In single-task mode there's
	// no registration loop populating runtimeIndex, so we seed it from the
	// runtime metadata the controller (or the operator running this command
	// manually) is expected to have pre-claimed. AgentData on the task payload
	// does NOT carry the provider; we read it from MULTICA_RUNTIME_PROVIDER,
	// falling back to the only provider on PATH if there's exactly one (the
	// runtime-claude container image always satisfies that case).
	provider := strings.TrimSpace(os.Getenv("MULTICA_RUNTIME_PROVIDER"))
	if provider == "" {
		if len(agents) == 1 {
			for p := range agents {
				provider = p
			}
		} else {
			return fmt.Errorf("MULTICA_RUNTIME_PROVIDER env is required when multiple agent CLIs are present (found: %v)", sortedKeys(agents))
		}
	}
	if _, ok := agents[provider]; !ok {
		return fmt.Errorf("MULTICA_RUNTIME_PROVIDER=%q but no matching agent CLI is installed (found: %v)", provider, sortedKeys(agents))
	}
	r.SeedRuntime(task.RuntimeID, provider)

	logger.Info("starting single-task run",
		"task_id", task.ID,
		"workspace_id", task.WorkspaceID,
		"runtime_id", task.RuntimeID,
		"provider", provider,
		"health_port", r.HealthPort(),
	)

	if err := r.RunOneTask(ctx, task); err != nil {
		return err
	}

	logger.Info("single-task run complete", "task_id", task.ID)
	return nil
}

func sortedKeys(m map[string]daemon.AgentEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Cheap insertion sort — n is tiny (≤ 11 agent providers).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
