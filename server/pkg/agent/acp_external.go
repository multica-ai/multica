package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// acpExternalBackend implements Backend for external runtime extensions
// loaded from runtime.json. It spawns the CLI with the manifest-declared
// command args (typically `--acp`) and drives an ACP JSON-RPC 2.0 handshake
// over stdin/stdout.
//
// Capability-driven parameter forwarding: optional ACP `session/new` params
// (mcpServers, sessionId, thinkingLevel, maxTurns) are only forwarded when
// the manifest opts in via capabilities, so a runtime that ignores extra
// params doesn't see noise it might reject. The contract is:
//
//   - mcp_config         → params.mcpServers (array of {name,command,args,env})
//   - session_resume     → params.sessionId  (resume an existing session)
//   - max_turns          → params.maxTurns
//   - thinking           → params.thinkingLevel
//   - model_selection    → params.model      (always forwarded; gated by opt-in)
//
// All forwarded params are also subject to opts.* being non-empty / non-zero.
type acpExternalBackend struct {
	cfg Config
}

const acpExternalExitGrace = 5 * time.Second

func (b *acpExternalBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		return nil, fmt.Errorf("acp external backend: executable path is empty")
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("acp external executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	blocked := manifestBlockedArgs(b.cfg.BlockedArgs)
	args := append([]string{}, b.cfg.ACPArgs...)
	args = append(args, filterCustomArgs(opts.ExtraArgs, blocked, b.cfg.Logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, blocked, b.cfg.Logger)...)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("acp external command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(mergeRuntimeEnv(b.cfg.Env, b.cfg.SkillsRoot))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("acp external stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("acp external stdin pipe: %w", err)
	}
	var closeStdinOnce sync.Once
	closeStdin := func() { closeStdinOnce.Do(func() { _ = stdin.Close() }) }
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[acp-ext:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start acp external: %w", err)
	}

	b.cfg.Logger.Info("acp external started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		<-runCtx.Done()
		closeStdin()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer closeStdin()
		defer close(msgCh)
		defer close(resCh)

		trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

		startTime := time.Now()
		var outputMu sync.Mutex
		var output strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string
		var streamingCurrentTurn atomic.Bool
		var err error

		promptDone := make(chan hermesPromptResult, 1)
		c := &hermesClient{
			cfg:          b.cfg,
			stdin:        stdin,
			pending:      make(map[int]*pendingRPC),
			pendingTools: make(map[string]*pendingToolCall),
			acceptNotification: func(string) bool {
				return streamingCurrentTurn.Load()
			},
			onMessage: func(msg Message) {
				if !streamingCurrentTurn.Load() {
					return
				}
				if msg.Type == MessageText {
					outputMu.Lock()
					output.WriteString(msg.Content)
					outputMu.Unlock()
				}
				trySend(msgCh, msg)
			},
			onPromptDone: func(result hermesPromptResult) {
				if !streamingCurrentTurn.Load() {
					return
				}
				select {
				case promptDone <- result:
				default:
				}
			},
		}

		readerDone := make(chan struct{})
		go func() {
			defer close(readerDone)
			scanner := bufio.NewScanner(stdout)
			scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				c.handleLine(line)
			}
			c.closeAllPending(fmt.Errorf("acp external process exited"))
		}()

		{
			// Phase 1: ACP initialize handshake.
			initResult, err := c.request(runCtx, "initialize", map[string]any{
				"protocolVersion": 1,
				"clientInfo": map[string]any{
					"name":    "multica-agent-sdk",
					"version": "0.2.0",
				},
				"clientCapabilities": map[string]any{},
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("acp initialize failed: %v", err)
				goto finish
			}

			// Phase 2: session/new — forward optional params per capability flags
			// and read the response so session/prompt can carry the ACP session ID.
			sessionParams := buildACPSessionParams(opts, b.cfg.Capabilities)
			if servers, ok := sessionParams["mcpServers"].([]map[string]any); ok {
				serversAny := make([]any, 0, len(servers))
				for _, server := range servers {
					serversAny = append(serversAny, server)
				}
				sessionParams["mcpServers"] = filterACPMcpServersByCapability(serversAny, extractACPMcpCapabilities(initResult), "acp external", b.cfg.Logger)
			}
			sessionResult, err := c.request(runCtx, "session/new", sessionParams)
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("acp session/new failed: %v", err)
				goto finish
			}
			sessionID = extractACPSessionID(sessionResult)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "acp session/new returned no session ID"
				goto finish
			}
			c.sessionID = sessionID
		}

		// Phase 3: session/prompt. ACP runtimes expect the sessionId
		// returned by session/new plus text-block prompt content. Send both
		// `prompt` and `content` so current Kiro-style and canonical ACP
		// shapes are accepted by generic runtime extensions.
		streamingCurrentTurn.Store(true)
		_, err = c.request(runCtx, "session/prompt", buildACPPromptParams(sessionID, prompt))
		if err != nil {
			switch {
			case runCtx.Err() == context.DeadlineExceeded:
				finalStatus = "timeout"
				finalError = fmt.Sprintf("acp external timed out after %s", timeout)
			case runCtx.Err() == context.Canceled:
				finalStatus = "aborted"
				finalError = "execution cancelled"
			default:
				finalStatus = "failed"
				finalError = fmt.Sprintf("acp session/prompt failed: %v", err)
			}
			goto finish
		}
		select {
		case pr := <-promptDone:
			if pr.stopReason == "cancelled" {
				finalStatus = "aborted"
				finalError = "acp external cancelled the prompt"
			}
			c.usageMu.Lock()
			c.usage.InputTokens += pr.usage.InputTokens
			c.usage.OutputTokens += pr.usage.OutputTokens
			c.usage.CacheReadTokens += pr.usage.CacheReadTokens
			c.usage.CacheWriteTokens += pr.usage.CacheWriteTokens
			c.usageMu.Unlock()
		default:
		}

	finish:
		closeStdin()
		ctxErr := runCtx.Err()
		waitDone := make(chan error, 1)
		go func() { waitDone <- cmd.Wait() }()

		var waitErr error
		select {
		case waitErr = <-waitDone:
		case <-runCtx.Done():
			ctxErr = runCtx.Err()
			cancel()
			waitErr = <-waitDone
		case <-time.After(acpExternalExitGrace):
			cancel()
			waitErr = <-waitDone
			if finalStatus == "completed" {
				finalStatus = "failed"
				finalError = fmt.Sprintf("acp external did not exit within %s after prompt completion", acpExternalExitGrace)
			}
		}
		<-readerDone
		cancel()
		duration := time.Since(startTime)

		switch {
		case ctxErr == context.DeadlineExceeded:
			finalStatus = "timeout"
			finalError = fmt.Sprintf("acp external timed out after %s", timeout)
		case ctxErr == context.Canceled:
			finalStatus = "aborted"
			finalError = "execution cancelled"
		case waitErr != nil && finalStatus == "completed":
			finalStatus = "failed"
			finalError = fmt.Sprintf("acp external exited with error: %v", waitErr)
		}

		if finalStatus == "failed" || finalStatus == "aborted" {
			if tail := stderrBuf.Tail(); tail != "" {
				finalError += fmt.Sprintf("\n[stderr tail]\n%s", tail)
			}
		}

		b.cfg.Logger.Info("acp external finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()
		c.usageMu.Lock()
		u := c.usage
		c.usageMu.Unlock()
		var usageMap map[string]TokenUsage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := opts.Model
			if model == "" {
				model = "unknown"
			}
			usageMap = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usageMap,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func buildACPPromptParams(sessionID, prompt string) map[string]any {
	promptBlocks := []map[string]any{
		{"type": "text", "text": prompt},
	}
	return map[string]any{
		"sessionId": sessionID,
		"content":   promptBlocks,
		"prompt":    promptBlocks,
	}
}

// buildACPSessionParams assembles the params object for the ACP `session/new`
// request, forwarding optional fields only when the manifest opted in to the
// matching capability AND opts.* carries a value. Keeping the gate explicit
// means a runtime that doesn't tolerate extra fields stays opt-in safe, while
// one that does can expose model/MCP/thinking simply by setting capability
// flags to `true` in runtime.json.
func buildACPSessionParams(opts ExecOptions, caps ConfigCapabilities) map[string]any {
	params := map[string]any{}
	if opts.Cwd != "" {
		params["cwd"] = opts.Cwd
	}
	if caps.ModelSelection && opts.Model != "" {
		params["model"] = opts.Model
	}
	if caps.SessionResume && opts.ResumeSessionID != "" {
		params["sessionId"] = opts.ResumeSessionID
	}
	if caps.MaxTurns && opts.MaxTurns > 0 {
		params["maxTurns"] = opts.MaxTurns
	}
	if caps.Thinking && opts.ThinkingLevel != "" {
		params["thinkingLevel"] = opts.ThinkingLevel
	}
	if caps.McpConfig && len(opts.McpConfig) > 0 {
		// Translate the daemon's MCP config (raw JSON, may be either an
		// object-of-objects { name: { command, args, env } } or already
		// an ACP-shaped { mcpServers: [...] }) into the array shape ACP
		// `session/new` expects. Failures fall back to passing the raw
		// message through under "mcpConfig" so a runtime that wants the
		// untranslated payload can still consume it.
		if servers, ok := normalizeMCPServers(opts.McpConfig); ok {
			params["mcpServers"] = servers
		} else {
			params["mcpConfig"] = json.RawMessage(opts.McpConfig)
		}
	}
	return params
}

// normalizeMCPServers accepts either of the two MCP config shapes the
// daemon may carry and returns an ACP-style array of server descriptors.
//
//	Shape A (Claude-style): {"name":{"command":"x","args":[...],"env":{...}}}
//	Shape B (ACP-native):   {"mcpServers":[{"name":"x", ...}]}
//
// Returns the array and true on success; (nil, false) when the payload
// cannot be parsed into a recognised shape.
func normalizeMCPServers(raw json.RawMessage) ([]map[string]any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	// Try shape B first.
	var direct struct {
		McpServers []map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &direct); err == nil && len(direct.McpServers) > 0 {
		return direct.McpServers, true
	}
	// Fall back to shape A.
	var byName map[string]map[string]any
	if err := json.Unmarshal(raw, &byName); err == nil && len(byName) > 0 {
		out := make([]map[string]any, 0, len(byName))
		for name, srv := range byName {
			entry := map[string]any{"name": name}
			for k, v := range srv {
				entry[k] = v
			}
			out = append(out, entry)
		}
		return out, true
	}
	return nil, false
}

// skipToResponse reads JSON-RPC frames from decoder until it finds one
// with an "id" field (a response, not a notification). The response is
// unmarshaled into dst. Returns an error if the response contains an
// "error" field or if decoding fails.
func (b *acpExternalBackend) skipToResponse(decoder *json.Decoder, dst *map[string]any) error {
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return fmt.Errorf("acp read response: %w", err)
		}
		var frame map[string]any
		if err := json.Unmarshal(raw, &frame); err != nil {
			continue
		}
		if _, hasID := frame["id"]; !hasID {
			continue
		}
		if errMsg, ok := frame["error"]; ok {
			return fmt.Errorf("acp rpc error: %v", errMsg)
		}
		*dst = frame
		return nil
	}
}

