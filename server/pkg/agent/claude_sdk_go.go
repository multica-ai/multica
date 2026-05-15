package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	claudecode "github.com/severity1/claude-agent-sdk-go"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// executeWithGoSDK runs a Claude session using the Go SDK (claude-agent-sdk-go).
// This replaces the Node.js bridge path while keeping the same Session/Message/Result contract.
func (b *claudeBackend) executeWithGoSDK(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	sdkOpts := buildGoSDKOptions(b.cfg, opts, cancel)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		var output strings.Builder
		var approvedPlanSnapshot string
		var sessionID string
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)
		preservePlanOutput := false
		var mu sync.Mutex // protects plan state accessed from canUseTool callback

		// Build canUseTool callback that bridges SDK permission requests to Multica's approval flow
		if opts.OnApproval != nil {
			sdkOpts = append(sdkOpts, claudecode.WithCanUseTool(func(
				callCtx context.Context,
				toolName string,
				input map[string]any,
				permCtx claudecode.ToolPermissionContext,
			) (claudecode.PermissionResult, error) {
				return b.handleGoSDKToolPermission(callCtx, toolName, input, opts, &mu, &approvedPlanSnapshot, &preservePlanOutput, &output, cancel)
			}))
		}

		err := claudecode.WithClient(runCtx, func(client claudecode.Client) error {
			if opts.ResumeSessionID != "" {
				// For resume, use one-shot Query API (WithResume is set in sdkOpts)
				// Client mode doesn't support --resume flag directly
			}
			if err := client.Query(runCtx, prompt); err != nil {
				return fmt.Errorf("query: %w", err)
			}
			for msg := range client.ReceiveMessages(runCtx) {
				switch m := msg.(type) {
				case *claudecode.AssistantMessage:
					for _, block := range m.Content {
						switch bl := block.(type) {
						case *claudecode.TextBlock:
							output.WriteString(bl.Text)
							trySend(msgCh, Message{Type: MessageText, Content: bl.Text})
							if opts.TraceCallback != nil {
								opts.TraceCallback("normalized", bl.Text, "")
							}
							emitDisplayEvent(opts.TraceCallback, "assistant_text", "Claude", bl.Text, map[string]any{"bridge": "sdk_go"})
						case *claudecode.ThinkingBlock:
							trySend(msgCh, Message{Type: MessageThinking, Content: bl.Thinking})
							emitDisplayEvent(opts.TraceCallback, "thinking", "Thinking", bl.Thinking, map[string]any{"bridge": "sdk_go"})
						case *claudecode.ToolUseBlock:
							trySend(msgCh, Message{Type: MessageToolUse, Tool: bl.Name, CallID: bl.ToolUseID, Input: bl.Input})
							if opts.TraceCallback != nil {
								opts.TraceCallback("normalized", "[tool_use: "+bl.Name+"]", "")
							}
							emitDisplayEvent(opts.TraceCallback, "tool_call", bl.Name, "", map[string]any{"call_id": bl.ToolUseID, "input": bl.Input, "bridge": "sdk_go"})
						case *claudecode.ToolResultBlock:
							content := toolResultContent(bl)
							trySend(msgCh, Message{Type: MessageToolResult, CallID: bl.ToolUseID, Output: content})
							if opts.TraceCallback != nil {
								opts.TraceCallback("normalized", "[tool_result: "+bl.ToolUseID+"]", content)
							}
							emitDisplayEvent(opts.TraceCallback, "tool_result", "Tool result", content, map[string]any{"call_id": bl.ToolUseID, "bridge": "sdk_go"})
						}
					}
				case *claudecode.SystemMessage:
					trySend(msgCh, Message{Type: MessageStatus, Status: m.Subtype})
					emitDisplayEvent(opts.TraceCallback, "status", "Claude SDK", m.Subtype, map[string]any{"bridge": "sdk_go"})
				case *claudecode.ResultMessage:
					if m.SessionID != "" {
						sessionID = m.SessionID
					}
					if m.IsError {
						finalStatus = "failed"
						if len(m.Errors) > 0 {
							finalError = strings.Join(m.Errors, "; ")
						}
					}
					if m.Usage != nil {
						goSDKParseUsage(*m.Usage, opts.Model, usage)
					}
				}
			}
			return nil
		}, sdkOpts...)

		duration := time.Since(startTime)

		if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("claude go sdk timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled && finalStatus == "completed" {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else if finalStatus == "completed" {
				finalStatus = "failed"
				finalError = err.Error()
			}
		}

		b.cfg.Logger.Info("claude go sdk finished", "status", finalStatus, "duration", duration.Round(time.Millisecond).String())
		emitDisplayEvent(opts.TraceCallback, "status", "Claude SDK", finalStatus, map[string]any{"error": finalError})

		finalOutput := output.String()
		if approvedPlanSnapshot != "" {
			finalOutput = mergeApprovedPlanIntoOutput(opts.VisibleLanguage, approvedPlanSnapshot, finalOutput)
		}

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// handleGoSDKToolPermission handles permission requests from the Go SDK's canUseTool callback.
// It maps SDK tool calls to Multica's approval flow, handling ExitPlanMode, AskUserQuestion,
// and regular tool permissions.
func (b *claudeBackend) handleGoSDKToolPermission(
	ctx context.Context,
	toolName string,
	input map[string]any,
	opts ExecOptions,
	mu *sync.Mutex,
	approvedPlanSnapshot *string,
	preservePlanOutput *bool,
	output *strings.Builder,
	cancel context.CancelFunc,
) (claudecode.PermissionResult, error) {
	// Auto-allow trusted multica platform commands
	if toolName == "Bash" {
		if cmd, ok := input["command"].(string); ok && isTrustedPlatformCommand(cmd) {
			return claudecode.PermissionResultAllow{
				Behavior:     "allow",
				UpdatedInput: input,
			}, nil
		}
	}

	if toolName == "ExitPlanMode" {
		return b.handleExitPlanMode(ctx, input, opts, mu, approvedPlanSnapshot, preservePlanOutput, output, cancel)
	}

	if toolName == "AskUserQuestion" {
		return b.handleAskUserQuestion(ctx, input, opts)
	}

	// Regular tool permission
	return b.handleRegularToolPermission(ctx, toolName, input, opts)
}

