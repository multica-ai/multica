package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var previewCmd = &cobra.Command{
	Use:   "preview",
	Short: "Manage daemon-hosted local previews",
}

var previewStartCmd = &cobra.Command{
	Use:   "start --cwd <dir> -- <command> [args...]",
	Short: "Start or reuse a daemon-hosted local preview",
	Args:  cobra.ArbitraryArgs,
	RunE:  runPreviewStart,
}

var previewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List local previews managed by the daemon",
	RunE:  runPreviewList,
}

var previewStatusCmd = &cobra.Command{
	Use:   "status <preview-id>",
	Short: "Show local preview status",
	Args:  exactArgs(1),
	RunE:  runPreviewStatus,
}

var previewLogsCmd = &cobra.Command{
	Use:   "logs <preview-id>",
	Short: "Show local preview logs",
	Args:  exactArgs(1),
	RunE:  runPreviewLogs,
}

var previewStopCmd = &cobra.Command{
	Use:   "stop <preview-id>",
	Short: "Stop a local preview",
	Args:  exactArgs(1),
	RunE:  runPreviewStop,
}

var previewRestartCmd = &cobra.Command{
	Use:   "restart <preview-id>",
	Short: "Restart a local preview",
	Args:  exactArgs(1),
	RunE:  runPreviewRestart,
}

var previewGCCmd = &cobra.Command{
	Use:   "gc",
	Short: "Garbage collect stale local previews",
	RunE:  runPreviewGC,
}

func init() {
	sf := previewStartCmd.Flags()
	sf.String("scope", "issue", "Preview scope")
	sf.String("issue", "", "Issue ID associated with this preview")
	sf.String("cwd", "", "Working directory for the preview command")
	sf.Int("port", 0, "Local preview port")
	sf.String("url", "", "Local preview URL")
	sf.String("health-url", "", "URL used for health checks")
	sf.Bool("restart", false, "Restart an existing matching preview instead of reusing it")
	sf.String("output", "json", "Output format: table or json")

	previewListCmd.Flags().String("output", "table", "Output format: table or json")
	previewStatusCmd.Flags().String("output", "json", "Output format: table or json")
	previewLogsCmd.Flags().IntP("lines", "n", 200, "Approximate number of log lines to show")
	previewStopCmd.Flags().String("output", "json", "Output format: table or json")
	previewRestartCmd.Flags().String("output", "json", "Output format: table or json")
	previewGCCmd.Flags().String("output", "json", "Output format: table or json")

	previewCmd.AddCommand(previewStartCmd)
	previewCmd.AddCommand(previewListCmd)
	previewCmd.AddCommand(previewStatusCmd)
	previewCmd.AddCommand(previewLogsCmd)
	previewCmd.AddCommand(previewStopCmd)
	previewCmd.AddCommand(previewRestartCmd)
	previewCmd.AddCommand(previewGCCmd)
}

type previewStartRequest struct {
	Scope              string   `json:"scope,omitempty"`
	WorkspaceID        string   `json:"workspace_id"`
	IssueID            string   `json:"issue_id,omitempty"`
	RuntimeOwnerUserID string   `json:"runtime_owner_user_id,omitempty"`
	OwnerAgentID       string   `json:"owner_agent_id,omitempty"`
	CWD                string   `json:"cwd"`
	Command            []string `json:"command"`
	Port               int      `json:"port,omitempty"`
	URL                string   `json:"url,omitempty"`
	HealthURL          string   `json:"health_url,omitempty"`
	Restart            bool     `json:"restart,omitempty"`
}

type previewActionRequest struct {
	ID string `json:"id"`
}

type previewRecord struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	IssueID        string    `json:"issue_id,omitempty"`
	Visibility     string    `json:"visibility"`
	CWD            string    `json:"cwd"`
	Command        []string  `json:"command"`
	PID            int       `json:"pid"`
	Port           int       `json:"port,omitempty"`
	URL            string    `json:"url,omitempty"`
	HealthURL      string    `json:"health_url,omitempty"`
	LogPath        string    `json:"log_path"`
	Status         string    `json:"status"`
	StartedAt      time.Time `json:"started_at"`
	LastHealthAt   time.Time `json:"last_health_at,omitempty"`
	LastAccessedAt time.Time `json:"last_accessed_at,omitempty"`
}

type previewStartResponse struct {
	Status  string        `json:"status"`
	Preview previewRecord `json:"preview"`
}

type previewListResponse struct {
	Previews []previewRecord `json:"previews"`
}

type previewLogsResponse struct {
	ID      string `json:"id"`
	LogPath string `json:"log_path"`
	Logs    string `json:"logs"`
}

