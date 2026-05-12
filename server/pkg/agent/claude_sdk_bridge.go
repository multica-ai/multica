package agent

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

//go:embed claude_sdk_bridge.mjs
var claudeSDKBridgeScript string

type claudeSDKBridgeConfig struct {
	Prompt          string          `json:"prompt"`
	Cwd             string          `json:"cwd,omitempty"`
	Model           string          `json:"model,omitempty"`
	PermissionMode  string          `json:"permission_mode,omitempty"`
	VisibleLanguage string          `json:"visible_language,omitempty"`
	ResumeSessionID string          `json:"resume_session_id,omitempty"`
	ExecutablePath  string          `json:"executable_path,omitempty"`
	SystemPrompt    string          `json:"system_prompt,omitempty"`
	McpConfig       json.RawMessage `json:"mcp_config,omitempty"`
	RequireRoot     string          `json:"require_root,omitempty"`
}

type claudeSDKBridgeEvent struct {
	Type      string          `json:"type"`
	Message   json.RawMessage `json:"message,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Context   json.RawMessage `json:"context,omitempty"`
	Options   json.RawMessage `json:"options,omitempty"`
	Title     string          `json:"title,omitempty"`
	Display   string          `json:"display_name,omitempty"`
	Desc      string          `json:"description,omitempty"`
	Blocked   string          `json:"blocked_path,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Stage     string          `json:"stage,omitempty"`
	Content   string          `json:"content,omitempty"`
	Status    string          `json:"status,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Output    string          `json:"output,omitempty"`
	Error     string          `json:"error,omitempty"`
	Usage     *struct {
		Model                    string `json:"model,omitempty"`
		InputTokens              int64  `json:"input_tokens,omitempty"`
		OutputTokens             int64  `json:"output_tokens,omitempty"`
		CacheReadInputTokens     int64  `json:"cache_read_input_tokens,omitempty"`
		CacheCreationInputTokens int64  `json:"cache_creation_input_tokens,omitempty"`
	} `json:"usage,omitempty"`
	Detail string `json:"detail,omitempty"`
}

func (b *claudeBackend) executeWithSDKBridge(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	scriptPath, cleanup, err := writeClaudeSDKBridgeScript()
	if err != nil {
		cancel()
		return nil, err
	}

	requireRoot := resolveClaudeSDKRequireRoot(opts.Cwd)
	logClaudeSDKDependencyStatus(b.cfg.Logger, requireRoot)
	cfg := claudeSDKBridgeConfig{
		Prompt:          prompt,
		Cwd:             opts.Cwd,
		Model:           opts.Model,
		PermissionMode:  opts.ClaudePermissionMode,
		VisibleLanguage: normalizedVisibleLanguage(opts.VisibleLanguage),
		ResumeSessionID: opts.ResumeSessionID,
		ExecutablePath:  b.cfg.ExecutablePath,
		SystemPrompt:    opts.SystemPrompt,
		McpConfig:       opts.McpConfig,
		RequireRoot:     requireRoot,
	}
	if cfg.PermissionMode == "" {
		cfg.PermissionMode = "plan"
	}
	cfgData, err := json.Marshal(cfg)
	if err != nil {
		cleanup()
		cancel()
		return nil, fmt.Errorf("marshal claude sdk bridge config: %w", err)
	}

	cmd := exec.CommandContext(runCtx, "node", scriptPath, base64.RawURLEncoding.EncodeToString(cfgData))
	b.cfg.Logger.Info("agent command", "exec", "node", "args", []string{scriptPath, "<config>"})
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)
	if requireRoot != "" {
		cmd.Env = append(cmd.Env, "MULTICA_NODE_REQUIRE_ROOT="+requireRoot)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		cancel()
		return nil, fmt.Errorf("claude sdk bridge stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cleanup()
		cancel()
		return nil, fmt.Errorf("claude sdk bridge stdin pipe: %w", err)
	}
	closeStdin := func() {
		if stdin != nil {
			_ = stdin.Close()
			stdin = nil
		}
	}
	if opts.TraceCallback != nil {
		cmd.Stderr = io.MultiWriter(newLogWriter(b.cfg.Logger, "[claude-sdk:stderr] "), newTraceWriter("raw_stderr", opts.TraceCallback))
	} else {
		cmd.Stderr = newLogWriter(b.cfg.Logger, "[claude-sdk:stderr] ")
	}

	if err := cmd.Start(); err != nil {
		cleanup()
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start claude sdk bridge: %w", err)
	}
	b.cfg.Logger.Info("claude sdk bridge started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer cleanup()
		defer close(msgCh)
		defer close(resCh)
		defer closeStdin()
		go func() {
			<-runCtx.Done()
			_ = stdout.Close()
		}()

		startTime := time.Now()
		var output strings.Builder
		var approvedPlanSnapshot string
		var sessionID string
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)
		var writeMu sync.Mutex
		preservePlanOutput := false
		abortRun := func(status, errMsg string) {
			finalStatus = status
			finalError = errMsg
			cancel()
			_ = stdout.Close()
		}

		writeApproval := func(requestID, chosen string, approved bool) (string, string) {
			if chosen == "" {
				if approved {
					chosen = "allow"
				} else {
					chosen = "deny"
				}
			}
			chosen, responseMessage := SplitApprovalChoice(chosen)
			resp := map[string]any{
				"type":             "approval_response",
				"request_id":       requestID,
				"chosen_option":    chosen,
				"approved":         approved,
				"response_message": responseMessage,
			}
			data, err := json.Marshal(resp)
			if err != nil {
				return chosen, responseMessage
			}
			data = append(data, '\n')
			writeMu.Lock()
			defer writeMu.Unlock()
			if stdin != nil {
				if _, err := stdin.Write(data); err != nil {
					b.cfg.Logger.Warn("claude sdk bridge: failed to write approval response", "error", err)
				}
			}
			return chosen, responseMessage
		}

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			rawLine := strings.TrimSpace(scanner.Text())
			if rawLine == "" {
				continue
			}
			if opts.TraceCallback != nil {
				opts.TraceCallback("raw_stdout", rawLine, "")
			}
			var event claudeSDKBridgeEvent
			if err := json.Unmarshal([]byte(rawLine), &event); err != nil {
				continue
			}
			if event.Type == "sdk_message" {
				if opts.TraceCallback != nil {
					opts.TraceCallback("provider_event", "", string(event.Message))
				}
				continue
			}
			if event.SessionID != "" {
				sessionID = event.SessionID
			}
			switch event.Type {
			case "stage":
				content := event.Content
				if content == "" {
					content = event.Stage
				}
				emitDisplayEvent(opts.TraceCallback, "plan_stage", "Claude native plan", content, map[string]any{"stage": event.Stage, "bridge": "sdk"})
			case "status":
				trySend(msgCh, Message{Type: MessageStatus, Status: event.Status})
				emitDisplayEvent(opts.TraceCallback, "status", "Claude SDK", event.Status, map[string]any{"session_id": event.SessionID})
			case "assistant_text":
				output.WriteString(event.Text)
				trySend(msgCh, Message{Type: MessageText, Content: event.Text})
				if opts.TraceCallback != nil {
					opts.TraceCallback("normalized", event.Text, "")
				}
				emitDisplayEvent(opts.TraceCallback, "assistant_text", "Claude", event.Text, map[string]any{"bridge": "sdk"})
			case "thinking":
				trySend(msgCh, Message{Type: MessageThinking, Content: event.Text})
				emitDisplayEvent(opts.TraceCallback, "thinking", "Thinking", event.Text, map[string]any{"bridge": "sdk"})
			case "tool_use":
				inputMap := rawJSONToMap(event.Input)
				trySend(msgCh, Message{Type: MessageToolUse, Tool: event.Name, CallID: event.ID, Input: inputMap})
				if opts.TraceCallback != nil {
					opts.TraceCallback("normalized", "[tool_use: "+event.Name+"]", "")
				}
				emitDisplayEvent(opts.TraceCallback, "tool_call", event.Name, "", map[string]any{"call_id": event.ID, "input": inputMap, "bridge": "sdk"})
			case "tool_result":
				trySend(msgCh, Message{Type: MessageToolResult, CallID: event.ID, Output: event.Content})
				if opts.TraceCallback != nil {
					opts.TraceCallback("normalized", "[tool_result: "+event.ID+"]", event.Content)
				}
				emitDisplayEvent(opts.TraceCallback, "tool_result", "Tool result", event.Content, map[string]any{"call_id": event.ID, "bridge": "sdk"})
			case "approval_request":
				req := bridgeApprovalRequest(event)
				if opts.OnApproval == nil {
					_, _ = writeApproval(event.RequestID, "allow", true)
					continue
				}
				if req.Type == protocol.InteractionPlanApproval {
					emitDisplayEvent(opts.TraceCallback, "approval_prompt", "Claude SDK approval", req.Title, map[string]any{"request_id": event.RequestID, "bridge": "sdk", "interaction_type": req.Type})
				}
				chosen, approved, err := opts.OnApproval(runCtx, req)
				if err != nil {
					b.cfg.Logger.Warn("claude sdk bridge: approval callback failed", "error", err)
					chosen, approved = "timeout", false
				}
				chosen, responseMessage := SplitApprovalChoice(chosen)
				if req.Type == protocol.InteractionPlanApproval && shouldAbortPlanApproval(chosen, err) {
					emitPlanApprovalStage(opts.TraceCallback, chosen, approved, responseMessage, cfg.VisibleLanguage)
					abortRun("aborted", planApprovalAbortMessage(chosen, err))
					continue
				}
				chosen, responseMessage = writeApproval(event.RequestID, EncodeApprovalChoice(chosen, responseMessage), approved)
				if req.Type == protocol.InteractionPlanApproval {
					if chosen == "allow" || chosen == "accept_similar" {
						approvedPlanSnapshot = strings.TrimSpace(req.Detail)
						preservePlanOutput = false
						output.Reset()
					} else if chosen == "revise" || chosen == "keep_planning" {
						preservePlanOutput = true
					}
					emitPlanApprovalStage(opts.TraceCallback, chosen, approved, responseMessage, cfg.VisibleLanguage)
				}
			case "result":
				if event.Output != "" {
					if output.Len() == 0 || !preservePlanOutput {
						output.Reset()
						output.WriteString(event.Output)
					}
				}
				if event.SessionID != "" {
					sessionID = event.SessionID
				}
				if event.Status != "" {
					finalStatus = event.Status
				}
				if event.Error != "" {
					finalError = event.Error
					if finalStatus == "completed" {
						finalStatus = "failed"
					}
				}
				if event.Usage != nil {
					model := event.Usage.Model
					if model == "" {
						model = opts.Model
					}
					if model == "" {
						model = "claude"
					}
					usage[model] = TokenUsage{
						InputTokens:      event.Usage.InputTokens,
						OutputTokens:     event.Usage.OutputTokens,
						CacheReadTokens:  event.Usage.CacheReadInputTokens,
						CacheWriteTokens: event.Usage.CacheCreationInputTokens,
					}
				}
			case "error":
				finalStatus = "failed"
				finalError = event.Content
				if finalError == "" {
					finalError = event.Error
				}
				if finalError == "" {
					finalError = event.Detail
				}
				trySend(msgCh, Message{Type: MessageError, Content: finalError})
				emitDisplayEvent(opts.TraceCallback, "error", "Claude SDK bridge error", finalError, map[string]any{"bridge": "sdk"})
			}
		}

		exitErr := cmd.Wait()
		duration := time.Since(startTime)
		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("claude sdk bridge timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled && finalStatus == "completed" {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if exitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("claude sdk bridge exited with error: %v", exitErr)
		}
		b.cfg.Logger.Info("claude sdk bridge finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())
		emitDisplayEvent(opts.TraceCallback, "status", "Claude SDK", finalStatus, map[string]any{"error": finalError})
		finalOutput := output.String()
		if approvedPlanSnapshot != "" {
			finalOutput = mergeApprovedPlanIntoOutput(cfg.VisibleLanguage, approvedPlanSnapshot, finalOutput)
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

func bridgeApprovalRequest(event claudeSDKBridgeEvent) ApprovalRequest {
	interactionType := protocol.InteractionPermissionRequest
	title := "Claude approval"
	if event.Title != "" {
		title = event.Title
	} else if event.Display != "" {
		title = event.Display
	} else if event.ToolName != "" {
		title = "Tool: " + event.ToolName
	}
	detail := string(event.Input)
	if detail == "" || detail == "null" {
		detail = string(event.Context)
	}
	if event.Desc != "" {
		if detail == "" {
			detail = event.Desc
		} else {
			detail = event.Desc + "\n\n" + detail
		}
	}
	if event.Blocked != "" {
		detail = strings.TrimSpace(detail + "\n\nBlocked path: " + event.Blocked)
	}
	options := []protocol.InteractionOption{
		{ID: "allow", Label: "Allow this tool"},
		{ID: "accept_similar", Label: "Allow similar this run"},
		{ID: "deny", Label: "Reject"},
	}
	if event.ToolName == "ExitPlanMode" {
		interactionType = protocol.InteractionPlanApproval
		title = "Plan ready"
		if plan := extractExitPlanText(event.Input); plan != "" {
			detail = plan
		}
		options = []protocol.InteractionOption{
			{ID: "allow", Label: "Run this plan"},
			{ID: "revise", Label: "Revise plan"},
			{ID: "deny", Label: "Reject plan"},
		}
	} else if event.ToolName == "AskUserQuestion" {
		title = "Claude needs input"
		options = []protocol.InteractionOption{
			{ID: "allow", Label: "Show question"},
			{ID: "deny", Label: "Cancel question"},
		}
	}
	return ApprovalRequest{
		Type:          interactionType,
		Title:         title,
		Detail:        detail,
		Options:       options,
		DefaultOption: "deny",
	}
}

func shouldAbortPlanApproval(chosen string, err error) bool {
	chosen = strings.ToLower(strings.TrimSpace(chosen))
	if err != nil {
		return true
	}
	switch chosen {
	case "deny", "reject", "decline", "cancel", "stop", "timeout", "timed_out", "cancelled":
		return true
	default:
		return false
	}
}

func planApprovalAbortMessage(chosen string, err error) string {
	if err != nil {
		return fmt.Sprintf("plan approval did not receive a response: %v", err)
	}
	switch strings.ToLower(strings.TrimSpace(chosen)) {
	case "timeout", "timed_out":
		return "plan approval timed out before a response"
	case "cancelled":
		return "plan approval was cancelled"
	default:
		return "plan rejected by user"
	}
}

func emitPlanApprovalStage(trace TraceCallback, chosen string, approved bool, responseMessage string, language string) {
	language = normalizedVisibleLanguage(language)
	switch strings.ToLower(strings.TrimSpace(chosen)) {
	case "allow", "accept_similar":
		emitDisplayEvent(trace, "plan_stage", "Plan accepted", localizedPlanStageContent(language, "accepted", responseMessage), map[string]any{
			"stage": "executing",
		})
	case "revise", "keep_planning":
		emitDisplayEvent(trace, "plan_stage", "Plan revision requested", localizedPlanStageContent(language, "revise", responseMessage), map[string]any{
			"stage": "planning",
		})
	default:
		if approved {
			return
		}
		stage := "rejected"
		title := "Plan rejected"
		content := "The plan was rejected and Claude should stop this plan run."
		switch strings.ToLower(strings.TrimSpace(chosen)) {
		case "timeout", "timed_out":
			stage = "expired"
			title = "Plan approval expired"
			content = localizedPlanStageContent(language, "expired", responseMessage)
		case "cancelled":
			stage = "cancelled"
			title = "Plan approval cancelled"
			content = localizedPlanStageContent(language, "cancelled", responseMessage)
		default:
			content = localizedPlanStageContent(language, "rejected", responseMessage)
		}
		emitDisplayEvent(trace, "plan_stage", title, content, map[string]any{
			"stage": stage,
		})
	}
}

func normalizedVisibleLanguage(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	switch {
	case strings.HasPrefix(language, "zh"):
		return "zh"
	default:
		return "en"
	}
}

func localizedPlanStageContent(language, kind, responseMessage string) string {
	language = normalizedVisibleLanguage(language)
	responseMessage = strings.TrimSpace(responseMessage)
	if language == "zh" {
		switch kind {
		case "accepted":
			return "Claude 已退出计划模式，并将在这次运行中继续执行。"
		case "revise":
			content := "Claude 将继续停留在计划模式中修改方案。"
			if responseMessage != "" {
				content += "\n\n修改要求：\n" + responseMessage
			}
			return content
		case "expired":
			return "该计划确认长时间未处理，本次运行已停止，且不会向 Claude 回写拒绝。"
		case "cancelled":
			return "该计划确认已被取消，本次运行已停止，且不会向 Claude 回写拒绝。"
		default:
			return "该计划已被拒绝，Claude 将停止这次计划运行。"
		}
	}
	switch kind {
	case "accepted":
		return "Claude exited plan mode and is continuing execution in this run."
	case "revise":
		content := "Claude is staying in plan mode to revise the plan."
		if responseMessage != "" {
			content += "\n\nRevision request:\n" + responseMessage
		}
		return content
	case "expired":
		return "The plan approval was not answered in time, so this run was stopped without sending a rejection back to Claude."
	case "cancelled":
		return "The plan approval was cancelled, so this run was stopped without sending a rejection back to Claude."
	default:
		return "The plan was rejected and Claude should stop this plan run."
	}
}

func mergeApprovedPlanIntoOutput(language, approvedPlan, executionOutput string) string {
	approvedPlan = strings.TrimSpace(approvedPlan)
	executionOutput = strings.TrimSpace(executionOutput)
	if approvedPlan == "" {
		return executionOutput
	}
	if executionOutput == "" {
		return approvedPlan
	}
	if strings.Contains(executionOutput, approvedPlan) {
		return executionOutput
	}
	if normalizedVisibleLanguage(language) == "zh" {
		return "已批准方案：\n\n" + approvedPlan + "\n\n执行结果：\n\n" + executionOutput
	}
	return "Approved plan:\n\n" + approvedPlan + "\n\nExecution result:\n\n" + executionOutput
}

func extractExitPlanText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		return ""
	}
	for _, key := range []string{"plan", "content", "summary", "message"} {
		if value, ok := record[key]; ok {
			if s := strings.TrimSpace(fmt.Sprint(value)); s != "" {
				return s
			}
		}
	}
	return ""
}

func rawJSONToMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

func writeClaudeSDKBridgeScript() (string, func(), error) {
	dir, err := os.MkdirTemp("", "multica-claude-sdk-bridge-*")
	if err != nil {
		return "", nil, fmt.Errorf("create claude sdk bridge temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, "claude_sdk_bridge.mjs")
	if err := os.WriteFile(path, []byte(claudeSDKBridgeScript), 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write claude sdk bridge script: %w", err)
	}
	return path, cleanup, nil
}

func resolveClaudeSDKRequireRoot(cwd string) string {
	if v := os.Getenv("MULTICA_NODE_REQUIRE_ROOT"); v != "" {
		return v
	}
	if root := findPackageRootFromCaller(); root != "" {
		return root
	}
	if cwd != "" {
		if root := findPackageRoot(cwd); root != "" {
			return root
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return findPackageRoot(wd)
	}
	return ""
}

func findPackageRootFromCaller() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		if fileExists(filepath.Join(dir, "package.json")) && fileExists(filepath.Join(dir, "pnpm-workspace.yaml")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func findPackageRoot(start string) string {
	dir := start
	for i := 0; i < 8; i++ {
		if fileExists(filepath.Join(dir, "package.json")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func hasClaudeSDKDependency(root string) bool {
	if root == "" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), `"@anthropic-ai/claude-agent-sdk"`) ||
		strings.Contains(string(data), `"@anthropic-ai/claude-code"`)
}

func logClaudeSDKDependencyStatus(logger *slog.Logger, root string) {
	if logger == nil || hasClaudeSDKDependency(root) {
		return
	}
	logger.Warn("claude sdk bridge dependency is not declared in package.json", "root", root)
}
