package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var codexPluginCmd = &cobra.Command{
	Use:   "codex-plugin",
	Short: "Work with the Multica Codex App plugin",
}

const codexPluginDefaultSource = "codex_app_plugin"

var codexPluginSchemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Print the helper tool schema for the Multica Codex App plugin",
	RunE:  runCodexPluginSchema,
}

var codexPluginMCPCmd = &cobra.Command{
	Use:    "mcp",
	Short:  "Start the local helper MCP server for the Multica Codex App plugin",
	Hidden: true,
	RunE:   runCodexPluginMCP,
}

var codexPluginBindCmd = &cobra.Command{
	Use:   "bind <issue>",
	Short: "Bind a Codex App thread/session to a Multica issue",
	Args:  exactArgs(1),
	RunE:  runCodexPluginBind,
}

var codexPluginEventCmd = &cobra.Command{
	Use:   "event <issue>",
	Short: "Append a Codex App runtime event to a bound Multica issue",
	Args:  exactArgs(1),
	RunE:  runCodexPluginEvent,
}

var codexPluginUsageCmd = &cobra.Command{
	Use:   "usage <issue>",
	Short: "Report Codex App token usage for a bound Multica issue",
	Args:  exactArgs(1),
	RunE:  runCodexPluginUsage,
}

var codexPluginHookCmd = &cobra.Command{
	Use:    "hook",
	Short:  "Handle Codex lifecycle hook events for the Multica Codex App plugin",
	Hidden: true,
	RunE:   runCodexPluginHook,
}

type codexPluginToolSchema struct {
	Name        string            `json:"name"`
	Purpose     string            `json:"purpose"`
	Input       map[string]string `json:"input"`
	Output      map[string]string `json:"output"`
	Idempotency string            `json:"idempotency,omitempty"`
	Notes       []string          `json:"notes,omitempty"`
	Errors      []string          `json:"errors,omitempty"`
}

type codexPluginSchema struct {
	Version               string                  `json:"version"`
	Source                string                  `json:"source"`
	DefaultSource         string                  `json:"default_source"`
	ResponseEnvelope      map[string]string       `json:"response_envelope"`
	CapabilityAssumptions []string                `json:"capability_assumptions"`
	Tools                 []codexPluginToolSchema `json:"tools"`
	ErrorCodes            map[string]string       `json:"error_codes"`
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type mcpToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type mcpToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type codexPluginHookInput struct {
	SessionID            string `json:"session_id"`
	TranscriptPath       string `json:"transcript_path"`
	CWD                  string `json:"cwd"`
	HookEventName        string `json:"hook_event_name"`
	Model                string `json:"model"`
	TurnID               string `json:"turn_id"`
	PermissionMode       string `json:"permission_mode"`
	Prompt               string `json:"prompt"`
	StopHookActive       bool   `json:"stop_hook_active"`
	LastAssistantMessage string `json:"last_assistant_message"`
}

type codexPluginHookState struct {
	Bindings    []codexPluginHookBinding    `json:"bindings"`
	Prompts     []codexPluginHookPrompt     `json:"prompts"`
	SyncedTurns []codexPluginHookSyncedTurn `json:"synced_turns,omitempty"`
}

type codexPluginHookBinding struct {
	IssueID         string `json:"issue_id"`
	BindingID       string `json:"binding_id"`
	TopCommentID    string `json:"top_comment_id,omitempty"`
	ProjectFolder   string `json:"project_folder,omitempty"`
	CodexSessionID  string `json:"codex_session_id,omitempty"`
	CodexThreadID   string `json:"codex_thread_id,omitempty"`
	Source          string `json:"source,omitempty"`
	SourceKey       string `json:"source_key,omitempty"`
	LastBoundAt     string `json:"last_bound_at"`
	LastBoundUnixNS int64  `json:"last_bound_unix_ns"`
}

type codexPluginHookPrompt struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
	CWD       string `json:"cwd,omitempty"`
	Prompt    string `json:"prompt"`
	Model     string `json:"model,omitempty"`
	UpdatedAt string `json:"updated_at"`
	UnixNS    int64  `json:"unix_ns"`
}

type codexPluginHookSyncedTurn struct {
	BindingID string `json:"binding_id"`
	UserHash  string `json:"user_hash"`
	SourceKey string `json:"source_key,omitempty"`
	SyncedAt  string `json:"synced_at"`
	UnixNS    int64  `json:"unix_ns"`
}

func init() {
	codexPluginCmd.AddCommand(codexPluginSchemaCmd)
	codexPluginCmd.AddCommand(codexPluginMCPCmd)
	codexPluginCmd.AddCommand(codexPluginBindCmd)
	codexPluginCmd.AddCommand(codexPluginEventCmd)
	codexPluginCmd.AddCommand(codexPluginUsageCmd)
	codexPluginCmd.AddCommand(codexPluginHookCmd)
	codexPluginSchemaCmd.Flags().String("output", "json", "Output format: json or table")

	codexPluginBindCmd.Flags().String("codex-thread-id", "", "Codex thread ID when available")
	codexPluginBindCmd.Flags().String("codex-session-id", "", "Codex session ID when available")
	codexPluginBindCmd.Flags().String("project-folder", "", "Codex App project folder")
	codexPluginBindCmd.Flags().String("branch", "", "Current Git branch")
	codexPluginBindCmd.Flags().String("source", "codex_app_plugin", "Source name for idempotency")
	codexPluginBindCmd.Flags().String("source-key", "", "Stable idempotency key for this binding")
	codexPluginBindCmd.Flags().String("output", "json", "Output format: json")

	codexPluginEventCmd.Flags().String("binding-id", "", "Codex issue binding UUID")
	codexPluginEventCmd.Flags().String("event-type", "", "Event type: plan, progress, tool_summary, final, error, approval_waiting")
	codexPluginEventCmd.Flags().String("title", "", "Short event title")
	codexPluginEventCmd.Flags().String("content", "", "Markdown event content")
	codexPluginEventCmd.Flags().Bool("content-stdin", false, "Read event content from stdin")
	codexPluginEventCmd.Flags().String("content-file", "", "Read event content from a UTF-8 file")
	codexPluginEventCmd.Flags().String("occurred-at", "", "RFC3339 event timestamp")
	codexPluginEventCmd.Flags().String("visibility", "timeline_only", "visibility: timeline_only or issue_comment")
	codexPluginEventCmd.Flags().String("source", "codex_app_plugin", "Source name for idempotency")
	codexPluginEventCmd.Flags().String("source-key", "", "Stable idempotency key for this event")
	codexPluginEventCmd.Flags().String("output", "json", "Output format: json")

	codexPluginUsageCmd.Flags().String("binding-id", "", "Codex issue binding UUID")
	codexPluginUsageCmd.Flags().String("provider", "openai", "Model provider")
	codexPluginUsageCmd.Flags().String("model", "", "Model id")
	codexPluginUsageCmd.Flags().String("usage-mode", "cumulative", "usage mode: cumulative")
	codexPluginUsageCmd.Flags().Int64("input-tokens", 0, "Input token count")
	codexPluginUsageCmd.Flags().Int64("output-tokens", 0, "Output token count")
	codexPluginUsageCmd.Flags().Int64("total-tokens", 0, "Total token count")
	codexPluginUsageCmd.Flags().Int64("cached-input-tokens", 0, "Cached input token count")
	codexPluginUsageCmd.Flags().Int64("reasoning-tokens", 0, "Reasoning token count")
	codexPluginUsageCmd.Flags().String("source", "codex_app_plugin", "Source name for idempotency")
	codexPluginUsageCmd.Flags().String("source-key", "", "Accepted for schema compatibility; localrun usage is cumulative by binding/provider/model")
	codexPluginUsageCmd.Flags().String("output", "json", "Output format: json")
}