func runPreviewStart(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("preview command is required after --")
	}
	cwd, _ := cmd.Flags().GetString("cwd")
	if strings.TrimSpace(cwd) == "" {
		return fmt.Errorf("--cwd is required")
	}
	workspaceID, err := requireWorkspaceID(cmd)
	if err != nil {
		return err
	}
	port, _ := cmd.Flags().GetInt("port")
	restart, _ := cmd.Flags().GetBool("restart")
	req := previewStartRequest{
		Scope:              flagString(cmd, "scope"),
		WorkspaceID:        workspaceID,
		IssueID:            flagString(cmd, "issue"),
		RuntimeOwnerUserID: os.Getenv("MULTICA_RUNTIME_OWNER_USER_ID"),
		OwnerAgentID:       os.Getenv("MULTICA_AGENT_ID"),
		CWD:                cwd,
		Command:            append([]string(nil), args...),
		Port:               port,
		URL:                flagString(cmd, "url"),
		HealthURL:          flagString(cmd, "health-url"),
		Restart:            restart,
	}
	var resp previewStartResponse
	if err := previewDaemonJSON(cmd, http.MethodPost, "/preview/start", req, &resp); err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printPreviewStart(resp)
		return nil
	}
	return cli.PrintJSON(os.Stdout, resp)
}

func runPreviewList(cmd *cobra.Command, _ []string) error {
	var resp previewListResponse
	if err := previewDaemonJSON(cmd, http.MethodGet, "/preview/list", nil, &resp); err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	printPreviewTable(resp.Previews)
	return nil
}

func runPreviewStatus(cmd *cobra.Command, args []string) error {
	var preview previewRecord
	if err := previewDaemonJSON(cmd, http.MethodGet, "/preview/status?id="+args[0], nil, &preview); err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printPreviewTable([]previewRecord{preview})
		return nil
	}
	return cli.PrintJSON(os.Stdout, preview)
}

func runPreviewLogs(cmd *cobra.Command, args []string) error {
	lines, _ := cmd.Flags().GetInt("lines")
	var resp previewLogsResponse
	path := fmt.Sprintf("/preview/logs?id=%s&tail=%d", args[0], lines)
	if err := previewDaemonJSON(cmd, http.MethodGet, path, nil, &resp); err != nil {
		return err
	}
	if resp.Logs != "" {
		fmt.Fprint(os.Stdout, resp.Logs)
		if !strings.HasSuffix(resp.Logs, "\n") {
			fmt.Fprintln(os.Stdout)
		}
	}
	return nil
}

func runPreviewStop(cmd *cobra.Command, args []string) error {
	var preview previewRecord
	if err := previewDaemonJSON(cmd, http.MethodPost, "/preview/stop", previewActionRequest{ID: args[0]}, &preview); err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printPreviewTable([]previewRecord{preview})
		return nil
	}
	return cli.PrintJSON(os.Stdout, preview)
}

func runPreviewRestart(cmd *cobra.Command, args []string) error {
	var resp previewStartResponse
	if err := previewDaemonJSON(cmd, http.MethodPost, "/preview/restart", previewActionRequest{ID: args[0]}, &resp); err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printPreviewStart(resp)
		return nil
	}
	return cli.PrintJSON(os.Stdout, resp)
}

func runPreviewGC(cmd *cobra.Command, _ []string) error {
	var resp map[string]string
	if err := previewDaemonJSON(cmd, http.MethodPost, "/preview/gc", map[string]string{}, &resp); err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		fmt.Fprintln(os.Stdout, "Preview GC completed.")
		return nil
	}
	return cli.PrintJSON(os.Stdout, resp)
}

func previewDaemonJSON(cmd *cobra.Command, method, path string, body any, out any) error {
	port, err := resolveDaemonPortForPreview(cmd)
	if err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("preview daemon request failed: %s", strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parse daemon response: %w", err)
	}
	return nil
}

func resolveDaemonPortForPreview(cmd *cobra.Command) (int, error) {
	if raw := strings.TrimSpace(os.Getenv("MULTICA_DAEMON_PORT")); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port <= 0 {
			return 0, fmt.Errorf("invalid MULTICA_DAEMON_PORT %q", raw)
		}
		return port, nil
	}
	profile := resolveProfile(cmd)
	configPath := resolveConfigPath(cmd)
	port := healthPortForInstance(profile, configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	health := checkDaemonHealthOnPort(ctx, port)
	if health["status"] != "running" {
		return 0, fmt.Errorf("daemon is not running on 127.0.0.1:%d; run 'multica daemon start' first", port)
	}
	return port, nil
}

func printPreviewStart(resp previewStartResponse) {
	p := resp.Preview
	rows := [][]string{{
		resp.Status,
		shortPreviewID(p.ID),
		p.Status,
		emptyDash(p.URL),
		strconv.Itoa(p.PID),
		p.Visibility,
	}}
	cli.PrintTable(os.Stdout, []string{"RESULT", "ID", "STATUS", "URL", "PID", "VISIBILITY"}, rows)
}

func printPreviewTable(previews []previewRecord) {
	if len(previews) == 0 {
		fmt.Fprintln(os.Stdout, "(no previews)")
		return
	}
	rows := make([][]string, 0, len(previews))
	for _, p := range previews {
		rows = append(rows, []string{
			shortPreviewID(p.ID),
			p.Status,
			emptyDash(p.URL),
			strconv.Itoa(p.PID),
			p.Visibility,
			emptyDash(p.IssueID),
			emptyDash(p.LogPath),
		})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "STATUS", "URL", "PID", "VISIBILITY", "ISSUE", "LOG"}, rows)
}

func shortPreviewID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}
