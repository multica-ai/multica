package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Work with agents",
}

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List agents in the workspace",
	RunE:  runAgentList,
}

var agentGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get agent details",
	Args:  exactArgs(1),
	RunE:  runAgentGet,
}

var agentCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new agent",
	RunE:  runAgentCreate,
}

var agentUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update an agent",
	Args:  exactArgs(1),
	RunE:  runAgentUpdate,
}

var agentArchiveCmd = &cobra.Command{
	Use:   "archive <id>",
	Short: "Archive an agent",
	Args:  exactArgs(1),
	RunE:  runAgentArchive,
}

var agentRestoreCmd = &cobra.Command{
	Use:   "restore <id>",
	Short: "Restore an archived agent",
	Args:  exactArgs(1),
	RunE:  runAgentRestore,
}

var agentTasksCmd = &cobra.Command{
	Use:   "tasks <id>",
	Short: "List tasks for an agent",
	Args:  exactArgs(1),
	RunE:  runAgentTasks,
}

var agentAvatarCmd = &cobra.Command{
	Use:   "avatar <id>",
	Short: "Upload an avatar image for an agent",
	Args:  exactArgs(1),
	RunE:  runAgentAvatar,
}

// Agent skills subcommands.

var agentSkillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Manage agent skill assignments",
}

var agentSkillsListCmd = &cobra.Command{
	Use:   "list <agent-id>",
	Short: "List skills assigned to an agent",
	Args:  exactArgs(1),
	RunE:  runAgentSkillsList,
}

var agentSkillsSetCmd = &cobra.Command{
	Use:   "set <agent-id>",
	Short: "Set skills for an agent (replaces all current assignments)",
	Args:  exactArgs(1),
	RunE:  runAgentSkillsSet,
}