func runCodexPluginSchema(cmd *cobra.Command, _ []string) error {
	schema := buildCodexPluginSchema()
	output, _ := cmd.Flags().GetString("output")
	switch output {
	case "json":
		return cli.PrintJSON(cmd.OutOrStdout(), schema)
	case "table":
		rows := make([][]string, 0, len(schema.Tools))
		for _, tool := range schema.Tools {
			rows = append(rows, []string{tool.Name, tool.Purpose})
		}
		cli.PrintTable(cmd.OutOrStdout(), []string{"TOOL", "PURPOSE"}, rows)
		return nil
	default:
		return fmt.Errorf("unsupported output format %q (expected json or table)", output)
	}
}

func runCodexPluginBind(cmd *cobra.Command, args []string) error {
	client, issueRef, err := codexPluginClientAndIssue(cmd, args[0])
	if err != nil {
		return err
	}
	sourceKey, _ := cmd.Flags().GetString("source-key")
	if strings.TrimSpace(sourceKey) == "" {
		return fmt.Errorf("source-key is required")
	}
	body := map[string]any{
		"cli_name":         "codex_app",
		"work_dir":         codexPluginFlagString(cmd, "project-folder"),
		"context_dir":      codexPluginBindContextDir(cmd),
		"comments_mode":    "thread",
		"no_status_update": true,
		"source":           codexPluginFlagString(cmd, "source"),
		"source_key":       sourceKey,
	}
	var result map[string]any
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := "/api/issues/" + url.PathEscape(issueRef.ID) + "/local-runs"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("bind codex plugin session: %w", err)
	}
	if err := codexPluginPersistBinding(cmd, issueRef.ID, result, map[string]string{
		"project_folder":   codexPluginFlagString(cmd, "project-folder"),
		"codex_thread_id":  codexPluginFlagString(cmd, "codex-thread-id"),
		"codex_session_id": codexPluginFlagString(cmd, "codex-session-id"),
		"source":           codexPluginFlagString(cmd, "source"),
		"source_key":       sourceKey,
	}); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, codexPluginLocalRunBindingResponse(result))
}

func runCodexPluginEvent(cmd *cobra.Command, args []string) error {
	client, _, err := codexPluginClientAndIssue(cmd, args[0])
	if err != nil {
		return err
	}
	bindingID, _ := cmd.Flags().GetString("binding-id")
	sourceKey, _ := cmd.Flags().GetString("source-key")
	eventType, _ := cmd.Flags().GetString("event-type")
	content, _, err := resolveTextFlag(cmd, "content")
	if err != nil {
		return err
	}
	if strings.TrimSpace(bindingID) == "" {
		return fmt.Errorf("binding-id is required")
	}
	if strings.TrimSpace(sourceKey) == "" {
		return fmt.Errorf("source-key is required")
	}
	if strings.TrimSpace(eventType) == "" {
		return fmt.Errorf("event-type is required")
	}
	body := map[string]any{
		"type":       codexPluginLocalMessageType(eventType, codexPluginFlagString(cmd, "visibility")),
		"content":    content,
		"source":     codexPluginFlagString(cmd, "source"),
		"source_key": sourceKey,
		"input": map[string]any{
			"kind":        "codex_app_plugin_event",
			"event_type":  eventType,
			"title":       codexPluginFlagString(cmd, "title"),
			"occurred_at": codexPluginFlagString(cmd, "occurred-at"),
			"visibility":  codexPluginFlagString(cmd, "visibility"),
		},
	}
	var result map[string]any
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := "/api/local-runs/" + url.PathEscape(bindingID) + "/messages"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("append codex plugin event: %w", err)
	}
	return cli.PrintJSON(os.Stdout, codexPluginLocalRunEventResponse(result))
}

func runCodexPluginUsage(cmd *cobra.Command, args []string) error {
	client, _, err := codexPluginClientAndIssue(cmd, args[0])
	if err != nil {
		return err
	}
	bindingID, _ := cmd.Flags().GetString("binding-id")
	model, _ := cmd.Flags().GetString("model")
	if strings.TrimSpace(bindingID) == "" {
		return fmt.Errorf("binding-id is required")
	}
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("model is required")
	}
	usageMode, _ := cmd.Flags().GetString("usage-mode")
	if strings.TrimSpace(usageMode) != "" && strings.TrimSpace(usageMode) != "cumulative" {
		return fmt.Errorf("usage-mode must be cumulative when reusing localrun usage")
	}
	provider := codexPluginLocalUsageProvider(codexPluginFlagString(cmd, "provider"))
	body := map[string]any{
		"usage": []map[string]any{{
			"provider":           provider,
			"model":              model,
			"input_tokens":       codexPluginFlagInt64(cmd, "input-tokens"),
			"output_tokens":      codexPluginFlagInt64(cmd, "output-tokens"),
			"cache_read_tokens":  codexPluginFlagInt64(cmd, "cached-input-tokens"),
			"cache_write_tokens": int64(0),
		}},
	}
	var result map[string]any
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := "/api/local-runs/" + url.PathEscape(bindingID) + "/usage"
	if err := client.PutJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("update codex plugin usage: %w", err)
	}
	return cli.PrintJSON(os.Stdout, codexPluginLocalRunUsageResponse(bindingID, body))
}

func runCodexPluginHook(cmd *cobra.Command, _ []string) error {
	var input codexPluginHookInput
	if err := json.NewDecoder(cmd.InOrStdin()).Decode(&input); err != nil {
		return fmt.Errorf("decode codex hook input: %w", err)
	}

	switch input.HookEventName {
	case "UserPromptSubmit":
		if err := codexPluginStorePrompt(cmd, input); err != nil {
			fmt.Fprintln(os.Stderr, "multica codex plugin prompt sync:", err)
		}
		return nil
	case "Stop":
		systemMessage := ""
		if err := codexPluginSyncStopConversation(cmd, input); err != nil {
			systemMessage = "Multica sync failed: " + err.Error()
			fmt.Fprintln(os.Stderr, "multica codex plugin conversation sync:", err)
		}
		resp := map[string]any{"continue": true}
		if systemMessage != "" {
			resp["systemMessage"] = systemMessage
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
	default:
		return nil
	}
}

func codexPluginClientAndIssue(cmd *cobra.Command, issueInput string) (*cli.APIClient, resolvedID, error) {
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, resolvedID{}, err
	}
	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return nil, resolvedID{}, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	issueRef, err := resolveIssueRef(ctx, client, issueInput)
	if err != nil {
		return nil, resolvedID{}, fmt.Errorf("resolve issue: %w", err)
	}
	return client, issueRef, nil
}

func codexPluginClient(cmd *cobra.Command) (*cli.APIClient, error) {
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, err
	}
	if client.WorkspaceID == "" {
		workspaceID, err := requireWorkspaceID(cmd)
		if err != nil {
			return nil, err
		}
		client.WorkspaceID = workspaceID
	}
	return client, nil
}

