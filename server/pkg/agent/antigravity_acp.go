package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	antigravityACPArg          = "--multica-antigravity-acp"
	antigravityACPSetupTimeout = 30 * time.Second
	antigravityACPCancelGrace  = 3 * time.Second
	antigravityACPExitGrace    = 3 * time.Second
)

// AntigravityUsesACP selects the transport explicitly so existing print-mode
// arguments and workspace hook behavior remain available during migration.
func AntigravityUsesACP(argLists ...[]string) bool {
	for _, args := range argLists {
		if slices.ContainsFunc(args, isAntigravityACPArg) {
			return true
		}
	}
	return false
}

func isAntigravityACPArg(arg string) bool {
	return unshellQuoteArg(arg) == antigravityACPArg
}

var antigravityACPBlockedArgs = map[string]blockedArgMode{
	antigravityACPArg:          blockedStandalone,
	"acp":                      blockedStandalone,
	"--login":                  blockedStandalone,
	"--worker":                 blockedStandalone,
	"--help":                   blockedStandalone,
	"-h":                       blockedStandalone,
	"--disable-slash-commands": blockedStandalone,
	"--effort":                 blockedWithValue,
}

type antigravityACPInitialize struct {
	ProtocolVersion int `json:"protocolVersion"`
	Capabilities    struct {
		LoadSession bool `json:"loadSession"`
		Session     struct {
			Resume *struct{} `json:"resume"`
			Load   *struct{} `json:"load"`
		} `json:"sessionCapabilities"`
	} `json:"agentCapabilities"`
}

