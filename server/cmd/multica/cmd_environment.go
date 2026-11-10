package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var environmentCmd = &cobra.Command{
	Use:     "environment",
	Aliases: []string{"env"},
	Short:   "Start and stop the environments a repository declares",
	Long: "Runs the environments declared in the repository's .multica/environments.toml under the daemon, " +
		"so they keep running after the task ends and show in the issue sidebar.\n\n" +
		"Each environment gets a port block of its own: MULTICA_PORT through MULTICA_PORT+9. " +
		"Its output goes to a log file the daemon keeps, and it is stopped when its TTL runs out.\n\n" +
		"Example .multica/environments.toml:\n\n" +
		"  [environments.web]\n" +
		"  label = \"Web app\"\n" +
		"  start = \"pnpm dev --port $MULTICA_PORT\"\n" +
		"  # url defaults to http://localhost:$MULTICA_PORT; url = \"\" for none\n" +
		"  # ready = \"http://localhost:$MULTICA_PORT/health\"\n" +
		"  # stop = \"docker compose down\"\n" +
		"  ttl = \"24h\"\n\n" +
		"These commands run inside a daemon task; they use the task's credential.",
}

var environmentStartCmd = &cobra.Command{
	Use:   "start <name>",
	Short: "Start a declared environment, or return it if it is already running",
	Long: "Starts the named environment from the .multica/environments.toml found in the current directory or a parent. " +
		"Starting one that is already running on this issue returns it unchanged. " +
		"By default the command waits until the environment answers at its ready URL and prints its URL.",
	Args: exactArgs(1),
	RunE: runEnvironmentStart,
}

var environmentStopCmd = &cobra.Command{
	Use:   "stop <name>",
	Short: "Stop a running environment",
	Long:  "Runs the environment's stop command, if it declares one, then stops whatever is left of its processes.",
	Args:  exactArgs(1),
	RunE:  runEnvironmentStop,
}

var environmentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List this issue's environments on this machine",
	Args:  cobra.NoArgs,
	RunE:  runEnvironmentList,
}

func init() {
	environmentStartCmd.Flags().Bool("wait", true, "wait until the environment is ready (or exits) before returning")
	environmentStartCmd.Flags().String("output", "table", "Output format: table or json")
	environmentStopCmd.Flags().String("output", "table", "Output format: table or json")
	environmentListCmd.Flags().String("output", "table", "Output format: table or json")

	environmentCmd.AddCommand(environmentStartCmd)
	environmentCmd.AddCommand(environmentStopCmd)
	environmentCmd.AddCommand(environmentListCmd)
}

// daemonEnvironment mirrors localenv.Environment as the daemon serves it.
type daemonEnvironment struct {
	Key       string     `json:"key"`
	Name      string     `json:"name"`
	Label     string     `json:"label"`
	URL       string     `json:"url"`
	Port      int        `json:"port"`
	State     string     `json:"state"`
	Detail    string     `json:"detail"`
	StartedAt time.Time  `json:"started_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	LogPath   string     `json:"log_path"`
}

type daemonEnvironmentResponse struct {
	Environment  *daemonEnvironment  `json:"environment"`
	Environments []daemonEnvironment `json:"environments"`
}

func runEnvironmentStart(cmd *cobra.Command, args []string) error {
	wait, _ := cmd.Flags().GetBool("wait")
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	var resp daemonEnvironmentResponse
	err = callDaemonEnvironment(cmd.Context(), "/environments/start", map[string]any{
		"name": args[0],
		"dir":  dir,
		"path": os.Getenv("PATH"),
		"wait": wait,
	}, &resp, 11*time.Minute)
	if err != nil {
		return err
	}
	if resp.Environment == nil {
		return fmt.Errorf("daemon returned no environment")
	}
	env := *resp.Environment
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		if err := cli.PrintJSON(os.Stdout, env); err != nil {
			return err
		}
	} else if env.URL != "" {
		fmt.Fprintln(os.Stdout, env.URL)
	}
	switch env.State {
	case "exited", "stopped":
		return fmt.Errorf("environment %s %s: %s (log: %s)", env.Name, env.State, env.Detail, env.LogPath)
	case "starting":
		if wait {
			fmt.Fprintf(os.Stderr, "Environment %s is still starting: %s (log: %s)\n", env.Name, env.Detail, env.LogPath)
			return nil
		}
	}
	fmt.Fprintln(os.Stderr, environmentStartSummary(env))
	return nil
}

func environmentStartSummary(env daemonEnvironment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Environment %s is %s", env.Name, env.State)
	if env.URL != "" {
		fmt.Fprintf(&b, " at %s", env.URL)
	}
	fmt.Fprintf(&b, " (ports %d-%d). Log: %s.", env.Port, env.Port+9, env.LogPath)
	if env.ExpiresAt != nil {
		fmt.Fprintf(&b, " It stops at %s", env.ExpiresAt.Local().Format(time.RFC3339))
	}
	fmt.Fprintf(&b, "; stop it sooner with `multica env stop %s`.", env.Name)
	return b.String()
}

func runEnvironmentStop(cmd *cobra.Command, args []string) error {
	var resp daemonEnvironmentResponse
	if err := callDaemonEnvironment(cmd.Context(), "/environments/stop", map[string]any{"name": args[0]}, &resp, 2*time.Minute); err != nil {
		return err
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" && resp.Environment != nil {
		return cli.PrintJSON(os.Stdout, *resp.Environment)
	}
	fmt.Fprintf(os.Stdout, "Stopped %s.\n", args[0])
	return nil
}

func runEnvironmentList(cmd *cobra.Command, _ []string) error {
	var resp daemonEnvironmentResponse
	if err := callDaemonEnvironment(cmd.Context(), "/environments/list", map[string]any{}, &resp, 30*time.Second); err != nil {
		return err
	}
	if resp.Environments == nil {
		resp.Environments = []daemonEnvironment{}
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp.Environments)
	}
	if len(resp.Environments) == 0 {
		fmt.Fprintln(os.Stderr, "No environments.")
		return nil
	}
	rows := make([][]string, 0, len(resp.Environments))
	for _, env := range resp.Environments {
		rows = append(rows, []string{env.Name, env.State, env.URL, env.LogPath})
	}
	cli.PrintTable(os.Stdout, []string{"NAME", "STATE", "URL", "LOG"}, rows)
	return nil
}

func callDaemonEnvironment(parent context.Context, path string, body, out any, timeout time.Duration) error {
	daemonPort := os.Getenv("MULTICA_DAEMON_PORT")
	if daemonPort == "" {
		return fmt.Errorf("MULTICA_DAEMON_PORT not set (this command is intended to be run by an agent inside a daemon task)")
	}
	taskToken := os.Getenv("MULTICA_TOKEN")
	if taskToken == "" {
		return fmt.Errorf("MULTICA_TOKEN not set (environment commands require the active task credential)")
	}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://127.0.0.1:%s%s", daemonPort, path), bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create daemon request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+taskToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read daemon response: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound && strings.HasPrefix(string(respBody), "404 page not found") {
		return fmt.Errorf("this daemon does not support environments yet; update it with `multica update`")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", strings.TrimSpace(string(respBody)))
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("parse daemon response: %w", err)
	}
	return nil
}