func (b *claudeBackend) handleExitPlanMode(
	ctx context.Context,
	input map[string]any,
	opts ExecOptions,
	mu *sync.Mutex,
	approvedPlanSnapshot *string,
	preservePlanOutput *bool,
	output *strings.Builder,
	cancel context.CancelFunc,
) (claudecode.PermissionResult, error) {
	planText := ""
	if p, ok := input["plan"].(string); ok {
		planText = p
	}

	req := ApprovalRequest{
		Type:   protocol.InteractionPlanApproval,
		Title:  "Plan ready",
		Detail: planText,
		Options: []protocol.InteractionOption{
			{ID: "allow", Label: "Run this plan"},
			{ID: "revise", Label: "Revise plan"},
			{ID: "deny", Label: "Reject plan"},
		},
		DefaultOption: "deny",
	}

	if opts.TraceCallback != nil {
		emitDisplayEvent(opts.TraceCallback, "approval_prompt", "Claude SDK approval", req.Title, map[string]any{"bridge": "sdk_go", "interaction_type": req.Type})
	}

	chosen, _, err := opts.OnApproval(ctx, req)
	if err != nil {
		b.cfg.Logger.Warn("claude go sdk: plan approval callback failed", "error", err)
		chosen = "timeout"
	}
	chosen, responseMessage := SplitApprovalChoice(chosen)

	if shouldAbortPlanApproval(chosen, err) {
		emitPlanApprovalStage(opts.TraceCallback, chosen, false, responseMessage, opts.VisibleLanguage)
		// Abort: cancel context to stop the session
		cancel()
		return claudecode.NewPermissionResultDeny("plan rejected"), nil
	}

	mu.Lock()
	defer mu.Unlock()

	if chosen == "allow" || chosen == "accept_similar" {
		*approvedPlanSnapshot = strings.TrimSpace(req.Detail)
		*preservePlanOutput = false
		output.Reset()
		emitPlanApprovalStage(opts.TraceCallback, chosen, true, responseMessage, opts.VisibleLanguage)
		// Must pass updatedInput back to CLI — matches Node bridge behavior
		return claudecode.PermissionResultAllow{
			Behavior:     "allow",
			UpdatedInput: input,
		}, nil
	}

	// revise / keep_planning
	*preservePlanOutput = true
	emitPlanApprovalStage(opts.TraceCallback, chosen, false, responseMessage, opts.VisibleLanguage)
	msg := "Stay in plan mode. Do not execute anything."
	if responseMessage != "" {
		msg += " Revise based on: " + responseMessage
	}
	return claudecode.NewPermissionResultDeny(msg), nil
}

func (b *claudeBackend) handleAskUserQuestion(
	ctx context.Context,
	input map[string]any,
	opts ExecOptions,
) (claudecode.PermissionResult, error) {
	// Marshal input to JSON to reuse existing parsing logic
	raw, _ := json.Marshal(input)
	req := buildClaudeSDKUserInputRequest(raw)

	chosen, _, err := opts.OnApproval(ctx, req)
	if err != nil {
		b.cfg.Logger.Warn("claude go sdk: question callback failed", "error", err)
		return claudecode.NewPermissionResultDeny("user declined to answer"), nil
	}

	chosen, responseMessage := SplitApprovalChoice(chosen)

	// Same behavior as Node bridge: deny with answer injected as message
	var msg strings.Builder
	msg.WriteString("The user selected: ")
	msg.WriteString(chosen)
	if responseMessage != "" {
		msg.WriteString(". Additional input: ")
		msg.WriteString(responseMessage)
	}
	msg.WriteString(". Continue with this answer. Do not ask the same question again.")
	return claudecode.NewPermissionResultDeny(msg.String()), nil
}