func init() {
	agentCmd.AddCommand(agentListCmd)
	agentCmd.AddCommand(agentGetCmd)
	agentCmd.AddCommand(agentCreateCmd)
	agentCmd.AddCommand(agentUpdateCmd)
	agentCmd.AddCommand(agentArchiveCmd)
	agentCmd.AddCommand(agentRestoreCmd)
	agentCmd.AddCommand(agentTasksCmd)
	agentCmd.AddCommand(agentAvatarCmd)
	agentCmd.AddCommand(agentSkillsCmd)

	agentSkillsCmd.AddCommand(agentSkillsListCmd)
	agentSkillsCmd.AddCommand(agentSkillsSetCmd)

	// agent list
	agentListCmd.Flags().String("output", "table", "Output format: table or json")
	agentListCmd.Flags().Bool("include-archived", false, "Include archived agents")

	// agent get
	agentGetCmd.Flags().String("output", "json", "Output format: table or json")

	// agent create
	agentCreateCmd.Flags().String("name", "", "Agent name (required)")
	agentCreateCmd.Flags().String("description", "", "Agent description")
	agentCreateCmd.Flags().String("instructions", "", "Agent instructions")
	agentCreateCmd.Flags().String("runtime-id", "", "Runtime ID (required)")
	agentCreateCmd.Flags().String("runtime-config", "", "Runtime config as JSON string")
	agentCreateCmd.Flags().String("model", "", "Model identifier (e.g. claude-sonnet-4-6, openai/gpt-4o). Prefer this over passing --model in --custom-args.")
	agentCreateCmd.Flags().String("custom-args", "", "Custom CLI arguments as JSON array. For model selection prefer --model; some providers (codex app-server, openclaw) reject --model in custom_args.")
	agentCreateCmd.Flags().String("custom-env", "", "Custom environment variables as JSON object, e.g. '{\"KEY\":\"value\"}'. Treated as secret material — never logged by the CLI, but values passed on the command line are visible to shell history and 'ps'; prefer --custom-env-stdin or --custom-env-file for real secrets. Pass '{}' to set an empty map.")
	agentCreateCmd.Flags().Bool("custom-env-stdin", false, "Read the --custom-env JSON object from stdin. Keeps secrets out of shell history and 'ps'. Mutually exclusive with --custom-env and --custom-env-file.")
	agentCreateCmd.Flags().String("custom-env-file", "", "Read the --custom-env JSON object from a file path (suggested mode: 0600). Mutually exclusive with --custom-env and --custom-env-stdin.")
	agentCreateCmd.Flags().String("visibility", "private", "Visibility: private or workspace")
	agentCreateCmd.Flags().Int32("max-concurrent-tasks", 6, "Maximum concurrent tasks")
	agentCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// agent update
	agentUpdateCmd.Flags().String("name", "", "New name")
	agentUpdateCmd.Flags().String("description", "", "New description")
	agentUpdateCmd.Flags().String("instructions", "", "New instructions")
	agentUpdateCmd.Flags().String("runtime-id", "", "New runtime ID")
	agentUpdateCmd.Flags().String("runtime-config", "", "New runtime config as JSON string")
	agentUpdateCmd.Flags().String("model", "", "New model identifier. Pass an empty string to clear and fall back to the runtime default.")
	agentUpdateCmd.Flags().String("custom-args", "", "New custom CLI arguments as JSON array. For model selection prefer --model; some providers (codex app-server, openclaw) reject --model in custom_args.")
	agentUpdateCmd.Flags().String("custom-env", "", "New custom environment variables as JSON object, e.g. '{\"KEY\":\"value\"}'. Treated as secret material — never logged by the CLI, but values passed on the command line are visible to shell history and 'ps'; prefer --custom-env-stdin or --custom-env-file for real secrets. Pass '{}' to clear the map; omit the flag to leave it unchanged.")
	agentUpdateCmd.Flags().Bool("custom-env-stdin", false, "Read the new --custom-env JSON object from stdin. Keeps secrets out of shell history and 'ps'. Mutually exclusive with --custom-env and --custom-env-file.")
	agentUpdateCmd.Flags().String("custom-env-file", "", "Read the new --custom-env JSON object from a file path (suggested mode: 0600). Mutually exclusive with --custom-env and --custom-env-stdin.")
	agentUpdateCmd.Flags().String("visibility", "", "New visibility: private or workspace")
	agentUpdateCmd.Flags().String("status", "", "New status")
	agentUpdateCmd.Flags().Int32("max-concurrent-tasks", 0, "New max concurrent tasks")
	agentUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// agent archive
	agentArchiveCmd.Flags().String("output", "json", "Output format: table or json")

	// agent restore
	agentRestoreCmd.Flags().String("output", "json", "Output format: table or json")

	// agent tasks
	agentTasksCmd.Flags().String("output", "table", "Output format: table or json")

	// agent avatar
	agentAvatarCmd.Flags().String("file", "", "Path to the avatar image file (required)")
	agentAvatarCmd.Flags().String("output", "json", "Output format: table or json")

	// agent skills list
	agentSkillsListCmd.Flags().String("output", "table", "Output format: table or json")

	// agent skills set
	agentSkillsSetCmd.Flags().StringSlice("skill-ids", nil, "Skill IDs to assign (comma-separated)")
	agentSkillsSetCmd.Flags().String("output", "json", "Output format: table or json")
}

// resolveProfile returns the --profile flag value (empty string means default profile).
func resolveProfile(cmd *cobra.Command) string {
	val, _ := cmd.Flags().GetString("profile")
	return val
}

func newAPIClient(cmd *cobra.Command) (*cli.APIClient, error) {
	serverURL := resolveServerURL(cmd)
	workspaceID := resolveWorkspaceID(cmd)
	token := resolveToken(cmd)

	if serverURL == "" {
		return nil, fmt.Errorf("server URL not set: use --server-url flag, MULTICA_SERVER_URL env, or 'multica config set server_url <url>'")
	}

	client := cli.NewAPIClient(serverURL, workspaceID, token)
	// When running inside a daemon task, attribute actions to the agent.
	if agentID := os.Getenv("MULTICA_AGENT_ID"); agentID != "" {
		client.AgentID = agentID
	}
	if taskID := os.Getenv("MULTICA_TASK_ID"); taskID != "" {
		client.TaskID = taskID
	}
	return client, nil
}