func codexPluginFlagString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

func codexPluginFlagInt64(cmd *cobra.Command, name string) int64 {
	v, _ := cmd.Flags().GetInt64(name)
	return v
}

func codexPluginBindContextDir(cmd *cobra.Command) string {
	items := []string{}
	if threadID := codexPluginFlagString(cmd, "codex-thread-id"); threadID != "" {
		items = append(items, "codex_thread_id="+threadID)
	}
	if sessionID := codexPluginFlagString(cmd, "codex-session-id"); sessionID != "" {
		items = append(items, "codex_session_id="+sessionID)
	}
	if branch := codexPluginFlagString(cmd, "branch"); branch != "" {
		items = append(items, "branch="+branch)
	}
	return strings.Join(items, "\n")
}

func codexPluginMCPBindContextDir(args map[string]any) string {
	items := []string{}
	if threadID := mcpString(args, "codex_thread_id"); threadID != "" {
		items = append(items, "codex_thread_id="+threadID)
	}
	if sessionID := mcpString(args, "codex_session_id"); sessionID != "" {
		items = append(items, "codex_session_id="+sessionID)
	}
	if branch := mcpString(args, "branch"); branch != "" {
		items = append(items, "branch="+branch)
	}
	return strings.Join(items, "\n")
}

func codexPluginLocalMessageType(eventType, visibility string) string {
	if eventType == "final" && visibility == "issue_comment" {
		return "final"
	}
	if eventType == "error" {
		return "error"
	}
	return "text"
}

func codexPluginLocalUsageProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "", "openai", "codex":
		return "codex"
	case "anthropic", "claude":
		return "claude"
	default:
		return provider
	}
}

func codexPluginLocalRunBindingResponse(run map[string]any) map[string]any {
	runID := strVal(run, "id")
	return map[string]any{
		"binding_id":      runID,
		"local_run_id":    runID,
		"issue_id":        strVal(run, "issue_id"),
		"status":          strVal(run, "status"),
		"top_comment_id":  strVal(run, "top_comment_id"),
		"already_existed": false,
		"run":             run,
	}
}

func codexPluginLocalRunEventResponse(msg map[string]any) map[string]any {
	bindingID := strVal(msg, "task_id")
	seq := intFromAny(msg["seq"])
	eventID := strVal(msg, "id")
	if eventID == "" && bindingID != "" && seq > 0 {
		eventID = fmt.Sprintf("%s:%d", bindingID, seq)
	}
	return map[string]any{
		"event_id":     eventID,
		"message_id":   eventID,
		"binding_id":   bindingID,
		"local_run_id": bindingID,
		"comment_id":   "",
		"deduped":      false,
		"message":      msg,
	}
}

func codexPluginConversationSyncResponse(userMsg, botMsg map[string]any, userContent, botContent string) map[string]any {
	userEvent := codexPluginLocalRunEventResponse(userMsg)
	botEvent := codexPluginLocalRunEventResponse(botMsg)
	return map[string]any{
		"event_id":     botEvent["event_id"],
		"message_id":   botEvent["message_id"],
		"binding_id":   firstNonEmpty(strVal(botMsg, "task_id"), strVal(userMsg, "task_id")),
		"local_run_id": firstNonEmpty(strVal(botMsg, "task_id"), strVal(userMsg, "task_id")),
		"comment_id":   botEvent["comment_id"],
		"deduped":      userEvent["deduped"] == true && botEvent["deduped"] == true,
		"user_event":   userEvent,
		"bot_event":    botEvent,
		"user_content": userContent,
		"bot_content":  botContent,
		"content":      userContent + "\n" + botContent,
	}
}

func codexPluginLocalRunUsageResponse(bindingID string, body map[string]any) map[string]any {
	sessionTotal := map[string]any{
		"input_tokens":        int64(0),
		"output_tokens":       int64(0),
		"total_tokens":        int64(0),
		"cached_input_tokens": int64(0),
		"reasoning_tokens":    int64(0),
	}
	if rows, ok := body["usage"].([]map[string]any); ok {
		for _, row := range rows {
			input := anyInt64(row["input_tokens"])
			output := anyInt64(row["output_tokens"])
			cacheRead := anyInt64(row["cache_read_tokens"])
			sessionTotal["input_tokens"] = input
			sessionTotal["output_tokens"] = output
			sessionTotal["total_tokens"] = input + output
			sessionTotal["cached_input_tokens"] = cacheRead
		}
	}
	return map[string]any{
		"usage_id":      "",
		"binding_id":    bindingID,
		"local_run_id":  bindingID,
		"session_total": sessionTotal,
		"deduped":       false,
	}
}

func anyInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func buildCodexPluginSchema() codexPluginSchema {
	return codexPluginSchema{
		Version:       "0.1.0",
		Source:        "OPE-1493",
		DefaultSource: "codex_app_plugin",
		ResponseEnvelope: map[string]string{
			"ok":         "boolean success flag",
			"data":       "tool-specific result object when ok is true",
			"error":      "structured error object when ok is false",
			"request_id": "server or helper request id for support and audit",
		},
		CapabilityAssumptions: []string{
			"Codex App owns thread, workdir, worktree, terminal, and Git UX.",
			"The Multica plugin/helper owns issue lookup, issue binding, runtime event sync, final conversation comments, attachments, and token usage sync.",
			"Host lifecycle hooks and full token usage are optional capabilities; the POC must degrade when they are unavailable.",
			"The plugin must reuse the local Multica login state and must not store Multica tokens in plugin files.",
			"Codex App bindings use localrun comment threads so visible conversation context stays under the issue.",
		},
		Tools: []codexPluginToolSchema{
			{
				Name:    "issue_search",
				Purpose: "Search issues the current Multica user can access.",
				Input: map[string]string{
					"workspace_id": "optional workspace UUID",
					"project_id":   "optional project UUID",
					"query":        "identifier, UUID, URL, or title keyword",
					"assignee":     "optional assignee filter such as me or a user UUID",
					"status":       "optional array of issue statuses",
					"limit":        "maximum result count, default 20",
					"cursor":       "optional pagination cursor",
				},
				Output: map[string]string{
					"items":       "array of issue summary objects",
					"next_cursor": "pagination cursor or null",
				},
				Errors: []string{"UNAUTHORIZED", "FORBIDDEN", "NETWORK_ERROR"},
			},
			{
				Name:    "issue_get",
				Purpose: "Resolve and load one issue before binding or syncing.",
				Input: map[string]string{
					"issue_ref": "issue identifier, UUID, or Multica issue URL",
				},
				Output: map[string]string{
					"issue": "issue details, summary, status, assignee, project, and metadata",
				},
				Errors: []string{"UNAUTHORIZED", "FORBIDDEN", "ISSUE_NOT_FOUND", "NETWORK_ERROR"},
			},
			{
				Name:    "session_bind",
				Purpose: "Bind the current Codex work context to a Multica issue.",
				Input: map[string]string{
					"issue_id":         "Multica issue UUID",
					"codex_thread_id":  "Codex thread id when available",
					"codex_session_id": "Codex session id when available",
					"project_folder":   "local project folder when available",
					"branch":           "current branch when available",
					"workspace_id":     "workspace UUID when available",
					"source":           "source name, default codex_app_plugin",
					"source_key":       "stable key for idempotent binding",
				},
				Output: map[string]string{
					"binding_id":      "binding UUID",
					"issue_id":        "bound issue UUID",
					"status":          "binding status",
					"top_comment_id":  "issue comment thread root created by localrun thread mode",
					"already_existed": "true when an idempotent retry reused an existing binding",
				},
				Idempotency: "source and source_key map to an existing local run binding when supported by the backend.",
				Notes:       []string{"The Codex App plugin binds with localrun comments_mode=thread so Codex App context is preserved in an issue comment thread."},
				Errors:      []string{"UNAUTHORIZED", "FORBIDDEN", "ISSUE_NOT_FOUND", "BINDING_CONFLICT", "NETWORK_ERROR"},
			},
			{
				Name:    "runtime_event_append",
				Purpose: "Append a structured plan, progress, tool summary, final conversation transcript, approval, or error event.",
				Input: map[string]string{
					"binding_id":  "binding UUID",
					"issue_id":    "Multica issue UUID",
					"event_type":  "plan, progress, tool_summary, final, error, or approval_waiting",
					"title":       "optional short event title",
					"content":     "markdown event body",
					"occurred_at": "RFC3339 timestamp",
					"source":      "source name, default codex_app_plugin",
					"source_key":  "stable key for idempotent event write",
					"visibility":  "issue_comment or timeline_only",
				},
				Output: map[string]string{
					"event_id":   "runtime event UUID",
					"comment_id": "comment UUID when visible as a reply in the localrun issue thread",
					"deduped":    "true when an idempotent retry returned an existing event",
				},
				Idempotency: "source and source_key identify one local run message.",
				Notes:       []string{"Use visibility=timeline_only for process events.", "For normal Codex App conversation turns, prefer conversation_sync so user prompts and bot replies are split into separate issue thread comments."},
				Errors:      []string{"UNAUTHORIZED", "FORBIDDEN", "ISSUE_NOT_FOUND", "DUPLICATE_SOURCE_KEY", "NETWORK_ERROR"},
			},
			{
				Name:    "comment_add",
				Purpose: "Add a direct issue comment when the workflow needs ordinary comment behavior.",
				Input: map[string]string{
					"issue_id":   "Multica issue UUID",
					"content":    "markdown comment body",
					"parent_id":  "optional parent comment UUID",
					"source":     "source name, default codex_app_plugin",
					"source_key": "optional stable idempotency key",
				},
				Output: map[string]string{
					"comment_id": "comment UUID",
					"deduped":    "true when an idempotent retry returned an existing comment",
				},
				Idempotency: "No direct comment idempotency in the reuse-first backend path; use runtime_event_append with event_type=final for idempotent final comments.",
				Errors:      []string{"UNAUTHORIZED", "FORBIDDEN", "ISSUE_NOT_FOUND", "DUPLICATE_SOURCE_KEY", "NETWORK_ERROR"},
			},
			{
				Name:    "conversation_sync",
				Purpose: "Write one visible Codex conversation turn to the bound issue comment thread.",
				Input: map[string]string{
					"binding_id":   "binding UUID",
					"issue_id":     "Multica issue UUID",
					"user_message": "user prompt or request text to include after 用户：",
					"bot_message":  "assistant result text to include after bot：",
					"occurred_at":  "RFC3339 timestamp",
					"source":       "source name, default codex_app_plugin",
					"source_key":   "stable key for idempotent final conversation write",
				},
				Output: map[string]string{
					"user_event":   "localrun user_input event for the user comment",
					"bot_event":    "localrun final event for the assistant reply comment",
					"user_content": "user comment body that was sent",
					"bot_content":  "assistant reply comment body that was sent",
					"deduped":      "true when both idempotent writes returned existing events",
				},
				Idempotency: "source and source_key identify one conversation turn. The helper derives separate user_input and final source keys from it.",
				Notes:       []string{"This is the preferred visible comment path for Codex App plugin sessions.", "It stores the user prompt as a localrun user_input message and the assistant reply as a localrun final message, so existing thread reply mirroring and message idempotency are reused."},
				Errors:      []string{"UNAUTHORIZED", "FORBIDDEN", "ISSUE_NOT_FOUND", "DUPLICATE_SOURCE_KEY", "NETWORK_ERROR"},
			},
			{
				Name:    "usage_update",
				Purpose: "Report token usage for the bound Codex session.",
				Input: map[string]string{
					"binding_id":          "binding UUID",
					"issue_id":            "Multica issue UUID",
					"provider":            "model provider, for example openai",
					"model":               "model id",
					"usage_mode":          "cumulative; delta is not accepted in the localrun reuse path",
					"input_tokens":        "input token count",
					"output_tokens":       "output token count",
					"total_tokens":        "total token count",
					"cached_input_tokens": "cached input token count when available",
					"reasoning_tokens":    "reasoning token count when available",
					"source":              "accepted for schema compatibility; not sent to the existing localrun usage endpoint",
					"source_key":          "accepted for schema compatibility; not sent to the existing localrun usage endpoint",
				},
				Output: map[string]string{
					"usage_id":      "usage record UUID",
					"session_total": "current aggregate input, output, and total token counts",
					"deduped":       "true when an idempotent retry returned an existing usage record",
				},
				Idempotency: "Existing localrun usage stores cumulative snapshots by binding/provider/model; callers must send cumulative totals to avoid double counting.",
				Notes:       []string{"When usage is unavailable, callers must omit the write or mark the value as partial in a runtime event.", "The first reuse-first implementation intentionally does not add a separate usage event ledger."},
				Errors:      []string{"UNAUTHORIZED", "FORBIDDEN", "ISSUE_NOT_FOUND", "DUPLICATE_SOURCE_KEY", "PLUGIN_CAPABILITY_LIMITED", "NETWORK_ERROR"},
			},
			{
				Name:    "attachment_upload",
				Purpose: "Upload screenshots, logs, diffs, or generated artifacts to the bound issue.",
				Input: map[string]string{
					"issue_id":     "Multica issue UUID",
					"binding_id":   "optional binding UUID",
					"file_path":    "local file path",
					"display_name": "optional display filename",
					"source":       "source name, default codex_app_plugin",
					"source_key":   "optional stable idempotency key",
				},
				Output: map[string]string{
					"attachment_id": "attachment UUID",
					"filename":      "stored filename",
					"size_bytes":    "file size",
					"url":           "issue-scoped attachment URL when available",
				},
				Idempotency: "source and source_key identify one upload when provided.",
				Errors:      []string{"UNAUTHORIZED", "FORBIDDEN", "ISSUE_NOT_FOUND", "ATTACHMENT_TOO_LARGE", "NETWORK_ERROR"},
			},
		},
		ErrorCodes: map[string]string{
			"UNAUTHORIZED":              "Local Multica login is missing or expired.",
			"FORBIDDEN":                 "The user cannot access the requested workspace, project, issue, binding, or attachment.",
			"ISSUE_NOT_FOUND":           "The issue reference could not be resolved.",
			"BINDING_CONFLICT":          "The current Codex context is already bound to a different issue.",
			"NETWORK_ERROR":             "The helper could not reach the Multica server.",
			"DUPLICATE_SOURCE_KEY":      "The source/source_key pair already exists and the existing result was returned.",
			"PLUGIN_CAPABILITY_LIMITED": "The Codex plugin host did not expose the requested lifecycle or usage data.",
			"ATTACHMENT_TOO_LARGE":      "The selected attachment exceeds the upload limit.",
		},
	}
}

