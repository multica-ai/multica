package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	dshProfile         = "multica"
	dshProtocolVersion = 1
	dshCancelGrace     = 3 * time.Second
	dshTerminateGrace  = 2 * time.Second
)

// dshBackend drives the Multica DSH bundle over its versioned JSONL
// stdio protocol. The adapter is intentionally independent of ACP: DSH owns
// the agent loop, session store, model catalog, tools, and MCP clients, while
// this package only translates those events into Multica's Backend contract.
type dshBackend struct {
	cfg Config
}

type dshModelSelection struct {
	Provider        string `json:"provider"`
	ID              string `json:"id"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

type dshMCPServer struct {
	Name              string            `json:"name"`
	Transport         string            `json:"transport"`
	Command           string            `json:"command,omitempty"`
	Args              []string          `json:"args,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	Cwd               string            `json:"cwd,omitempty"`
	URL               string            `json:"url,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	ToolCallTimeoutMS int               `json:"tool_call_timeout_ms,omitempty"`
}

type dshExecuteCommand struct {
	Version         int                `json:"v"`
	Type            string             `json:"type"`
	RequestID       string             `json:"request_id"`
	Cwd             string             `json:"cwd"`
	Prompt          string             `json:"prompt"`
	ResumeSessionID string             `json:"resume_session_id,omitempty"`
	Model           *dshModelSelection `json:"model,omitempty"`
	ReasoningEffort string             `json:"reasoning_effort,omitempty"`
	MCPServers      []dshMCPServer     `json:"mcp_servers"`
}

type dshCancelCommand struct {
	Version   int    `json:"v"`
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
}

type dshWireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type dshFrame struct {
	Version          int             `json:"v"`
	Type             string          `json:"type"`
	Runtime          string          `json:"runtime,omitempty"`
	ProtocolVersion  int             `json:"protocol_version,omitempty"`
	RequestID        string          `json:"request_id,omitempty"`
	SessionID        string          `json:"session_id,omitempty"`
	Resumed          bool            `json:"resumed,omitempty"`
	Content          string          `json:"content,omitempty"`
	CallID           string          `json:"call_id,omitempty"`
	Name             string          `json:"name,omitempty"`
	Arguments        string          `json:"arguments,omitempty"`
	Output           string          `json:"output,omitempty"`
	IsError          bool            `json:"is_error,omitempty"`
	Provider         string          `json:"provider,omitempty"`
	Model            string          `json:"model,omitempty"`
	InputTokens      int64           `json:"input_tokens,omitempty"`
	OutputTokens     int64           `json:"output_tokens,omitempty"`
	CacheReadTokens  int64           `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64           `json:"cache_write_tokens,omitempty"`
	Status           string          `json:"status,omitempty"`
	StopReason       string          `json:"stop_reason,omitempty"`
	ResumeRejected   bool            `json:"resume_rejected,omitempty"`
	Error            *dshWireError   `json:"error,omitempty"`
	Code             string          `json:"code,omitempty"`
	Message          string          `json:"message,omitempty"`
	Models           []dshModelFrame `json:"models,omitempty"`
}

type dshThinkingFrame struct {
	SupportedLevels []ThinkingLevel `json:"supported_levels"`
	DefaultLevel    string          `json:"default_level,omitempty"`
}

type dshModelFrame struct {
	ID       string            `json:"id"`
	Label    string            `json:"label"`
	Provider string            `json:"provider,omitempty"`
	Default  bool              `json:"default,omitempty"`
	Thinking *dshThinkingFrame `json:"thinking,omitempty"`
}

func dshLaunchArgs() []string {
	return []string{"--profile", dshProfile, "--stdio"}
}

func parseDshModelID(value string) (*dshModelSelection, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	providerPart, modelPart, ok := strings.Cut(value, "/")
	if !ok || providerPart == "" || modelPart == "" {
		return nil, fmt.Errorf("dsh model must use the provider/model ID advertised by DSH")
	}
	provider, err := url.PathUnescape(providerPart)
	if err != nil {
		return nil, fmt.Errorf("decode dsh model provider: %w", err)
	}
	if strings.TrimSpace(provider) == "" {
		return nil, errors.New("dsh model provider is empty")
	}
	model, err := url.PathUnescape(modelPart)
	if err != nil {
		return nil, fmt.Errorf("decode dsh model ID: %w", err)
	}
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("dsh model ID is empty")
	}
	return &dshModelSelection{Provider: provider, ID: model}, nil
}