func resolveServerURL(cmd *cobra.Command) string {
	val := cli.FlagOrEnv(cmd, "server-url", "MULTICA_SERVER_URL", "")
	if val != "" {
		return normalizeAPIBaseURL(val)
	}
	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err == nil && cfg.ServerURL != "" {
		return normalizeAPIBaseURL(cfg.ServerURL)
	}
	fmt.Fprintln(os.Stderr, "No server configured. Run 'multica setup' first.")
	os.Exit(1)
	return "" // unreachable
}

func normalizeAPIBaseURL(raw string) string {
	normalized, err := daemon.NormalizeServerBaseURL(raw)
	if err == nil {
		return normalized
	}
	return raw
}

// inAgentExecutionContext reports whether the CLI is being invoked from
// inside a daemon-managed agent task (daemon sets MULTICA_AGENT_ID and
// MULTICA_TASK_ID in the agent env). In that context the workspace must be
// provided explicitly by the daemon — falling back to user-global
// ~/.multica/config.json would let the agent act on whatever workspace the
// user last configured, which is how cross-workspace contamination happens
// when multiple workspaces share a host.
func inAgentExecutionContext() bool {
	return os.Getenv("MULTICA_AGENT_ID") != "" || os.Getenv("MULTICA_TASK_ID") != ""
}

func resolveWorkspaceID(cmd *cobra.Command) string {
	val := cli.FlagOrEnv(cmd, "workspace-id", "MULTICA_WORKSPACE_ID", "")
	if val != "" {
		return val
	}
	// Inside an agent task the daemon is the only authority on workspace
	// identity. Never read the user-global CLI config here.
	if inAgentExecutionContext() {
		return ""
	}
	profile := resolveProfile(cmd)
	cfg, _ := cli.LoadCLIConfigForProfile(profile)
	return cfg.WorkspaceID
}

// requireWorkspaceID resolves the workspace ID and returns an error with
// actionable instructions if it is empty (e.g. user has multiple workspaces
// but no default configured).
func requireWorkspaceID(cmd *cobra.Command) (string, error) {
	id := resolveWorkspaceID(cmd)
	if id == "" {
		if inAgentExecutionContext() {
			return "", fmt.Errorf("workspace_id is required: MULTICA_WORKSPACE_ID must be set by the daemon in agent execution context (no fallback to user config)")
		}
		return "", fmt.Errorf("workspace_id is required: use --workspace-id flag, set MULTICA_WORKSPACE_ID env, or run 'multica config set workspace_id <id>'")
	}
	return id, nil
}

// ---------------------------------------------------------------------------
// Agent commands
// ---------------------------------------------------------------------------