func runCodexPluginMCP(cmd *cobra.Command, _ []string) error {
	return runCodexPluginMCPServer(cmd, os.Stdin, os.Stdout)
}

func runCodexPluginMCPServer(cmd *cobra.Command, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	encoder := json.NewEncoder(out)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req mcpRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			if err := encoder.Encode(mcpResponse{
				JSONRPC: "2.0",
				Error:   &mcpError{Code: -32700, Message: "parse error"},
			}); err != nil {
				return err
			}
			continue
		}
		if len(req.ID) == 0 {
			if err := handleCodexPluginMCPNotification(cmd, req); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			continue
		}

		resp := mcpResponse{JSONRPC: "2.0", ID: req.ID}
		result, rpcErr := handleCodexPluginMCPRequest(cmd, req)
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = result
		}
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func handleCodexPluginMCPNotification(_ *cobra.Command, _ mcpRequest) error {
	return nil
}

func handleCodexPluginMCPRequest(cmd *cobra.Command, req mcpRequest) (any, *mcpError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "multica-codex-plugin",
				"version": version,
			},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": buildCodexPluginMCPTools()}, nil
	case "tools/call":
		var params mcpToolCallParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return nil, &mcpError{Code: -32602, Message: "invalid tools/call params"}
			}
		}
		return callCodexPluginMCPTool(cmd, params.Name, params.Arguments)
	default:
		return nil, &mcpError{Code: -32601, Message: "method not found"}
	}
}

func callCodexPluginMCPTool(cmd *cobra.Command, name string, args map[string]any) (any, *mcpError) {
	data, err := codexPluginMCPToolData(cmd, name, args)
	if err != nil {
		return nil, codexPluginMCPError(err)
	}
	payload := map[string]any{
		"ok":         true,
		"data":       data,
		"error":      nil,
		"request_id": "",
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, codexPluginMCPError(err)
	}
	return map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": string(encoded),
			},
		},
		"isError": false,
	}, nil
}

func codexPluginMCPToolData(cmd *cobra.Command, name string, args map[string]any) (any, error) {
	switch strings.TrimSpace(name) {
	case "issue_search":
		return codexPluginMCPToolIssueSearch(cmd, args)
	case "issue_get":
		return codexPluginMCPToolIssueGet(cmd, args)
	case "session_bind":
		return codexPluginMCPToolSessionBind(cmd, args)
	case "runtime_event_append":
		return codexPluginMCPToolRuntimeEventAppend(cmd, args)
	case "conversation_sync":
		return codexPluginMCPToolConversationSync(cmd, args)
	case "comment_add":
		return codexPluginMCPToolCommentAdd(cmd, args)
	case "usage_update":
		return codexPluginMCPToolUsageUpdate(cmd, args)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func codexPluginMCPToolIssueSearch(cmd *cobra.Command, args map[string]any) (any, error) {
	client, err := codexPluginClient(cmd)
	if err != nil {
		return nil, err
	}
	params := url.Values{}
	params.Set("workspace_id", client.WorkspaceID)
	if query := mcpString(args, "query"); query != "" {
		params.Set("q", query)
	}
	if projectID := mcpString(args, "project_id"); projectID != "" {
		params.Set("project_id", projectID)
	}
	if status := mcpString(args, "status"); status != "" {
		params.Set("status", status)
	}
	if statusList := mcpStringSlice(args, "status"); len(statusList) > 0 {
		params.Set("status", statusList[0])
	}
	if limit := mcpInt(args, "limit"); limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	} else {
		params.Set("limit", "20")
	}
	if offset := mcpCursorOffset(args); offset > 0 {
		params.Set("offset", strconv.Itoa(offset))
	}
	if assignee := mcpString(args, "assignee"); assignee != "" && assignee != "me" {
		params.Set("assignee_id", assignee)
	}

	path := "/api/issues"
	if params.Get("q") != "" {
		path = "/api/issues/search"
	}
	path += "?" + params.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return nil, fmt.Errorf("issue_search: %w", err)
	}
	return codexPluginMCPPage(result, params.Get("limit"), params.Get("offset")), nil
}

func codexPluginMCPToolIssueGet(cmd *cobra.Command, args map[string]any) (any, error) {
	issueRef := firstNonEmpty(mcpString(args, "issue_ref"), mcpString(args, "issue_id"))
	if issueRef == "" {
		return nil, fmt.Errorf("issue_ref is required")
	}
	client, resolved, err := codexPluginClientAndIssue(cmd, issueRef)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var issue map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(resolved.ID), &issue); err != nil {
		return nil, fmt.Errorf("issue_get: %w", err)
	}
	return map[string]any{"issue": issue}, nil
}

func codexPluginMCPToolSessionBind(cmd *cobra.Command, args map[string]any) (any, error) {
	issueID := mcpString(args, "issue_id")
	if issueID == "" {
		return nil, fmt.Errorf("issue_id is required")
	}
	client, issueRef, err := codexPluginClientAndIssue(cmd, issueID)
	if err != nil {
		return nil, err
	}
	sourceKey := mcpString(args, "source_key")
	if sourceKey == "" {
		sourceKey = defaultCodexPluginSourceKey(args, "bind")
	}
	if sourceKey == "" {
		return nil, fmt.Errorf("source_key is required")
	}
	body := map[string]any{
		"cli_name":         "codex_app",
		"work_dir":         mcpString(args, "project_folder"),
		"context_dir":      codexPluginMCPBindContextDir(args),
		"comments_mode":    "thread",
		"no_status_update": true,
		"source":           firstNonEmpty(mcpString(args, "source"), codexPluginDefaultSource),
		"source_key":       sourceKey,
	}
	var result map[string]any
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := "/api/issues/" + url.PathEscape(issueRef.ID) + "/local-runs"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return nil, fmt.Errorf("session_bind: %w", err)
	}
	if err := codexPluginPersistBinding(cmd, issueRef.ID, result, map[string]string{
		"project_folder":   mcpString(args, "project_folder"),
		"codex_thread_id":  mcpString(args, "codex_thread_id"),
		"codex_session_id": mcpString(args, "codex_session_id"),
		"source":           firstNonEmpty(mcpString(args, "source"), codexPluginDefaultSource),
		"source_key":       sourceKey,
	}); err != nil {
		return nil, err
	}
	return codexPluginLocalRunBindingResponse(result), nil
}