func (b *antigravityBackend) executeACP(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	executable := b.cfg.ExecutablePath
	if executable == "" {
		executable = "agy"
	}
	if _, err := exec.LookPath(executable); err != nil {
		return nil, fmt.Errorf("agy executable not found at %q: %w", executable, err)
	}
	cwd := opts.Cwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("agy acp work directory: %w", err)
		}
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("agy acp work directory: %w", err)
	}
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("agy acp: invalid mcp_config: %w", err)
	}
	if opts.SkillInvocation != nil {
		prompt = opts.SkillInvocation.FallbackPrompt()
	}

	args := []string{"acp"}
	for _, raw := range [][]string{opts.ExtraArgs, opts.CustomArgs} {
		// The selector belongs to Multica and is never forwarded to agy.
		raw = slices.DeleteFunc(slices.Clone(raw), isAntigravityACPArg)
		raw = filterCustomArgs(raw, antigravityBlockedArgs, b.cfg.Logger)
		args = append(args, filterCustomArgs(raw, antigravityACPBlockedArgs, b.cfg.Logger)...)
	}
	cfg := b.cfg
	cfg.LaunchPrefix = filterCustomArgs(cfg.LaunchPrefix, antigravityACPBlockedArgs, cfg.Logger)
	cmd := cfg.commandAt(executable).exec(context.Background(), args...)
	hideAgentWindow(cmd)
	cmd.Dir = cwd
	cmd.Env = buildEnv(cfg.Env)
	cmd.WaitDelay = antigravityACPExitGrace
	cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args, trustAgentCommandPositional(0, "acp")))

	cmd.Stderr = newLogWriter(cfg.Logger, "[agy acp:stderr] ")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("agy acp stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = stdout.Close()
		return nil, fmt.Errorf("agy acp stdin pipe: %w", err)
	}
	if err := startOwnedProcessTree(cmd, cfg.Logger); err != nil {
		return nil, fmt.Errorf("start agy acp: %w", err)
	}

	runCtx, cancelRun := runContext(ctx, opts.Timeout)
	stream := antigravityACPStream{messages: make(chan Message, 256), tools: make(map[string]bool)}
	results := make(chan Result, 1)
	transportCtx, disconnect := context.WithCancel(context.Background())
	setupTimeout := opts.HandshakeTimeout
	if setupTimeout <= 0 {
		setupTimeout = antigravityACPSetupTimeout
	}
	setupCtx, cancelSetup := context.WithTimeout(transportCtx, setupTimeout)
	// Context cancellation alone cannot release a write to a full pipe.
	context.AfterFunc(setupCtx, func() {
		if setupCtx.Err() == context.DeadlineExceeded {
			_ = stdin.Close()
		}
	})
	var sessionID string
	var streaming, promptSent, terminalObserved atomic.Bool
	c := &hermesClient{
		cfg:                        cfg,
		stdin:                      stdin,
		pending:                    make(map[int]*pendingRPC),
		toolName:                   antigravityACPToolName,
		toolStartCanComplete:       true,
		toolStartCarriesFinalInput: true,
		acceptSessionUpdate: func(id string) bool {
			return streaming.Load() && id == sessionID
		},
		// The response to our session/prompt is the terminal boundary. Ignore
		// dialect-specific turn_end notifications on the shared transport.
		acceptNotification: func(updateType string) bool { return updateType != "turn_end" },
		acceptUpdate:       stream.acceptToolUpdate,
		onMessage:          stream.send,
		selectPermission: func(raw json.RawMessage) (string, bool, bool) {
			if !streaming.Load() {
				return "", false, false
			}
			return antigravityACPPermission(raw, sessionID)
		},
		onPromptDone: func(hermesPromptResult) {
			streaming.Store(false)
			terminalObserved.Store(true)
		},
	}

	processDone := make(chan error, 1)
	go func() {
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				c.handleLine(line)
			}
		}
		readErr := scanner.Err()
		if readErr == nil {
			readErr = errors.New("ACP connection closed")
		}
		c.closeAllPending(readErr)
		disconnect()
		processDone <- cmd.Wait()
	}()

	start := time.Now()
	trySend(stream.messages, Message{Type: MessageStatus, Status: "running"})
	turnDone := make(chan Result, 1)
	go func() {
		defer cancelSetup()
		turnDone <- b.runACPTurn(runCtx, setupCtx, transportCtx, c, cwd, prompt, mcpServers, opts, func(id string) {
			cancelSetup()
			sessionID = id
			streaming.Store(true)
		}, func() {
			promptSent.Store(true)
			trySend(stream.messages, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
		})
	}()

	go func() {
		defer cancelRun()
		defer disconnect()
		defer close(results)
		var result Result
		var finished, interrupted bool
		var cancelDone chan struct{}
		select {
		case result = <-turnDone:
			finished = true
		case <-runCtx.Done():
			interrupted = !terminalObserved.Load()
			if promptSent.Load() && interrupted {
				cancelDone = make(chan struct{})
				go func() {
					defer close(cancelDone)
					if err := c.notify("session/cancel", map[string]string{"sessionId": sessionID}); err != nil {
						cfg.Logger.Debug("agy acp cancellation could not be sent", "error", err)
					}
				}()
				timer := time.NewTimer(antigravityACPCancelGrace)
				select {
				case result = <-turnDone:
					finished = true
				case <-timer.C:
				}
				timer.Stop()
			}
		}

		terminalObserved.Store(true)
		streaming.Store(false)
		_ = stdin.Close()
		forceStop := interrupted && !finished
		var exitErr error
		if !forceStop {
			timer := time.NewTimer(antigravityACPExitGrace)
			select {
			case exitErr = <-processDone:
			case <-timer.C:
				forceStop = true
			}
			timer.Stop()
		}
		if forceStop {
			signalProcessGroup(cmd, syscall.SIGKILL)
			_ = stdout.Close()
			exitErr = <-processDone
		} else {
			signalProcessGroup(cmd, syscall.SIGKILL)
		}
		releaseProcessGroup(cmd)
		if !finished {
			result = <-turnDone
		}
		if cancelDone != nil {
			<-cancelDone
		}

		if interrupted || (runCtx.Err() != nil && result.Status != "completed") {
			result.Status, result.Error, result.ResumeRejected = classifyACPResumeFailure(
				runCtx, "agy acp", "execution", runCtx.Err(), opts.Timeout, nil)
		}
		if exitErr != nil && result.Status == "failed" {
			cfg.Logger.Debug("agy acp process exited", "error", exitErr)
		}
		result.Output, _ = stream.deliverable.result()
		result.Output = strings.TrimRight(result.Output, "\r\n")
		result.DurationMs = time.Since(start).Milliseconds()
		close(stream.messages)
		results <- result
	}()

	// agy 1.5 steers with a second session/prompt. A prompt that races the
	// previous end_turn can also run as a new turn, with the same response.
	// Leave Supplement unset so Multica can schedule an explicit follow-up.
	// https://antigravity.google/docs/ide/extensions/acp/
	return &Session{Messages: stream.messages, Result: results, TerminalObserved: terminalObserved.Load, CleanupRequired: true}, nil
}