func runAgentList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var agents []map[string]any
	params := url.Values{}
	params.Set("workspace_id", client.WorkspaceID)
	if v, _ := cmd.Flags().GetBool("include-archived"); v {
		params.Set("include_archived", "true")
	}
	path := "/api/agents"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	if err := client.GetJSON(ctx, path, &agents); err != nil {
		return fmt.Errorf("list agents: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, agents)
	}

	headers := []string{"ID", "NAME", "STATUS", "RUNTIME", "ARCHIVED"}
	rows := make([][]string, 0, len(agents))
	for _, a := range agents {
		archived := ""
		if v := strVal(a, "archived_at"); v != "" {
			archived = "yes"
		}
		rows = append(rows, []string{
			strVal(a, "id"),
			strVal(a, "name"),
			strVal(a, "status"),
			strVal(a, "runtime_mode"),
			archived,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAgentGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var agent map[string]any
	if err := client.GetJSON(ctx, "/api/agents/"+args[0], &agent); err != nil {
		return fmt.Errorf("get agent: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, agent)
	}

	headers := []string{"ID", "NAME", "STATUS", "RUNTIME", "VISIBILITY", "AVATAR_URL", "DESCRIPTION"}
	rows := [][]string{{
		strVal(agent, "id"),
		strVal(agent, "name"),
		strVal(agent, "status"),
		strVal(agent, "runtime_mode"),
		strVal(agent, "visibility"),
		strVal(agent, "avatar_url"),
		strVal(agent, "description"),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAgentCreate(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	runtimeID, _ := cmd.Flags().GetString("runtime-id")
	if runtimeID == "" {
		return fmt.Errorf("--runtime-id is required")
	}

	body := map[string]any{
		"name":       name,
		"runtime_id": runtimeID,
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if v, _ := cmd.Flags().GetString("instructions"); v != "" {
		body["instructions"] = v
	}
	if cmd.Flags().Changed("runtime-config") {
		v, _ := cmd.Flags().GetString("runtime-config")
		var rc any
		if err := json.Unmarshal([]byte(v), &rc); err != nil {
			return fmt.Errorf("--runtime-config must be valid JSON: %w", err)
		}
		body["runtime_config"] = rc
	}
	if cmd.Flags().Changed("custom-args") {
		v, _ := cmd.Flags().GetString("custom-args")
		ca, err := parseCustomArgs(v)
		if err != nil {
			return err
		}
		body["custom_args"] = ca
	}
	if ce, ok, err := resolveCustomEnv(cmd); err != nil {
		return err
	} else if ok {
		body["custom_env"] = ce
	}
	if cmd.Flags().Changed("model") {
		v, _ := cmd.Flags().GetString("model")
		body["model"] = v
	}
	if cmd.Flags().Changed("visibility") {
		v, _ := cmd.Flags().GetString("visibility")
		body["visibility"] = v
	}
	if cmd.Flags().Changed("max-concurrent-tasks") {
		v, _ := cmd.Flags().GetInt32("max-concurrent-tasks")
		body["max_concurrent_tasks"] = v
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/agents", body, &result); err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Agent created: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runAgentUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		v, _ := cmd.Flags().GetString("name")
		body["name"] = v
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	if cmd.Flags().Changed("instructions") {
		v, _ := cmd.Flags().GetString("instructions")
		body["instructions"] = v
	}
	if cmd.Flags().Changed("runtime-id") {
		v, _ := cmd.Flags().GetString("runtime-id")
		body["runtime_id"] = v
	}
	if cmd.Flags().Changed("runtime-config") {
		v, _ := cmd.Flags().GetString("runtime-config")
		var rc any
		if err := json.Unmarshal([]byte(v), &rc); err != nil {
			return fmt.Errorf("--runtime-config must be valid JSON: %w", err)
		}
		body["runtime_config"] = rc
	}
	if cmd.Flags().Changed("custom-args") {
		v, _ := cmd.Flags().GetString("custom-args")
		ca, err := parseCustomArgs(v)
		if err != nil {
			return err
		}
		body["custom_args"] = ca
	}
	if ce, ok, err := resolveCustomEnv(cmd); err != nil {
		return err
	} else if ok {
		body["custom_env"] = ce
	}
	if cmd.Flags().Changed("model") {
		v, _ := cmd.Flags().GetString("model")
		body["model"] = v
	}
	if cmd.Flags().Changed("visibility") {
		v, _ := cmd.Flags().GetString("visibility")
		body["visibility"] = v
	}
	if cmd.Flags().Changed("status") {
		v, _ := cmd.Flags().GetString("status")
		body["status"] = v
	}
	if cmd.Flags().Changed("max-concurrent-tasks") {
		v, _ := cmd.Flags().GetInt32("max-concurrent-tasks")
		body["max_concurrent_tasks"] = v
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use --name, --description, --instructions, --runtime-id, --runtime-config, --model, --custom-args, --custom-env (or --custom-env-stdin, --custom-env-file), --visibility, --status, or --max-concurrent-tasks")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/agents/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update agent: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Agent updated: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runAgentArchive(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/agents/"+args[0]+"/archive", nil, &result); err != nil {
		return fmt.Errorf("archive agent: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Agent archived: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runAgentRestore(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/agents/"+args[0]+"/restore", nil, &result); err != nil {
		return fmt.Errorf("restore agent: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Agent restored: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runAgentTasks(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var tasks []map[string]any
	if err := client.GetJSON(ctx, "/api/agents/"+args[0]+"/tasks", &tasks); err != nil {
		return fmt.Errorf("list agent tasks: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, tasks)
	}

	headers := []string{"ID", "ISSUE_ID", "STATUS", "CREATED_AT"}
	rows := make([][]string, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, []string{
			strVal(t, "id"),
			strVal(t, "issue_id"),
			strVal(t, "status"),
			strVal(t, "created_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAgentAvatar(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	filePath, _ := cmd.Flags().GetString("file")
	if filePath == "" {
		return fmt.Errorf("--file is required")
	}

	// Validate file exists.
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("file not found: %w", err)
	}

	// Validate extension.
	ext := strings.ToLower(filepath.Ext(filePath))
	validExts := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}
	if !validExts[ext] {
		return fmt.Errorf("unsupported file format %q: must be .png, .jpg, .jpeg, .gif, or .webp", ext)
	}

	// Client-side size guard: reject files > 5MB.
	const maxSize = 5 << 20 // 5 MB
	if info.Size() > maxSize {
		return fmt.Errorf("file too large: %d bytes (max 5MB)", info.Size())
	}

	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	// Defensive re-check: guard against TOCTOU race where the file
	// was swapped between stat and read.
	if len(fileData) > maxSize {
		return fmt.Errorf("file too large: %d bytes (max 5MB)", len(fileData))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Agent existence pre-check.
	var agent map[string]any
	if err := client.GetJSON(ctx, "/api/agents/"+args[0], &agent); err != nil {
		return fmt.Errorf("get agent: %w", err)
	}

	// Temporarily extend HTTP client timeout for the upload; the default
	// 15s is too short for larger files on slower connections.
	origClient := client.HTTPClient
	client.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	id, url, err := client.UploadFileWithURL(ctx, fileData, filePath)
	client.HTTPClient = origClient
	if err != nil {
		return fmt.Errorf("upload avatar: %w", err)
	}

	body := map[string]any{"avatar_url": url}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/agents/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update agent avatar: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{
			"id":         id,
			"agent_id":   args[0],
			"avatar_url": url,
		})
	}

	headers := []string{"ID", "AGENT_ID", "AVATAR_URL"}
	rows := [][]string{{
		id,
		args[0],
		url,
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// Agent skills subcommands
// ---------------------------------------------------------------------------

func runAgentSkillsList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var skills []map[string]any
	if err := client.GetJSON(ctx, "/api/agents/"+args[0]+"/skills", &skills); err != nil {
		return fmt.Errorf("list agent skills: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, skills)
	}

	headers := []string{"ID", "NAME", "DESCRIPTION"}
	rows := make([][]string, 0, len(skills))
	for _, s := range skills {
		rows = append(rows, []string{
			strVal(s, "id"),
			strVal(s, "name"),
			strVal(s, "description"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAgentSkillsSet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	if !cmd.Flags().Changed("skill-ids") {
		return fmt.Errorf("--skill-ids is required (comma-separated skill IDs; use --skill-ids '' to clear all)")
	}
	skillIDs, _ := cmd.Flags().GetStringSlice("skill-ids")
	// Allow passing empty string to clear all skills.
	cleanIDs := make([]string, 0, len(skillIDs))
	for _, id := range skillIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			cleanIDs = append(cleanIDs, id)
		}
	}

	body := map[string]any{
		"skill_ids": cleanIDs,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result json.RawMessage
	if err := client.PutJSON(ctx, "/api/agents/"+args[0]+"/skills", body, &result); err != nil {
		return fmt.Errorf("set agent skills: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		var pretty any
		json.Unmarshal(result, &pretty)
		return cli.PrintJSON(os.Stdout, pretty)
	}

	fmt.Printf("Skills updated for agent %s\n", args[0])
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseCustomEnv parses the --custom-env flag value (a JSON object literal)
// into a string map suitable for the request body. The clear-all signal is
// the explicit JSON object "{}"; empty or whitespace-only input is rejected
// because for the stdin/file channels it almost always means an upstream
// failure (missing file, unset pipe, set -o pipefail off) rather than a
// deliberate clear. Treating it as "clear" silently wipes secrets.
//
// The payload is treated as secret material: parse errors never wrap the
// underlying json error, because json.SyntaxError / UnmarshalTypeError can
// surface short fragments of the input on some malformed inputs.
func parseCustomEnv(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("--custom-env: empty input; pass '{}' to clear")
	}
	var ce map[string]string
	if err := json.Unmarshal([]byte(raw), &ce); err != nil {
		return nil, fmt.Errorf("--custom-env must be a valid JSON object of string keys and string values")
	}
	if ce == nil {
		ce = map[string]string{}
	}
	return ce, nil
}

// parseCustomArgs parses the --custom-args flag value (a JSON array of
// CLI argument strings). The error message is content-free for the same
// reason as parseCustomEnv: although custom_args is not a dedicated
// secret channel today, it routinely carries values like "--api-key=…"
// for runtime providers, and json.Unmarshal errors can echo short
// fragments of malformed input.
func parseCustomArgs(raw string) ([]string, error) {
	var ca []string
	if err := json.Unmarshal([]byte(raw), &ca); err != nil {
		return nil, fmt.Errorf("--custom-args must be a valid JSON array of strings")
	}
	return ca, nil
}

// resolveCustomEnv collects the --custom-env, --custom-env-stdin, and
// --custom-env-file flags and returns the parsed map, a bool indicating
// whether the caller supplied any of them, and any error. The three input
// channels are mutually exclusive so callers can't accidentally provide a
// secret twice. Stdin and file inputs exist to keep secret material out of
// shell history and 'ps' / /proc/<pid>/cmdline.
func resolveCustomEnv(cmd *cobra.Command) (map[string]string, bool, error) {
	inline := cmd.Flags().Changed("custom-env")
	fromStdin, _ := cmd.Flags().GetBool("custom-env-stdin")
	filePath, _ := cmd.Flags().GetString("custom-env-file")
	// Note: an explicit --custom-env-file "" is honored as "the user asked
	// for this channel with an empty path" and surfaces a real error below,
	// rather than being silently swallowed.
	fromFile := cmd.Flags().Changed("custom-env-file")

	count := 0
	if inline {
		count++
	}
	if fromStdin {
		count++
	}
	if fromFile {
		count++
	}
	switch {
	case count == 0:
		return nil, false, nil
	case count > 1:
		return nil, false, fmt.Errorf("--custom-env, --custom-env-stdin, and --custom-env-file are mutually exclusive; pick one")
	}

	var raw string
	switch {
	case inline:
		raw, _ = cmd.Flags().GetString("custom-env")
	case fromStdin:
		buf, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, false, fmt.Errorf("read --custom-env-stdin: %w", err)
		}
		raw = string(buf)
		if strings.TrimSpace(raw) == "" {
			return nil, false, fmt.Errorf("--custom-env-stdin: empty input; pass '{}' to clear")
		}
	case fromFile:
		if filePath == "" {
			return nil, false, fmt.Errorf("--custom-env-file: path must not be empty")
		}
		buf, err := os.ReadFile(filePath)
		if err != nil {
			// Filesystem errors may include the path but not the contents —
			// safe to surface via %w.
			return nil, false, fmt.Errorf("read --custom-env-file: %w", err)
		}
		raw = string(buf)
		if strings.TrimSpace(raw) == "" {
			return nil, false, fmt.Errorf("--custom-env-file %q: empty contents; pass '{}' to clear", filePath)
		}
	}

	ce, err := parseCustomEnv(raw)
	if err != nil {
		return nil, false, err
	}
	return ce, true, nil
}

func strVal(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