func codexPluginMCPToolRuntimeEventAppend(cmd *cobra.Command, args map[string]any) (any, error) {
	issueID := mcpString(args, "issue_id")
	if issueID == "" {
		return nil, fmt.Errorf("issue_id is required")
	}
	client, issueRef, err := codexPluginClientAndIssue(cmd, issueID)
	if err != nil {
		return nil, err
	}
	sourceKey := mcpString(args, "source_key")
	if sourceKey == "" {
		sourceKey = defaultCodexPluginSourceKey(args, "event")
	}
	if sourceKey == "" {
		return nil, fmt.Errorf("source_key is required")
	}
	eventType := mcpString(args, "event_type")
	if eventType == "" {
		return nil, fmt.Errorf("event_type is required")
	}
	body := map[string]any{
		"type":       codexPluginLocalMessageType(eventType, firstNonEmpty(mcpString(args, "visibility"), "timeline_only")),
		"content":    mcpString(args, "content"),
		"source":     firstNonEmpty(mcpString(args, "source"), codexPluginDefaultSource),
		"source_key": sourceKey,
		"input": map[string]any{
			"kind":        "codex_app_plugin_event",
			"event_type":  eventType,
			"title":       mcpString(args, "title"),
			"occurred_at": mcpString(args, "occurred_at"),
			"visibility":  firstNonEmpty(mcpString(args, "visibility"), "timeline_only"),
		},
	}
	bindingID := mcpString(args, "binding_id")
	if bindingID == "" {
		return nil, fmt.Errorf("binding_id is required")
	}
	var result map[string]any
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = issueRef
	path := "/api/local-runs/" + url.PathEscape(bindingID) + "/messages"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return nil, fmt.Errorf("runtime_event_append: %w", err)
	}
	return codexPluginLocalRunEventResponse(result), nil
}

func codexPluginMCPToolConversationSync(cmd *cobra.Command, args map[string]any) (any, error) {
	issueID := mcpString(args, "issue_id")
	if issueID == "" {
		return nil, fmt.Errorf("issue_id is required")
	}
	client, issueRef, err := codexPluginClientAndIssue(cmd, issueID)
	if err != nil {
		return nil, err
	}
	bindingID := mcpString(args, "binding_id")
	if bindingID == "" {
		return nil, fmt.Errorf("binding_id is required")
	}
	userMessage := mcpString(args, "user_message")
	if userMessage == "" {
		return nil, fmt.Errorf("user_message is required")
	}
	botMessage := mcpString(args, "bot_message")
	if botMessage == "" {
		return nil, fmt.Errorf("bot_message is required")
	}
	sourceKey := mcpString(args, "source_key")
	if sourceKey == "" {
		sourceKey = defaultCodexPluginSourceKey(args, "conversation")
	}
	if sourceKey == "" {
		return nil, fmt.Errorf("source_key is required")
	}
	userLabel := firstNonEmpty(mcpString(args, "user_label"), "用户")
	botLabel := firstNonEmpty(mcpString(args, "bot_label"), "bot")
	source := firstNonEmpty(mcpString(args, "source"), codexPluginDefaultSource)
	occurredAt := mcpString(args, "occurred_at")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = issueRef
	path := "/api/local-runs/" + url.PathEscape(bindingID) + "/messages"

	userContent := formatCodexConversationPart(userLabel, userMessage)
	userBody := map[string]any{
		"type":       "user_input",
		"content":    userContent,
		"source":     source,
		"source_key": sourceKey + ":user",
		"input": map[string]any{
			"kind":        "codex_app_plugin_conversation_user",
			"event_type":  "user_input",
			"occurred_at": occurredAt,
			"visibility":  "issue_comment",
			"user_label":  userLabel,
		},
	}
	var userResult map[string]any
	if err := client.PostJSON(ctx, path, userBody, &userResult); err != nil {
		return nil, fmt.Errorf("conversation_sync: %w", err)
	}

	botContent := formatCodexConversationPart(botLabel, botMessage)
	botBody := map[string]any{
		"type":       "final",
		"content":    botContent,
		"source":     source,
		"source_key": sourceKey + ":bot",
		"input": map[string]any{
			"kind":        "codex_app_plugin_conversation_bot",
			"event_type":  "final",
			"occurred_at": occurredAt,
			"visibility":  "issue_comment",
			"bot_label":   botLabel,
		},
	}
	var botResult map[string]any
	if err := client.PostJSON(ctx, path, botBody, &botResult); err != nil {
		return nil, fmt.Errorf("conversation_sync: %w", err)
	}
	if err := codexPluginMarkConversationSynced(cmd, bindingID, userMessage, sourceKey); err != nil {
		return nil, fmt.Errorf("conversation_sync: %w", err)
	}
	return codexPluginConversationSyncResponse(userResult, botResult, userContent, botContent), nil
}

func codexPluginMCPToolCommentAdd(cmd *cobra.Command, args map[string]any) (any, error) {
	issueID := mcpString(args, "issue_id")
	if issueID == "" {
		return nil, fmt.Errorf("issue_id is required")
	}
	content := mcpString(args, "content")
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	client, issueRef, err := codexPluginClientAndIssue(cmd, issueID)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"content": content,
	}
	if parentID := mcpString(args, "parent_id"); parentID != "" {
		body["parent_id"] = parentID
	}
	var result map[string]any
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.PostJSON(ctx, "/api/issues/"+url.PathEscape(issueRef.ID)+"/comments", body, &result); err != nil {
		return nil, fmt.Errorf("comment_add: %w", err)
	}
	return map[string]any{
		"comment_id": strVal(result, "id"),
		"deduped":    false,
		"comment":    result,
	}, nil
}

func codexPluginMCPToolUsageUpdate(cmd *cobra.Command, args map[string]any) (any, error) {
	issueID := mcpString(args, "issue_id")
	if issueID == "" {
		return nil, fmt.Errorf("issue_id is required")
	}
	client, _, err := codexPluginClientAndIssue(cmd, issueID)
	if err != nil {
		return nil, err
	}
	usageMode := firstNonEmpty(mcpString(args, "usage_mode"), "cumulative")
	if usageMode != "cumulative" {
		return nil, fmt.Errorf("usage_mode must be cumulative when reusing localrun usage")
	}
	bindingID := mcpString(args, "binding_id")
	body := map[string]any{
		"usage": []map[string]any{{
			"provider":           codexPluginLocalUsageProvider(firstNonEmpty(mcpString(args, "provider"), "openai")),
			"model":              firstNonEmpty(mcpString(args, "model"), "unknown"),
			"input_tokens":       mcpInt64(args, "input_tokens"),
			"output_tokens":      mcpInt64(args, "output_tokens"),
			"cache_read_tokens":  mcpInt64(args, "cached_input_tokens"),
			"cache_write_tokens": int64(0),
		}},
	}
	if bindingID == "" {
		return nil, fmt.Errorf("binding_id is required")
	}
	var result map[string]any
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := "/api/local-runs/" + url.PathEscape(bindingID) + "/usage"
	if err := client.PutJSON(ctx, path, body, &result); err != nil {
		return nil, fmt.Errorf("usage_update: %w", err)
	}
	return codexPluginLocalRunUsageResponse(bindingID, body), nil
}

func codexPluginStatePath(cmd *cobra.Command) (string, error) {
	profile, _ := cmd.Flags().GetString("profile")
	configPath, _ := cmd.Flags().GetString("config")
	dir, err := cli.StateDirForInstance(profile, configPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "codex-plugin", "state.json"), nil
}

func codexPluginReadHookState(cmd *cobra.Command) (codexPluginHookState, error) {
	path, err := codexPluginStatePath(cmd)
	if err != nil {
		return codexPluginHookState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return codexPluginHookState{}, nil
		}
		return codexPluginHookState{}, fmt.Errorf("read hook state: %w", err)
	}
	var state codexPluginHookState
	if err := json.Unmarshal(data, &state); err != nil {
		return codexPluginHookState{}, fmt.Errorf("parse hook state: %w", err)
	}
	return state, nil
}