func buildDshMCPServers(raw json.RawMessage, logger interface {
	Warn(string, ...any)
}) ([]dshMCPServer, error) {
	servers, err := buildACPMcpServers(raw, nil)
	if err != nil {
		return nil, err
	}
	out := make([]dshMCPServer, 0, len(servers))
	for _, rawServer := range servers {
		server, ok := rawServer.(map[string]any)
		if !ok {
			continue
		}
		name, _ := server["name"].(string)
		if command, _ := server["command"].(string); command != "" {
			entry := dshMCPServer{Name: name, Transport: "stdio", Command: command, Args: []string{}, Env: map[string]string{}}
			if args, ok := server["args"].([]string); ok {
				entry.Args = args
			}
			if pairs, ok := server["env"].([]map[string]any); ok {
				for _, pair := range pairs {
					key, _ := pair["name"].(string)
					value, _ := pair["value"].(string)
					if key != "" {
						entry.Env[key] = value
					}
				}
			}
			out = append(out, entry)
			continue
		}

		transport, _ := server["type"].(string)
		if transport == "sse" {
			return nil, fmt.Errorf("dsh MCP server %q uses SSE, but the DSH runtime supports stdio and streamable HTTP only", name)
		}
		remoteURL, _ := server["url"].(string)
		if remoteURL == "" {
			if logger != nil {
				logger.Warn("skipping invalid DSH MCP server", "name", name)
			}
			continue
		}
		entry := dshMCPServer{Name: name, Transport: "streamable-http", URL: remoteURL, Headers: map[string]string{}}
		if pairs, ok := server["headers"].([]map[string]any); ok {
			for _, pair := range pairs {
				key, _ := pair["name"].(string)
				value, _ := pair["value"].(string)
				if key != "" {
					entry.Headers[key] = value
				}
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

func (b *dshBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "dsh"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("dsh executable not found at %q: %w", execPath, err)
	}
	model, err := parseDshModelID(opts.Model)
	if err != nil {
		return nil, err
	}
	mcpServers, err := buildDshMCPServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("dsh: invalid mcp_config: %w", err)
	}

	runCtx, cancel := runContext(ctx, opts.Timeout)
	args := dshLaunchArgs()
	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	configureProcessGroup(cmd)
	cmd.Cancel = func() error { return nil }
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("dsh stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("dsh stdin pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[dsh:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start dsh: %w", err)
	}

	requestID := b.cfg.TaskID
	if requestID == "" {
		requestID = "multica-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	command := dshExecuteCommand{
		Version: dshProtocolVersion, Type: "execute", RequestID: requestID,
		Cwd: opts.Cwd, Prompt: prompt, ResumeSessionID: opts.ResumeSessionID,
		Model: model, ReasoningEffort: opts.ThinkingLevel, MCPServers: mcpServers,
	}
	var writeMu sync.Mutex
	encoder := json.NewEncoder(stdin)
	writeFrame := func(value any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return encoder.Encode(value)
	}
	if err := writeFrame(command); err != nil {
		signalProcessGroup(cmd, syscall.SIGKILL)
		_ = cmd.Wait()
		cancel()
		return nil, fmt.Errorf("send dsh execute command: %w", err)
	}

	b.cfg.Logger.Info("dsh started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)
	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	procDone := make(chan struct{})

	go func() {
		select {
		case <-procDone:
			return
		case <-runCtx.Done():
		}
		_ = writeFrame(dshCancelCommand{Version: dshProtocolVersion, Type: "cancel", RequestID: requestID})
		timer := time.NewTimer(dshCancelGrace)
		defer timer.Stop()
		select {
		case <-procDone:
			return
		case <-timer.C:
		}
		signalProcessGroup(cmd, syscall.SIGTERM)
		if !waitProcessGroupGone(cmd, dshTerminateGrace) {
			signalProcessGroup(cmd, syscall.SIGKILL)
		}
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer func() { _ = stdin.Close() }()
		started := time.Now()
		state := dshRunState{usage: make(map[string]TokenUsage)}
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var frame dshFrame
			if err := json.Unmarshal([]byte(line), &frame); err != nil {
				state.invalidFrames++
				continue
			}
			state.frameCount++
			handleDshFrame(frame, requestID, msgCh, &state)
		}
		scanErr := scanner.Err()
		if scanErr != nil {
			_ = stdout.Close()
		}
		exitErr := cmd.Wait()
		close(procDone)

		result := state.result
		if result == nil {
			result = &Result{Status: "failed", SessionID: state.sessionID, Usage: state.usage}
			switch {
			case errors.Is(runCtx.Err(), context.DeadlineExceeded):
				result.Status = "timeout"
				result.Error = fmt.Sprintf("dsh timed out after %s", opts.Timeout)
			case errors.Is(runCtx.Err(), context.Canceled):
				result.Status = "cancelled"
				result.Error = "dsh execution cancelled"
			case scanErr != nil:
				result.Error = fmt.Sprintf("read dsh event stream: %v", scanErr)
			case state.protocolError != "":
				result.Error = state.protocolError
			case !state.ready:
				result.Error = "dsh exited before the runtime protocol became ready"
			case exitErr != nil:
				result.Error = fmt.Sprintf("dsh exited with error: %v", exitErr)
			default:
				result.Error = "dsh exited without a terminal result"
			}
		}
		if result.Error != "" {
			result.Error = withAgentStderr(result.Error, "dsh", stderrBuf.Tail())
		}
		result.DurationMs = time.Since(started).Milliseconds()
		b.cfg.Logger.Info("dsh finished", "pid", cmd.Process.Pid, "status", result.Status,
			"duration", time.Since(started).Round(time.Millisecond).String(), "frames", state.frameCount,
			"invalid_frames", state.invalidFrames)
		resCh <- *result
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

type dshRunState struct {
	ready         bool
	sessionID     string
	protocolError string
	frameCount    int
	invalidFrames int
	usage         map[string]TokenUsage
	result        *Result
}

func handleDshFrame(frame dshFrame, requestID string, ch chan<- Message, state *dshRunState) {
	if frame.Version != dshProtocolVersion {
		state.protocolError = fmt.Sprintf("dsh returned unsupported protocol version %d", frame.Version)
		return
	}
	if frame.RequestID != "" && frame.RequestID != requestID {
		return
	}
	switch frame.Type {
	case "ready":
		state.ready = frame.Runtime == "dsh"
	case "session":
		state.sessionID = frame.SessionID
		trySend(ch, Message{Type: MessageStatus, Status: "running", SessionID: frame.SessionID})
	case "text":
		if frame.Content != "" {
			trySend(ch, Message{Type: MessageText, Content: frame.Content})
		}
	case "thinking":
		if frame.Content != "" {
			trySend(ch, Message{Type: MessageThinking, Content: frame.Content})
		}
	case "tool_call":
		input := map[string]any{}
		if frame.Arguments != "" && json.Unmarshal([]byte(frame.Arguments), &input) != nil {
			input = map[string]any{"raw": frame.Arguments}
		}
		trySend(ch, Message{Type: MessageToolUse, Tool: frame.Name, CallID: frame.CallID, Input: input})
	case "tool_result":
		trySend(ch, Message{Type: MessageToolResult, Tool: frame.Name, CallID: frame.CallID, Output: frame.Output})
	case "usage":
		key := frame.Model
		if frame.Provider != "" {
			key = frame.Provider + "/" + frame.Model
		}
		if key != "" {
			usage := state.usage[key]
			usage.InputTokens += frame.InputTokens
			usage.OutputTokens += frame.OutputTokens
			usage.CacheReadTokens += frame.CacheReadTokens
			usage.CacheWriteTokens += frame.CacheWriteTokens
			state.usage[key] = usage
		}
	case "protocol_error":
		state.protocolError = strings.TrimSpace(frame.Code + ": " + frame.Message)
	case "result":
		errorText := ""
		if frame.Error != nil {
			errorText = strings.TrimSpace(frame.Error.Code + ": " + frame.Error.Message)
		}
		state.result = &Result{
			Status: frame.Status, Output: frame.Output, Error: errorText,
			SessionID: frame.SessionID, Usage: state.usage, ResumeRejected: frame.ResumeRejected,
		}
		if state.result.SessionID == "" {
			state.result.SessionID = state.sessionID
		}
	}
}

func discoverDshModels(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = "dsh"
	}
	cmd := exec.CommandContext(ctx, executablePath, "--profile", dshProfile, "--list-models")
	hideAgentWindow(cmd)
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var models []Model
	scanner := newAgentStreamScanner(stdout)
	for scanner.Scan() {
		var frame dshFrame
		if json.Unmarshal(scanner.Bytes(), &frame) != nil || frame.Version != dshProtocolVersion || frame.Type != "models" {
			continue
		}
		for _, item := range frame.Models {
			model := Model{ID: item.ID, Label: item.Label, Provider: item.Provider, Default: item.Default}
			if item.Thinking != nil && len(item.Thinking.SupportedLevels) > 0 {
				model.Thinking = &ModelThinking{SupportedLevels: item.Thinking.SupportedLevels, DefaultLevel: item.Thinking.DefaultLevel}
			}
			models = append(models, model)
		}
	}
	scanErr := scanner.Err()
	exitErr := cmd.Wait()
	if scanErr != nil {
		return nil, scanErr
	}
	if exitErr != nil {
		return nil, exitErr
	}
	if len(models) == 0 {
		return nil, errors.New("dsh returned an empty model catalog")
	}
	return models, nil
}