func (b *acpExternalBackend) handleAssistantMessage(params map[string]any, ch chan<- Message, output *strings.Builder, usage map[string]TokenUsage) {
	content, _ := params["content"].([]any)
	if content == nil {
		if text, ok := params["text"].(string); ok && text != "" {
			output.WriteString(text)
			trySend(ch, Message{Type: MessageText, Content: text})
		}
		return
	}

	for _, block := range content {
		bmap, ok := block.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := bmap["type"].(string)
		switch typ {
		case "text":
			if text, ok := bmap["text"].(string); ok && text != "" {
				output.WriteString(text)
				trySend(ch, Message{Type: MessageText, Content: text})
			}
		case "tool_use":
			toolName, _ := bmap["name"].(string)
			callID, _ := bmap["id"].(string)
			input, _ := bmap["input"].(map[string]any)
			trySend(ch, Message{
				Type:   MessageToolUse,
				Tool:   toolName,
				CallID: callID,
				Input:  input,
			})
		}
	}
}

func (b *acpExternalBackend) handleToolResult(params map[string]any, ch chan<- Message) {
	callID, _ := params["toolCallId"].(string)
	content, _ := params["content"].(string)
	trySend(ch, Message{
		Type:   MessageToolResult,
		CallID: callID,
		Output: content,
	})
}