func codexPluginWriteHookState(cmd *cobra.Command, state codexPluginHookState) error {
	path, err := codexPluginStatePath(cmd)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create hook state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal hook state: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func codexPluginPersistBinding(cmd *cobra.Command, issueID string, run map[string]any, meta map[string]string) error {
	bindingID := strVal(run, "id")
	if bindingID == "" {
		return fmt.Errorf("session_bind response missing binding id")
	}
	now := time.Now()
	binding := codexPluginHookBinding{
		IssueID:         firstNonEmpty(issueID, strVal(run, "issue_id")),
		BindingID:       bindingID,
		TopCommentID:    strVal(run, "top_comment_id"),
		ProjectFolder:   strings.TrimSpace(meta["project_folder"]),
		CodexSessionID:  strings.TrimSpace(meta["codex_session_id"]),
		CodexThreadID:   strings.TrimSpace(meta["codex_thread_id"]),
		Source:          firstNonEmpty(meta["source"], codexPluginDefaultSource),
		SourceKey:       strings.TrimSpace(meta["source_key"]),
		LastBoundAt:     now.UTC().Format(time.RFC3339Nano),
		LastBoundUnixNS: now.UnixNano(),
	}
	if binding.IssueID == "" {
		return fmt.Errorf("session_bind response missing issue id")
	}
	state, err := codexPluginReadHookState(cmd)
	if err != nil {
		return err
	}
	state.Bindings = upsertCodexPluginBinding(state.Bindings, binding)
	state.Bindings = trimCodexPluginBindings(state.Bindings, 50)
	return codexPluginWriteHookState(cmd, state)
}

func upsertCodexPluginBinding(bindings []codexPluginHookBinding, binding codexPluginHookBinding) []codexPluginHookBinding {
	out := make([]codexPluginHookBinding, 0, len(bindings)+1)
	for _, existing := range bindings {
		if existing.BindingID == binding.BindingID {
			continue
		}
		out = append(out, existing)
	}
	return append([]codexPluginHookBinding{binding}, out...)
}

func trimCodexPluginBindings(bindings []codexPluginHookBinding, max int) []codexPluginHookBinding {
	if len(bindings) <= max {
		return bindings
	}
	return bindings[:max]
}

func codexPluginStorePrompt(cmd *cobra.Command, input codexPluginHookInput) error {
	if strings.TrimSpace(input.Prompt) == "" {
		return nil
	}
	state, err := codexPluginReadHookState(cmd)
	if err != nil {
		return err
	}
	now := time.Now()
	prompt := codexPluginHookPrompt{
		SessionID: strings.TrimSpace(input.SessionID),
		TurnID:    strings.TrimSpace(input.TurnID),
		CWD:       strings.TrimSpace(input.CWD),
		Prompt:    input.Prompt,
		Model:     strings.TrimSpace(input.Model),
		UpdatedAt: now.UTC().Format(time.RFC3339Nano),
		UnixNS:    now.UnixNano(),
	}
	state.Prompts = upsertCodexPluginPrompt(state.Prompts, prompt)
	state.Prompts = trimCodexPluginPrompts(state.Prompts, 200)
	return codexPluginWriteHookState(cmd, state)
}

func upsertCodexPluginPrompt(prompts []codexPluginHookPrompt, prompt codexPluginHookPrompt) []codexPluginHookPrompt {
	out := make([]codexPluginHookPrompt, 0, len(prompts)+1)
	for _, existing := range prompts {
		if prompt.SessionID != "" && prompt.TurnID != "" && existing.SessionID == prompt.SessionID && existing.TurnID == prompt.TurnID {
			continue
		}
		out = append(out, existing)
	}
	return append([]codexPluginHookPrompt{prompt}, out...)
}

func trimCodexPluginPrompts(prompts []codexPluginHookPrompt, max int) []codexPluginHookPrompt {
	if len(prompts) <= max {
		return prompts
	}
	return prompts[:max]
}

func codexPluginSyncStopConversation(cmd *cobra.Command, input codexPluginHookInput) error {
	if strings.TrimSpace(input.LastAssistantMessage) == "" || input.StopHookActive {
		return nil
	}
	state, err := codexPluginReadHookState(cmd)
	if err != nil {
		return err
	}
	prompt, ok := findCodexPluginPrompt(state.Prompts, input)
	if !ok || strings.TrimSpace(prompt.Prompt) == "" {
		return nil
	}
	binding, ok := findCodexPluginBinding(state.Bindings, input, prompt)
	if !ok {
		return nil
	}
	if codexPluginPromptAlreadySynced(state.SyncedTurns, binding.BindingID, prompt.Prompt) {
		return nil
	}
	client, _, err := codexPluginClientAndIssue(cmd, binding.IssueID)
	if err != nil {
		return err
	}
	sourceKey := codexPluginHookConversationSourceKey(input, prompt, binding)
	source := firstNonEmpty(binding.Source, codexPluginDefaultSource)
	occurredAt := time.Now().UTC().Format(time.RFC3339Nano)
	turnID := firstNonEmpty(input.TurnID, prompt.TurnID)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := "/api/local-runs/" + url.PathEscape(binding.BindingID) + "/messages"

	userBody := map[string]any{
		"type":       "user_input",
		"content":    formatCodexConversationPart("用户", prompt.Prompt),
		"source":     source,
		"source_key": sourceKey + ":user",
		"input": map[string]any{
			"kind":        "codex_app_plugin_hook_conversation_user",
			"event_type":  "user_input",
			"occurred_at": occurredAt,
			"visibility":  "issue_comment",
			"user_label":  "用户",
			"session_id":  input.SessionID,
			"turn_id":     turnID,
			"model":       input.Model,
		},
	}
	var userResult map[string]any
	if err := client.PostJSON(ctx, path, userBody, &userResult); err != nil {
		return err
	}

	botBody := map[string]any{
		"type":       "final",
		"content":    formatCodexConversationPart("bot", input.LastAssistantMessage),
		"source":     source,
		"source_key": sourceKey + ":bot",
		"input": map[string]any{
			"kind":        "codex_app_plugin_hook_conversation_bot",
			"event_type":  "final",
			"occurred_at": occurredAt,
			"visibility":  "issue_comment",
			"bot_label":   "bot",
			"session_id":  input.SessionID,
			"turn_id":     turnID,
			"model":       input.Model,
		},
	}
	var botResult map[string]any
	if err := client.PostJSON(ctx, path, botBody, &botResult); err != nil {
		return err
	}
	if err := codexPluginMarkConversationSynced(cmd, binding.BindingID, prompt.Prompt, sourceKey); err != nil {
		return err
	}
	return nil
}

func codexPluginMarkConversationSynced(cmd *cobra.Command, bindingID, userMessage, sourceKey string) error {
	userHash := codexPluginConversationUserHash(userMessage)
	if strings.TrimSpace(bindingID) == "" || userHash == "" {
		return nil
	}
	state, err := codexPluginReadHookState(cmd)
	if err != nil {
		return err
	}
	now := time.Now()
	synced := codexPluginHookSyncedTurn{
		BindingID: strings.TrimSpace(bindingID),
		UserHash:  userHash,
		SourceKey: strings.TrimSpace(sourceKey),
		SyncedAt:  now.UTC().Format(time.RFC3339Nano),
		UnixNS:    now.UnixNano(),
	}
	state.SyncedTurns = upsertCodexPluginSyncedTurn(state.SyncedTurns, synced)
	state.SyncedTurns = trimCodexPluginSyncedTurns(state.SyncedTurns, 200)
	return codexPluginWriteHookState(cmd, state)
}

func upsertCodexPluginSyncedTurn(turns []codexPluginHookSyncedTurn, synced codexPluginHookSyncedTurn) []codexPluginHookSyncedTurn {
	out := make([]codexPluginHookSyncedTurn, 0, len(turns)+1)
	for _, existing := range turns {
		if existing.BindingID == synced.BindingID && existing.UserHash == synced.UserHash {
			continue
		}
		out = append(out, existing)
	}
	return append([]codexPluginHookSyncedTurn{synced}, out...)
}

func trimCodexPluginSyncedTurns(turns []codexPluginHookSyncedTurn, max int) []codexPluginHookSyncedTurn {
	if len(turns) <= max {
		return turns
	}
	return turns[:max]
}

func codexPluginPromptAlreadySynced(turns []codexPluginHookSyncedTurn, bindingID, prompt string) bool {
	userHash := codexPluginConversationUserHash(prompt)
	if strings.TrimSpace(bindingID) == "" || userHash == "" {
		return false
	}
	for _, turn := range turns {
		if turn.BindingID == bindingID && turn.UserHash == userHash {
			return true
		}
	}
	return false
}

func codexPluginConversationUserHash(message string) string {
	normalized := strings.TrimSpace(message)
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", sum[:8])
}

func findCodexPluginPrompt(prompts []codexPluginHookPrompt, input codexPluginHookInput) (codexPluginHookPrompt, bool) {
	sessionID := strings.TrimSpace(input.SessionID)
	turnID := strings.TrimSpace(input.TurnID)
	for _, prompt := range prompts {
		if sessionID != "" && turnID != "" && prompt.SessionID == sessionID && prompt.TurnID == turnID {
			return prompt, true
		}
	}
	for _, prompt := range prompts {
		if sessionID != "" && prompt.SessionID == sessionID {
			return prompt, true
		}
	}
	return codexPluginHookPrompt{}, false
}

func findCodexPluginBinding(bindings []codexPluginHookBinding, input codexPluginHookInput, prompt codexPluginHookPrompt) (codexPluginHookBinding, bool) {
	sessionID := strings.TrimSpace(input.SessionID)
	for _, binding := range bindings {
		if sessionID != "" && binding.CodexSessionID == sessionID {
			return binding, true
		}
	}
	cwd := firstNonEmpty(strings.TrimSpace(input.CWD), strings.TrimSpace(prompt.CWD))
	for _, binding := range bindings {
		if cwd != "" && binding.ProjectFolder != "" && sameOrNestedPath(cwd, binding.ProjectFolder) {
			return binding, true
		}
	}
	if len(bindings) == 1 {
		return bindings[0], true
	}
	return codexPluginHookBinding{}, false
}

func sameOrNestedPath(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func codexPluginHookConversationSourceKey(input codexPluginHookInput, prompt codexPluginHookPrompt, binding codexPluginHookBinding) string {
	if input.SessionID != "" && input.TurnID != "" {
		return input.SessionID + ":turn:" + input.TurnID + ":conversation"
	}
	if prompt.SessionID != "" && prompt.TurnID != "" {
		return prompt.SessionID + ":turn:" + prompt.TurnID + ":conversation"
	}
	sum := sha256.Sum256([]byte(binding.BindingID + "\n" + prompt.Prompt + "\n" + input.LastAssistantMessage))
	return binding.BindingID + ":conversation:" + fmt.Sprintf("%x", sum[:8])
}

func codexPluginMCPError(err error) *mcpError {
	return &mcpError{
		Code:    -32000,
		Message: err.Error(),
	}
}

func buildCodexPluginMCPTools() []mcpToolDefinition {
	schema := buildCodexPluginSchema()
	tools := make([]mcpToolDefinition, 0, len(schema.Tools)-1)
	for _, tool := range schema.Tools {
		if tool.Name == "attachment_upload" {
			continue
		}
		tools = append(tools, mcpToolDefinition{
			Name:        tool.Name,
			Description: tool.Purpose,
			InputSchema: codexPluginMCPInputSchema(tool),
		})
	}
	return tools
}

func codexPluginMCPInputSchema(tool codexPluginToolSchema) map[string]any {
	properties := map[string]any{}
	for name, description := range tool.Input {
		properties[name] = map[string]any{
			"type":        codexPluginMCPJSONType(name),
			"description": description,
		}
	}
	requiredByTool := map[string][]string{
		"issue_get":            {"issue_ref"},
		"session_bind":         {"issue_id"},
		"runtime_event_append": {"issue_id", "binding_id", "event_type", "content"},
		"conversation_sync":    {"issue_id", "binding_id", "user_message", "bot_message"},
		"comment_add":          {"issue_id", "content"},
		"usage_update":         {"issue_id", "binding_id"},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             requiredByTool[tool.Name],
	}
}

func formatCodexConversationComment(userLabel, userMessage, botLabel, botMessage string) string {
	return formatCodexConversationPart(userLabel, userMessage) + "\n" + formatCodexConversationPart(botLabel, botMessage)
}

func formatCodexConversationPart(label, message string) string {
	label = strings.TrimSpace(label)
	message = strings.TrimSpace(message)
	if label == "" {
		label = "bot"
	}
	if !strings.Contains(message, "\n") {
		return label + "：" + message
	}
	return label + "：\n" + message
}

func codexPluginMCPJSONType(name string) any {
	switch name {
	case "limit", "input_tokens", "output_tokens", "total_tokens", "cached_input_tokens", "reasoning_tokens":
		return "integer"
	case "status":
		return []string{"string", "array"}
	default:
		return "string"
	}
}

func codexPluginMCPPage(result map[string]any, limitRaw, offsetRaw string) map[string]any {
	issues, _ := result["issues"].([]any)
	total := intFromAny(result["total"])
	limit, _ := strconv.Atoi(limitRaw)
	offset, _ := strconv.Atoi(offsetRaw)
	if limit <= 0 {
		limit = len(issues)
	}
	nextCursor := any(nil)
	if offset+len(issues) < total {
		nextCursor = strconv.Itoa(offset + len(issues))
	}
	return map[string]any{
		"items":       issues,
		"next_cursor": nextCursor,
		"total":       total,
	}
}

func mcpString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	switch v := args[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return ""
	}
}

func mcpStringSlice(args map[string]any, key string) []string {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func mcpInt(args map[string]any, key string) int {
	return int(mcpInt64(args, key))
}

func mcpInt64(args map[string]any, key string) int64 {
	if args == nil {
		return 0
	}
	switch v := args[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}

func mcpCursorOffset(args map[string]any) int {
	cursor := mcpString(args, "cursor")
	if cursor == "" {
		return 0
	}
	offset, _ := strconv.Atoi(cursor)
	return offset
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func defaultCodexPluginSourceKey(args map[string]any, suffix string) string {
	threadID := mcpString(args, "codex_thread_id")
	if threadID == "" {
		threadID = mcpString(args, "codex_session_id")
	}
	if threadID == "" {
		return ""
	}
	return "codex_app_plugin:" + threadID + ":" + suffix
}
