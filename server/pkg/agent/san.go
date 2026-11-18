package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sync/atomic"
	"time"
)

const sanShutdownGrace = 2 * time.Second

type sanBackend struct {
	cfg Config
}

func (b *sanBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if opts.Model != "" {
		return nil, fmt.Errorf("san uses its locally configured model; clear the Multica model override")
	}
	if len(opts.CustomArgs) != 0 {
		return nil, fmt.Errorf("san does not support custom arguments; configure San locally instead")
	}
	cwd, err := filepath.Abs(opts.Cwd)
	if err != nil {
		return nil, fmt.Errorf("san working directory: %w", err)
	}
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("san: invalid mcp_config: %w", err)
	}

	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "san"
	}
	// Let San flush its transcript on EOF before killing an unresponsive process.
	procCtx, kill := context.WithCancel(context.WithoutCancel(ctx))
	cmd := b.cfg.commandAt(execPath).exec(procCtx, "acp")
	cmd.Dir, cmd.Env = cwd, buildEnv(b.cfg.Env)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs([]string{"acp"}, trustAgentCommandPositional(0, "acp")))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		kill()
		return nil, fmt.Errorf("san stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		kill()
		_ = stdin.Close()
		return nil, fmt.Errorf("san stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		kill()
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("san stderr pipe: %w", err)
	}
	runCtx, cancel := runContext(ctx, opts.Timeout)
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		kill()
		return nil, fmt.Errorf("start san: %w", err)
	}

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	var live atomic.Bool
	var deliverable acpDeliverableTracker
	client := &hermesClient{
		cfg:          b.cfg,
		stdin:        stdin,
		pending:      make(map[int]*pendingRPC),
		pendingTools: make(map[string]*pendingToolCall),
		// session/load replays history; only forward the current prompt's updates.
		acceptNotification: func(string) bool { return live.Load() },
		selectPermission:   selectSanPermission,
		onMessage: func(msg Message) {
			deliverable.observe(msg)
			trySend(msgCh, msg)
		},
	}
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			client.handleLine(scanner.Text())
		}
		_ = stdin.Close()
		if err := scanner.Err(); err != nil {
			client.closeAllPending(fmt.Errorf("read San ACP stream: %w", err))
		} else {
			client.closeAllPending(io.EOF)
		}
	}()
	stderrTail := newStderrTail(newLogWriter(b.cfg.Logger, "[san:stderr] "), 0)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrTail, stderr)
	}()
	stopOnCancel := context.AfterFunc(runCtx, func() { _ = stdin.Close() })

	go func() {
		defer close(msgCh)
		defer close(resCh)
		defer cancel()
		defer stopOnCancel()
		started := time.Now()
		result := b.runTurn(runCtx, client, cwd, mcpServers, prompt, opts, &live, msgCh)
		_ = stdin.Close()
		waitForHermesPipeDrain(readerDone, stderrDone, sanShutdownGrace)
		kill()
		_ = cmd.Wait()
		<-readerDone
		<-stderrDone
		releaseProcessGroup(cmd)
		result.Output, _ = deliverable.result()
		if result.Status == "failed" {
			result.Error = withAgentStderr(sanitizeAgentDiagnostic(result.Error), "san", stderrTail.Tail())
		}
		result.DurationMs = time.Since(started).Milliseconds()
		resCh <- result
	}()

	return &Session{Messages: msgCh, Result: resCh, CleanupRequired: true}, nil
}

func (b *sanBackend) runTurn(ctx context.Context, client *hermesClient, cwd string, mcpServers []any, prompt string, opts ExecOptions, live *atomic.Bool, messages chan Message) Result {
	var result Result
	fail := func(method string, err error) Result {
		status, detail, rejected := classifyACPResumeFailure(ctx, "san", method, err, opts.Timeout, nil)
		result.Status, result.Error = status, detail
		result.ResumeRejected = method == "session/load" && rejected
		return result
	}
	init, err := client.request(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientInfo":         map[string]string{"name": "multica-agent-sdk", "version": "0.2.0"},
		"clientCapabilities": map[string]any{},
	})
	if err != nil {
		return fail("initialize", err)
	}
	var handshake struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			LoadSession bool `json:"loadSession"`
		} `json:"agentCapabilities"`
	}
	if json.Unmarshal(init, &handshake) != nil || handshake.ProtocolVersion != 1 {
		return fail("initialize", fmt.Errorf("expected ACP protocol version 1"))
	}
	params := map[string]any{
		"cwd":        cwd,
		"mcpServers": filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(init), "san", b.cfg),
	}
	if opts.ResumeSessionID != "" {
		if !handshake.AgentCapabilities.LoadSession {
			return fail("session/load", fmt.Errorf("San does not advertise session loading"))
		}
		params["sessionId"] = opts.ResumeSessionID
		if _, err = client.request(ctx, "session/load", params); err != nil {
			return fail("session/load", err)
		}
		result.SessionID = opts.ResumeSessionID
	} else {
		created, err := client.request(ctx, "session/new", params)
		if err != nil {
			return fail("session/new", err)
		}
		result.SessionID = extractACPSessionID(created)
		if result.SessionID == "" {
			return fail("session/new", fmt.Errorf("missing session ID"))
		}
	}
	trySend(messages, Message{Type: MessageStatus, Status: "running", SessionID: result.SessionID})
	if opts.SystemPrompt != "" {
		prompt = opts.SystemPrompt + "\n\n---\n\n" + prompt
	}
	live.Store(true)
	raw, err := client.request(ctx, "session/prompt", map[string]any{
		"sessionId": result.SessionID,
		"prompt":    []map[string]string{{"type": "text", "text": prompt}},
	})
	if err != nil {
		return fail("session/prompt", err)
	}
	var response struct {
		StopReason string `json:"stopReason"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return fail("session/prompt", err)
	}
	switch response.StopReason {
	case "end_turn":
		result.Status = "completed"
	case "cancelled":
		result.Status, result.Error = "aborted", "san cancelled the prompt"
	default:
		return fail("session/prompt", fmt.Errorf("unexpected stop reason %q", response.StopReason))
	}
	return result
}

// Requests that San's local policy leaves to a human cannot be approved by the daemon.
func selectSanPermission(params json.RawMessage) (string, bool, bool) {
	var request struct {
		Options []acpPermissionOption `json:"options"`
	}
	if json.Unmarshal(params, &request) == nil {
		for _, opt := range request.Options {
			if opt.Kind == "reject_once" && opt.OptionID != "" {
				return opt.OptionID, false, true
			}
		}
	}
	return "", false, false
}