func (b *antigravityBackend) runACPTurn(runCtx, setupCtx, transportCtx context.Context, c *hermesClient, cwd, prompt string, mcpServers []any,
	opts ExecOptions, bound func(string), sent func()) Result {
	result := Result{SessionID: opts.ResumeSessionID}
	fail := func(method string, err error) Result {
		if setupCtx.Err() == context.DeadlineExceeded {
			err = setupCtx.Err()
		}
		result.Status, result.Error, _ = classifyACPResumeFailure(runCtx, "agy acp", method, err, opts.Timeout, nil)
		if transportCtx.Err() != nil && errors.Is(err, context.Canceled) && runCtx.Err() == nil {
			result.Error = "agy acp connection closed during " + method
		}
		return result
	}
	initialize, err := c.request(setupCtx, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientInfo": map[string]string{
			"name": "multica", "title": "Multica", "version": b.cfg.DaemonVersion,
		},
		"clientCapabilities": map[string]any{},
	})
	if err != nil {
		return fail("initialize (install an agy release with the acp subcommand)", err)
	}
	var init antigravityACPInitialize
	if err := json.Unmarshal(initialize, &init); err != nil || init.ProtocolVersion != 1 {
		return fail("initialize", fmt.Errorf("expected ACP protocol version 1"))
	}

	method := "session/new"
	params := map[string]any{
		"cwd":        cwd,
		"mcpServers": filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initialize), "antigravity", b.cfg),
	}
	if opts.ResumeSessionID != "" {
		params["sessionId"] = opts.ResumeSessionID
		switch {
		case init.Capabilities.Session.Resume != nil:
			method = "session/resume"
		case init.Capabilities.LoadSession || init.Capabilities.Session.Load != nil:
			method = "session/load"
		default:
			return fail("resume", errors.New("ACP server does not advertise session/resume or session/load"))
		}
	}
	setup, err := c.request(setupCtx, method, params)
	if err != nil {
		var rpcErr *acpRPCError
		if errors.As(err, &rpcErr) && rpcErr.Code == -32000 && strings.Contains(strings.ToLower(err.Error()), "auth") {
			return fail(method, fmt.Errorf("%w (sign in to Antigravity CLI with agy acp --login)", err))
		}
		if opts.ResumeSessionID != "" {
			result.Status, result.Error, result.ResumeRejected = classifyACPResumeFailure(
				runCtx, "agy acp", method, err, opts.Timeout, b.cfg.Logger)
			if result.Status == "failed" && rpcErr != nil && strings.EqualFold(rpcErr.Message, "trajectory not found") {
				result.ResumeRejected = true
			}
			return result
		}
		return fail(method, err)
	}

	id := extractACPSessionID(setup)
	if opts.ResumeSessionID != "" {
		var changed bool
		id, changed = resolveResumedSessionID(opts.ResumeSessionID, setup)
		if changed {
			result.Status, result.Error, result.ResumeRejected = "failed", "agy acp returned a different session on resume", true
			return result
		}
	}
	if id == "" {
		return fail(method, errors.New("ACP response did not contain a session ID"))
	}
	model, err := antigravityACPSelectModel(setupCtx, c.request, id, opts.Model, setup)
	if err != nil {
		return fail("model selection", err)
	}
	if err := setupCtx.Err(); err != nil {
		return fail("session setup", err)
	}
	if err := runCtx.Err(); err != nil {
		return fail("session/prompt", err)
	}

	bound(id)
	response, err := c.requestAndNotifySent(transportCtx, "session/prompt", map[string]any{
		"sessionId": id,
		"prompt":    []map[string]string{{"type": "text", "text": prompt}},
	}, func() {
		result.SessionID = id
		sent()
	})
	if err != nil {
		return fail("session/prompt", err)
	}

	pr, err := parseACPPromptResult(response)
	if err != nil {
		return fail("session/prompt", err)
	}
	c.mergeUsage(pr.usage)
	usage := c.accumulatedUsage()
	if acpUsagePresent(usage) {
		if pr.modelID != "" {
			model = pr.modelID
		}
		if model == "" {
			model = "unknown"
		}
		result.Usage = map[string]TokenUsage{model: usage}
	}
	switch pr.stopReason {
	case "end_turn":
		result.Status = "completed"
	case "cancelled":
		result.Status, result.Error = "aborted", "agy acp cancelled the turn"
	case "":
		return fail("session/prompt", errors.New("ACP response did not contain a stop reason"))
	default:
		return fail("session/prompt", fmt.Errorf("ACP stop reason %q", pr.stopReason))
	}
	return result
}

