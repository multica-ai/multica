package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	AgentInfo       struct {
		Version string `json:"version"`
	} `json:"agentInfo"`
	Capabilities struct {
		LoadSession bool `json:"loadSession"`
		Session     struct {
			Resume *struct{} `json:"resume"`
			Load   *struct{} `json:"load"`
		} `json:"sessionCapabilities"`
	} `json:"agentCapabilities"`
}

// AntigravityACPSupportsTaskSupplement reports whether agy can use another
// session/prompt for steering. The 1.4 harness queues concurrent prompts.
func AntigravityACPSupportsTaskSupplement(version string) bool {
	detected, err := parseSemver(version)
	return err == nil && !detected.lessThan(semver{Major: 1, Minor: 5})
}

// Writes are serialized by hermesClient. If a frame stalls or breaks partway
// through, retire stdin so the next RPC cannot follow a truncated JSON line.
type antigravityACPWriter struct{ io.WriteCloser }

func (w antigravityACPWriter) Write(data []byte) (int, error) {
	timer := time.AfterFunc(5*time.Second, func() { _ = w.Close() })
	defer timer.Stop()
	n, err := w.WriteCloser.Write(data)
	if n == len(data) {
		return n, nil
	}
	_ = w.Close()
	if err == nil {
		err = io.ErrShortWrite
	}
	return n, err
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
	stream := antigravityACPStream{
		messages: make(chan Message, 256),
		tools:    make(map[string]antigravityACPTool),
		activity: make(chan struct{}, 1),
	}
	turn := antigravityACPTurn{
		stream: &stream, submissions: make(chan antigravityACPSupplement), done: make(chan struct{}),
	}
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
		stdin:                      antigravityACPWriter{stdin},
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
		acceptUpdate:       stream.acceptUpdate,
		onMessage:          stream.send,
		onUsage:            opts.OnUsage,
		selectPermission: func(raw json.RawMessage) (string, bool, bool) {
			if !streaming.Load() {
				return "", false, false
			}
			option, grant, ok := antigravityACPPermission(raw, sessionID)
			if ok {
				stream.signalActivity()
			}
			return option, grant, ok
		},
	}
	c.onPromptResponse = func(id int) {
		if int(turn.latestPromptID.Load()) == id {
			stream.replyOpen.Store(false)
			// A detached reply can follow this response. Keep the prompt's
			// answer available as the fallback if no more text arrives.
			stream.deliverable.startBlock()
			stream.textNewlines, stream.thoughtNewlines = "", ""
		}
	}
	c.onPromptDone = func(result hermesPromptResult) { turn.recordPromptUsage(c, result) }

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
		defer close(turn.done)
		result := b.runACPTurn(runCtx, setupCtx, transportCtx, c, &turn, cwd, prompt, mcpServers, opts, func(id string) {
			cancelSetup()
			sessionID = id
			streaming.Store(true)
		}, func() {
			promptSent.Store(true)
			trySend(stream.messages, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
		})
		terminalObserved.Store(true)
		turnDone <- result
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
		result.Usage = c.liveUsage()
		result.Output, _ = stream.deliverable.result()
		result.Output = strings.TrimRight(result.Output, "\r\n")
		result.DurationMs = time.Since(start).Milliseconds()
		close(stream.messages)
		results <- result
	}()

	session := &Session{Messages: stream.messages, Result: results, TerminalObserved: terminalObserved.Load, CleanupRequired: true}
	if opts.EnableTaskSupplement {
		session.Supplement = turn.supplement
		session.SupplementReady = func() bool { return runCtx.Err() == nil && turn.ready.Load() }
	}
	return session, nil
}

func (b *antigravityBackend) runACPTurn(runCtx, setupCtx, transportCtx context.Context, c *hermesClient, turn *antigravityACPTurn, cwd, prompt string, mcpServers []any,
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
	model, modelConfig, err := antigravityACPSelectModel(setupCtx, c.request, id, opts.Model, setup)
	if err != nil {
		return fail("model selection", err)
	}
	if err := antigravityACPSetThinking(setupCtx, c.request, id, opts.ThinkingLevel, modelConfig); err != nil {
		return fail("thinking level selection", err)
	}
	if err := setupCtx.Err(); err != nil {
		return fail("session setup", err)
	}
	if err := runCtx.Err(); err != nil {
		return fail("session/prompt", err)
	}

	c.usageMu.Lock()
	c.usageModel = model
	c.usageMu.Unlock()
	bound(id)
	first, err := turn.sendPrompt(c, id, prompt)
	if err != nil {
		return fail("session/prompt", err)
	}
	sent()
	turn.ready.Store(opts.EnableTaskSupplement && AntigravityACPSupportsTaskSupplement(init.AgentInfo.Version))
	defer turn.ready.Store(false)
	result = turn.wait(runCtx, transportCtx, c, id, first)
	result.SessionID = id
	return result
}