func (b *claudeBackend) handleRegularToolPermission(
	ctx context.Context,
	toolName string,
	input map[string]any,
	opts ExecOptions,
) (claudecode.PermissionResult, error) {
	title := "Tool: " + toolName
	detail := ""
	if inputJSON, err := json.Marshal(input); err == nil {
		detail = string(inputJSON)
	}

	req := ApprovalRequest{
		Type:   protocol.InteractionPermissionRequest,
		Title:  title,
		Detail: detail,
		Options: []protocol.InteractionOption{
			{ID: "allow", Label: "Allow this tool"},
			{ID: "accept_similar", Label: "Allow similar this run"},
			{ID: "deny", Label: "Reject"},
		},
		DefaultOption: "deny",
	}

	chosen, approved, err := opts.OnApproval(ctx, req)
	if err != nil {
		b.cfg.Logger.Warn("claude go sdk: tool approval callback failed", "error", err, "tool", toolName)
		return claudecode.NewPermissionResultDeny("approval failed"), nil
	}

	if approved || chosen == "allow" || chosen == "accept_similar" {
		return claudecode.PermissionResultAllow{
			Behavior:     "allow",
			UpdatedInput: input,
		}, nil
	}
	return claudecode.NewPermissionResultDeny("denied by user"), nil
}

// buildGoSDKOptions maps ExecOptions to claudecode.Option slice.
func buildGoSDKOptions(cfg Config, opts ExecOptions, cancel context.CancelFunc) []claudecode.Option {
	var sdkOpts []claudecode.Option

	if cfg.ExecutablePath != "" {
		sdkOpts = append(sdkOpts, claudecode.WithCLIPath(cfg.ExecutablePath))
	}
	if opts.Cwd != "" {
		sdkOpts = append(sdkOpts, claudecode.WithCwd(opts.Cwd))
	}
	if opts.Model != "" {
		sdkOpts = append(sdkOpts, claudecode.WithModel(opts.Model))
	}
	if opts.SystemPrompt != "" {
		sdkOpts = append(sdkOpts, claudecode.WithAppendSystemPrompt(opts.SystemPrompt))
	}
	if opts.ResumeSessionID != "" {
		sdkOpts = append(sdkOpts, claudecode.WithResume(opts.ResumeSessionID))
	}
	if opts.MaxTurns > 0 {
		sdkOpts = append(sdkOpts, claudecode.WithMaxTurns(opts.MaxTurns))
	}
	if len(cfg.Env) > 0 {
		sdkOpts = append(sdkOpts, claudecode.WithEnv(cfg.Env))
	}

	// Ensure CLI loads user settings (e.g. ~/.claude/settings.json) which may contain
	// proxy env vars like ANTHROPIC_BASE_URL. The SDK defaults to --setting-sources ""
	// which disables all settings loading.
	sdkOpts = append(sdkOpts, claudecode.WithSettingSources(
		claudecode.SettingSourceUser,
		claudecode.SettingSourceProject,
		claudecode.SettingSourceLocal,
	))

	// Map permission mode
	permMode := goSDKPermissionMode(opts)
	sdkOpts = append(sdkOpts, claudecode.WithPermissionMode(permMode))

	return sdkOpts
}

func goSDKPermissionMode(opts ExecOptions) claudecode.PermissionMode {
	if opts.OnApproval == nil {
		return claudecode.PermissionModeBypassPermissions
	}
	switch strings.ToLower(opts.ClaudePermissionMode) {
	case "plan":
		return claudecode.PermissionModePlan
	case "acceptedits":
		return claudecode.PermissionModeAcceptEdits
	default:
		return claudecode.PermissionModeDefault
	}
}

// goSDKParseUsage extracts token usage from the SDK's ResultMessage.Usage map.
func goSDKParseUsage(rawUsage map[string]any, defaultModel string, usage map[string]TokenUsage) {
	// The SDK returns usage as map[string]any with model-keyed entries or flat fields
	// Try to extract from the raw map
	model := defaultModel
	if model == "" {
		model = "claude"
	}

	var tu TokenUsage
	if v, ok := rawUsage["input_tokens"]; ok {
		tu.InputTokens = toInt64(v)
	}
	if v, ok := rawUsage["output_tokens"]; ok {
		tu.OutputTokens = toInt64(v)
	}
	if v, ok := rawUsage["cache_read_input_tokens"]; ok {
		tu.CacheReadTokens = toInt64(v)
	}
	if v, ok := rawUsage["cache_creation_input_tokens"]; ok {
		tu.CacheWriteTokens = toInt64(v)
	}
	if tu.InputTokens > 0 || tu.OutputTokens > 0 {
		usage[model] = tu
	}
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

// toolResultContent extracts text content from a ToolResultBlock.
func toolResultContent(bl *claudecode.ToolResultBlock) string {
	if bl.Content == nil {
		return ""
	}
	switch v := bl.Content.(type) {
	case string:
		return v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}