// manifestBlockedArgs converts the manifest's string-valued blocked-args map
// into the internal blockedArgMode form filterCustomArgs consumes. Manifest
// values are expected to be either "value" (flag takes a value, blocked
// together with its argument) or "flag" (boolean flag, no value to skip).
// Unknown values default to "value" since that is the safer interpretation
// — an extra argv slot is dropped rather than a real positional being
// shifted into the blocked flag's slot.
func manifestBlockedArgs(in map[string]string) map[string]blockedArgMode {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]blockedArgMode, len(in))
	for flag, mode := range in {
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "flag", "standalone", "bool", "boolean":
			out[flag] = blockedStandalone
		default:
			out[flag] = blockedWithValue
		}
	}
	return out
}

// mergeRuntimeEnv augments the daemon-supplied env map with manifest-declared
// env entries and the optional MULTICA_AGENT_SKILLS_ROOT pointer. Existing
// keys in `extra` win — the daemon already injects task-scoped credentials
// through it and a manifest must not be able to clobber them.
func mergeRuntimeEnv(extra map[string]string, skillsRoot string) map[string]string {
	out := make(map[string]string, len(extra)+2)
	for k, v := range extra {
		out[k] = v
	}
	if skillsRoot != "" {
		if _, set := out["MULTICA_AGENT_SKILLS_ROOT"]; !set {
			out["MULTICA_AGENT_SKILLS_ROOT"] = skillsRoot
		}
	}
	return out
}