func antigravityACPSelectModel(ctx context.Context, request acpRequestFn, id, requested string, setup json.RawMessage) (string, json.RawMessage, error) {
	if _, ok := findACPConfigOption(setup, "model"); ok {
		current, _ := acpConfigOptionCurrentValue(setup, "model")
		if requested == "" || current == requested {
			return current, setup, nil
		}
		updated, err := request(ctx, "session/set_config_option", map[string]string{
			"sessionId": id, "configId": "model", "value": requested,
		})
		if err != nil {
			return "", nil, err
		}
		// Antigravity accepts legacy variant IDs such as <model>-high,
		// then reports the base model and thinking level separately.
		if selected, ok := acpConfigOptionCurrentValue(updated, "model"); ok {
			if selected != requested && !strings.HasPrefix(requested, selected+"-") {
				return "", nil, fmt.Errorf("requested model %q, ACP selected %q", requested, selected)
			}
			return selected, updated, nil
		}
		return "", updated, nil
	}

	current := extractACPCurrentModelID(setup)
	if requested == "" || current == requested {
		return current, setup, nil
	}
	if current == "" {
		return "", nil, fmt.Errorf("ACP session does not advertise model selection for %q", requested)
	}
	updated, err := request(ctx, "session/set_model", map[string]string{"sessionId": id, "modelId": requested})
	return requested, updated, err
}

func antigravityACPSetThinking(ctx context.Context, request acpRequestFn, id, level string, config json.RawMessage) error {
	if level == "" {
		return nil
	}
	option, ok := parseACPEffortOption(config)
	if !ok || !option.supports(level) {
		return fmt.Errorf("ACP session does not advertise thinking level %q", level)
	}
	if option.CurrentValue == level {
		return nil
	}
	updated, err := request(ctx, "session/set_config_option", map[string]string{
		"sessionId": id, "configId": option.ConfigID, "value": level,
	})
	if err != nil {
		return err
	}
	if current, ok := acpConfigOptionCurrentValue(updated, option.ConfigID); !ok || current != level {
		return fmt.Errorf("ACP did not confirm thinking level %q (reported %q)", level, current)
	}
	return nil
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
	tools           map[string]antigravityACPTool
	activity        chan struct{}
	activeTools     atomic.Int64
	replyOpen       atomic.Bool
	textNewlines    string
	thoughtNewlines string
}

type antigravityACPTool struct {
	active, daemon, done bool
}

func (s *antigravityACPStream) quietPeriod() time.Duration {
	if s.activeTools.Load() > 0 {
		return antigravityACPBackgroundQuiet
	}
	if s.replyOpen.Load() {
		return antigravityACPReplyQuiet
	}
	return antigravityACPSettleQuiet
}

func (s *antigravityACPStream) acceptUpdate(updateType string, data json.RawMessage) bool {
	switch updateType {
	case "tool_call", "tool_call_update":
		return s.acceptToolUpdate(updateType, data)
	case "agent_message_chunk", "agent_thought_chunk":
		s.replyOpen.Store(true)
	case "usage_update":
		s.replyOpen.Store(false)
	default:
		return true
	}
	s.signalActivity()
	return true
}

func (s *antigravityACPStream) signalActivity() {
	select {
	case s.activity <- struct{}{}:
	default:
	}
}

func (s *antigravityACPStream) acceptToolUpdate(updateType string, data json.RawMessage) bool {
	var update struct {
		ID     string          `json:"toolCallId"`
		Status string          `json:"status"`
		Input  json.RawMessage `json:"rawInput"`
	}
	if json.Unmarshal(data, &update) != nil || update.ID == "" {
		return false
	}
	tool, seen := s.tools[update.ID]
	if tool.done {
		return false
	}
	wasActive := tool.active
	var command struct {
		IsDaemon bool `json:"IsDaemon"`
	}
	_ = json.Unmarshal(update.Input, &command)
	tool.daemon = tool.daemon || command.IsDaemon
	tool.done = update.Status == "completed" || update.Status == "failed"
	tool.active = !tool.daemon && !tool.done && (tool.active ||
		updateType == "tool_call" || update.Status == "in_progress" || update.Status == "pending")
	s.tools[update.ID] = tool
	if tool.active && !wasActive {
		s.activeTools.Add(1)
	} else if wasActive && !tool.active {
		s.activeTools.Add(-1)
	}
	accepted := tool.done || updateType != "tool_call" || !seen
	if accepted || wasActive != tool.active {
		s.signalActivity()
	}
	return accepted
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