func antigravityACPSelectModel(ctx context.Context, request acpRequestFn, id, requested string, setup json.RawMessage) (string, error) {
	if _, ok := findACPConfigOption(setup, "model"); ok {
		current, _ := acpConfigOptionCurrentValue(setup, "model")
		if requested == "" || current == requested {
			return current, nil
		}
		updated, err := request(ctx, "session/set_config_option", map[string]string{
			"sessionId": id, "configId": "model", "value": requested,
		})
		if err != nil {
			return "", err
		}
		// Antigravity accepts legacy variant IDs such as <model>-high,
		// then reports the base model and thinking level separately.
		if selected, ok := acpConfigOptionCurrentValue(updated, "model"); ok {
			if selected != requested && !strings.HasPrefix(requested, selected+"-") {
				return "", fmt.Errorf("requested model %q, ACP selected %q", requested, selected)
			}
			return selected, nil
		}
		return "", nil
	}

	current := extractACPCurrentModelID(setup)
	if requested == "" || current == requested {
		return current, nil
	}
	if current == "" {
		return "", fmt.Errorf("ACP session does not advertise model selection for %q", requested)
	}
	_, err := request(ctx, "session/set_model", map[string]string{"sessionId": id, "modelId": requested})
	return requested, err
}

func antigravityACPPermission(raw json.RawMessage, sessionID string) (string, bool, bool) {
	var p struct {
		SessionID string `json:"sessionId"`
		ToolCall  struct {
			ID string `json:"toolCallId"`
		} `json:"toolCall"`
	}
	if sessionID == "" || json.Unmarshal(raw, &p) != nil || p.SessionID != sessionID || p.ToolCall.ID == "" ||
		strings.HasPrefix(p.ToolCall.ID, "interaction_") {
		return "", false, false
	}
	return selectACPPermissionOption(raw)
}

func antigravityACPToolName(name, title, kind string) string {
	if name != "" {
		return name
	}
	if tool, ok := strings.CutPrefix(title, "Run "); ok && strings.Contains(tool, "_") {
		return tool
	}
	return kimiToolNameFromTitle(hermesToolNameFromTitle(title, kind))
}

// All stream state lives on the ACP reader. Text frames carry deltas without
// event IDs. Identical deltas can be intentional, unlike repeated tool IDs.
type antigravityACPStream struct {
	messages        chan Message
	deliverable     acpDeliverableTracker
	tools           map[string]bool // true once a tool has finished
	textNewlines    string
	thoughtNewlines string
}

func (s *antigravityACPStream) acceptToolUpdate(updateType string, data json.RawMessage) bool {
	if updateType != "tool_call" && updateType != "tool_call_update" {
		return true
	}
	var update struct {
		ID     string `json:"toolCallId"`
		Status string `json:"status"`
	}
	if json.Unmarshal(data, &update) != nil || update.ID == "" {
		return false
	}
	done, seen := s.tools[update.ID]
	if done {
		return false
	}
	if update.Status == "completed" || update.Status == "failed" {
		s.tools[update.ID] = true
		return true
	}
	if updateType == "tool_call" {
		s.tools[update.ID] = false
		return !seen
	}
	return true
}

func (s *antigravityACPStream) send(msg Message) {
	switch msg.Type {
	case MessageText:
		msg.Content = trimACPChunkTail(msg.Content, &s.textNewlines)
	case MessageThinking:
		msg.Content = trimACPChunkTail(msg.Content, &s.thoughtNewlines)
	case MessageToolUse:
		msg.Input = antigravityToolInput(msg.Input)
		s.textNewlines, s.thoughtNewlines = "", ""
	case MessageToolResult:
		s.textNewlines, s.thoughtNewlines = "", ""
	}
	if (msg.Type == MessageText || msg.Type == MessageThinking) && msg.Content == "" {
		return
	}
	s.deliverable.observe(msg)
	trySend(s.messages, msg)
}

func trimACPChunkTail(delta string, trailing *string) string {
	content := *trailing + delta
	text := strings.TrimRight(content, "\r\n")
	*trailing = content[len(text):]
	return text
}
