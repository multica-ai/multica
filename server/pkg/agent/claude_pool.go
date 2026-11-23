package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type claudeWarmTurn struct {
	backend       *claudeBackend
	runCtx        context.Context
	cancel        context.CancelFunc
	opts          ExecOptions
	msgCh         chan Message
	coalescer     *claudeStreamCoalescer
	inputWriter   *claudeInputWriter
	supplements   *claudeSupplementSession
	initializer   *claudeSupplementSession
	usageSnapshot *claudeUsageSnapshot
	baselineUsage map[string]TokenUsage
	rateLimits    *rateLimitTracker
	controlWrites sync.WaitGroup
	controlErrors chan error
	resultCh      chan struct{}
	resultOnce    sync.Once

	mu                       sync.Mutex
	lastAssistantText        string
	finalResultText          string
	sawResult                bool
	resultIsError            bool
	terminalReasonError      string
	sessionID                string
	lastUsageResult          *claudeSDKMessage
	lastRawModelUsage        map[string]TokenUsage
	sawAsyncLaunch           bool
	usage                    map[string]TokenUsage
	seenUsage                map[string]struct{}
	eventCount               int
	invalidEventCount        int
	assistantEventCount      int
	toolUseCount             int
	unreadableAssistantCount int
}

func (t *claudeWarmTurn) handleMessage(msg claudeSDKMessage) {
	t.mu.Lock()
	t.eventCount++
	switch msg.Type {
	case "stream_event":
		if msg.ParentToolUseID == "" {
			t.coalescer.handleEvent(msg.Event)
		}
		t.mu.Unlock()
	case "assistant":
		t.assistantEventCount++
		turn := t.backend.handleAssistantWithStream(msg, t.msgCh, t.usage, t.seenUsage, t.coalescer)
		t.toolUseCount += turn.toolUses
		if !turn.understood {
			t.unreadableAssistantCount++
		}
		t.lastAssistantText = turn.resolveFallback(t.lastAssistantText)
		t.mu.Unlock()
	case "user":
		t.coalescer.flushAll()
		if t.backend.handleUser(msg, t.msgCh) {
			t.sawAsyncLaunch = true
		}
		t.mu.Unlock()
	case "rate_limit_event":
		rl := t.rateLimits
		t.mu.Unlock()
		if update, ok := parseClaudeRateLimitEvent(msg.RateLimitInfo); ok {
			rl.apply(update)
		}
	case "system":
		if msg.SessionID != "" {
			t.sessionID = msg.SessionID
		}
		sid := t.sessionID
		t.mu.Unlock()
		trySend(t.msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sid})
	case "result":
		t.coalescer.flushAll()
		t.sawResult = true
		t.finalResultText = msg.ResultText
		t.resultIsError = msg.IsError
		t.terminalReasonError = claudeTerminalReasonFailure(msg.TerminalReason, msg.ResultText)
		if msg.SessionID != "" {
			t.sessionID = msg.SessionID
		}
		if len(msg.ModelUsage) > 0 {
			t.lastRawModelUsage = claudeModelUsage(msg.ModelUsage)
		}
		unverifiableBaseline := t.usageSnapshot == nil && t.baselineUsage == nil && t.opts.ResumeSessionID != ""
		baseline := t.baselineUsage
		if baseline == nil && t.usageSnapshot != nil {
			baseline = t.usageSnapshot.baseline
		}
		if resultUsage, authoritative := claudeResultUsageSince(msg, t.opts.Model, baseline); authoritative {
			usageMsg := msg
			t.lastUsageResult = &usageMsg
			if !unverifiableBaseline {
				t.usage = resultUsage
			}
		}
		t.mu.Unlock()
		t.resultOnce.Do(func() { close(t.resultCh) })
	case "log":
		t.mu.Unlock()
		if msg.Log != nil {
			trySend(t.msgCh, Message{
				Type:    MessageLog,
				Level:   msg.Log.Level,
				Content: msg.Log.Message,
			})
		}
	case "control_request":
		supplements := t.supplements
		t.mu.Unlock()
		var reply func(io.Writer) error
		if supplements != nil {
			reply, _ = supplements.prepareHook(msg)
		}
		t.controlWrites.Add(1)
		go func(msg claudeSDKMessage, reply func(io.Writer) error) {
			defer t.controlWrites.Done()
			if reply == nil {
				t.backend.handleControlRequest(msg, t.inputWriter)
				return
			}
			if err := reply(t.inputWriter); err != nil {
				select {
				case t.controlErrors <- err:
				default:
				}
				t.cancel()
			}
		}(msg, reply)
	case "control_response":
		init := t.initializer
		t.mu.Unlock()
		if init != nil {
			init.handleResponse(msg.Response)
		}
	default:
		t.mu.Unlock()
	}
}

func (t *claudeWarmTurn) noteInvalidEvent() {
	t.mu.Lock()
	t.invalidEventCount++
	t.mu.Unlock()
}

type warmClaudeProcess struct {
	fingerprint              string
	sessionID                string
	cmd                      *exec.Cmd
	stdin                    io.WriteCloser
	inputWriter              *claudeInputWriter
	stdout                   io.ReadCloser
	stderrBuf                *stderrTail
	procDone                 chan struct{}
	mcpConfigPath            string
	sessionDir               string
	taskEnvFile              string
	sessionTempDir           string
	logger                   *slog.Logger
	initialized              bool
	initializedCommands      map[string]bool
	lastCumulativeModelUsage map[string]TokenUsage

	turnMu     sync.Mutex
	activeTurn *claudeWarmTurn
	scanErr    error
	exitErr    error

	closed    atomic.Bool
	closeOnce sync.Once
}

func (p *warmClaudeProcess) Fingerprint() string { return p.fingerprint }

func (p *warmClaudeProcess) Alive() bool {
	if p.closed.Load() {
		return false
	}
	select {
	case <-p.procDone:
		return false
	default:
		return true
	}
}

func (p *warmClaudeProcess) setActiveTurn(t *claudeWarmTurn) {
	p.turnMu.Lock()
	p.activeTurn = t
	p.turnMu.Unlock()
}

func (p *warmClaudeProcess) getErrors() (scanErr, exitErr error) {
	p.turnMu.Lock()
	defer p.turnMu.Unlock()
	return p.scanErr, p.exitErr
}

func (p *warmClaudeProcess) Close() bool {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		_ = ClearTaskEnvFile(p.taskEnvFile)
		_ = p.stdin.Close()

		// Give the CLI a brief window to exit cleanly on stdin EOF before
		// signalling the process group, mirroring claudeBackend.Execute.
		select {
		case <-p.procDone:
		case <-time.After(1500 * time.Millisecond):
			if p.cmd.Process != nil {
				signalProcessGroup(p.cmd, syscall.SIGTERM)
				if !waitProcessGroupGone(p.cmd, claudeTerminateGrace()) {
					signalProcessGroup(p.cmd, syscall.SIGKILL)
				}
			}
			_ = p.stdout.Close()
			<-p.procDone
		}

		if p.mcpConfigPath != "" {
			cleanupMcpConfigTemp(p.mcpConfigPath)
		}
		if p.sessionDir != "" {
			_ = os.RemoveAll(p.sessionDir)
		}
	})
	return true
}

func (p *warmClaudeProcess) terminateImmediately() {
	p.closed.Store(true)
	_ = p.stdin.Close()
	if p.cmd.Process != nil {
		signalProcessGroup(p.cmd, syscall.SIGTERM)
		if !waitProcessGroupGone(p.cmd, claudeTerminateGrace()) {
			signalProcessGroup(p.cmd, syscall.SIGKILL)
		}
	}
	_ = p.stdout.Close()
}

func (b *claudeBackend) executeWarm(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "claude"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("claude executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	// Build fingerprint args without --resume so a Turn-1 spawned process and
	// a Turn-2 resumed request for the same session share an identical key.
	optsForFP := opts
	optsForFP.ResumeSessionID = ""
	argsForFP := buildClaudeArgs(optsForFP, b.cfg.Logger)
	var configFiles []string
	if opts.ClaudeSettingsPath != "" {
		configFiles = append(configFiles, opts.ClaudeSettingsPath)
	}
	fingerprint := computeWarmSessionFingerprint(WarmSessionFingerprintInput{
		Provider:      "claude",
		Executable:    execPath,
		LaunchPrefix:  b.cfg.LaunchPrefix,
		Args:          argsForFP,
		Cwd:           opts.Cwd,
		Model:         opts.Model,
		ThinkingLevel: opts.ThinkingLevel,
		McpConfig:     opts.McpConfig,
		ConfigFiles:   configFiles,
		BriefHash:     opts.WarmPoolBriefHash,
		Env:           b.cfg.Env,
		ExtraFlags:    []string{fmt.Sprintf("supplement=%t", opts.EnableTaskSupplement)},
	})

	var warmProc *warmClaudeProcess
	warmHit := false
	if opts.ResumeSessionID != "" {
		if acquired := b.cfg.WarmPool.Acquire("claude", opts.ResumeSessionID, fingerprint); acquired != nil {
			if candidate, ok := acquired.(*warmClaudeProcess); ok {
				if err := WriteTaskEnvFile(candidate.taskEnvFile, b.cfg.Env); err != nil {
					b.cfg.Logger.Warn("warm claude: failed to refresh task env overlay; evicting", "error", err)
					candidate.Close()
				} else {
					warmProc = candidate
					warmHit = true
				}
			} else {
				acquired.Close()
			}
		}
	}

	var usageSnapshot *claudeUsageSnapshot
	if !warmHit {
		var err error
		warmProc, usageSnapshot, err = b.spawnWarmClaudeProcess(execPath, opts, fingerprint)
		if err != nil {
			cancel()
			return nil, err
		}
	}

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var supplements *claudeSupplementSession
	if opts.EnableTaskSupplement {
		supplements = newClaudeSupplementSession(runCtx)
	}
	initializer := supplements
	if initializer == nil && opts.SkillInvocation != nil {
		initializer = newClaudeSupplementSession(runCtx)
	}

	var baselineUsage map[string]TokenUsage
	if warmHit && len(warmProc.lastCumulativeModelUsage) > 0 {
		baselineUsage = make(map[string]TokenUsage, len(warmProc.lastCumulativeModelUsage))
		for k, v := range warmProc.lastCumulativeModelUsage {
			baselineUsage[k] = v
		}
	}

	turn := &claudeWarmTurn{
		backend:       b,
		runCtx:        runCtx,
		cancel:        cancel,
		opts:          opts,
		msgCh:         msgCh,
		coalescer:     newClaudeStreamCoalescer(msgCh, nil),
		inputWriter:   warmProc.inputWriter,
		supplements:   supplements,
		initializer:   initializer,
		usageSnapshot: usageSnapshot,
		baselineUsage: baselineUsage,
		rateLimits:    newRateLimitTracker(opts.OnRateLimits),
		controlErrors: make(chan error, 1),
		resultCh:      make(chan struct{}),
		usage:         make(map[string]TokenUsage),
		seenUsage:     make(map[string]struct{}),
	}
	if warmHit {
		turn.sessionID = warmProc.sessionID
	}
	warmProc.setActiveTurn(turn)

	if warmHit {
		b.cfg.Logger.Info("claude warm session reused",
			"pid", warmProc.cmd.Process.Pid,
			"session_id", warmProc.sessionID,
			"cwd", opts.Cwd,
			"model", opts.Model,
		)
		trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: warmProc.sessionID})
	}

	writeDone := make(chan error, 1)
	go func() {
		var commands map[string]bool
		if warmHit {
			if warmProc.initialized && !slashCommandsDisabled(b.cfg.LaunchPrefix, opts.ExtraArgs, opts.CustomArgs) {
				commands = warmProc.initializedCommands
			}
		} else if initializer != nil {
			if initErr := initializer.initialize(warmProc.inputWriter, opts.HandshakeTimeout, opts.EnableTaskSupplement); initErr != nil {
				b.cfg.Logger.Warn("Claude initialization unavailable; continuing with text input", "error", initErr)
				initializer.end()
			} else {
				warmProc.initialized = true
				warmProc.initializedCommands = initializer.availableCommands()
				if !slashCommandsDisabled(b.cfg.LaunchPrefix, opts.ExtraArgs, opts.CustomArgs) {
					commands = warmProc.initializedCommands
				}
			}
		}
		blocks := []string{prompt}
		if opts.SkillInvocation != nil {
			blocks = opts.SkillInvocation.claudeTextBlocks(commands)
		}
		err := writeClaudeInput(warmProc.inputWriter, blocks...)
		if err != nil {
			if supplements != nil {
				cancel()
			}
		}
		writeDone <- err
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()

		// Wait for either the turn's result event, process exit, or context
		// cancellation/timeout.
		var cancelledOrTimedOut bool
		select {
		case <-turn.resultCh:
		case <-warmProc.procDone:
		case <-runCtx.Done():
			cancelledOrTimedOut = true
			warmProc.terminateImmediately()
			<-warmProc.procDone
		}

		if initializer != nil {
			initializer.end()
		}

		turn.mu.Lock()
		turn.coalescer.flushAll()
		sawResult := turn.sawResult
		resultIsError := turn.resultIsError
		terminalReasonError := turn.terminalReasonError
		sawAsyncLaunch := turn.sawAsyncLaunch
		sessionID := turn.sessionID
		lastAssistantText := turn.lastAssistantText
		finalResultText := turn.finalResultText
		lastUsageResult := turn.lastUsageResult
		lastRawModelUsage := turn.lastRawModelUsage
		usage := turn.usage
		eventCount := turn.eventCount
		invalidEventCount := turn.invalidEventCount
		assistantEventCount := turn.assistantEventCount
		toolUseCount := turn.toolUseCount
		unreadableAssistantCount := turn.unreadableAssistantCount
		turn.mu.Unlock()

		turn.controlWrites.Wait()
		writeErr := <-writeDone
		if writeErr == nil {
			select {
			case writeErr = <-turn.controlErrors:
			default:
			}
		}

		if supplements != nil && writeErr != nil && ctx.Err() == nil && terminalReasonError == "" && !errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			terminalReasonError = fmt.Sprintf("claude input/control protocol failed: %v", writeErr)
		}

		// Detach the turn before deciding to park or close.
		warmProc.setActiveTurn(nil)
		_ = ClearTaskEnvFile(warmProc.taskEnvFile)

		scanErr, exitErr := warmProc.getErrors()
		canPark := sawResult &&
			!resultIsError &&
			terminalReasonError == "" &&
			!sawAsyncLaunch &&
			!cancelledOrTimedOut &&
			writeErr == nil &&
			scanErr == nil &&
			runCtx.Err() == nil &&
			sessionID != "" &&
			warmProc.Alive()

		if canPark {
			warmProc.sessionID = sessionID
			if len(lastRawModelUsage) > 0 {
				warmProc.lastCumulativeModelUsage = lastRawModelUsage
			}
			b.cfg.WarmPool.Park("claude", sessionID, warmProc)
		} else {
			// Close stdin immediately so a persistent stream-json CLI waiting for
			// the next user frame exits on EOF instead of blocking for the error
			// result grace window.
			warmProc.Close()
			scanErr, exitErr = warmProc.getErrors()
		}

		duration := time.Since(startTime)

		if !sawResult && usageSnapshot != nil {
			appendedUsage, found, snapshotErr := usageSnapshot.appendedCostStateUsage()
			switch {
			case snapshotErr != nil:
				b.cfg.Logger.Warn("claude final cost state unavailable; keeping stream usage fallback", "error", snapshotErr)
			case found:
				usage = subtractClaudeUsage(appendedUsage, usageSnapshot.baseline)
			}
		}

		completionGuardError := ""
		if sawAsyncLaunch {
			completionGuardError = "claude launched an async background task; Multica-managed runs require foreground execution"
		}
		finalStatus, finalOutput, finalError := finalizeStreamResult(
			"claude",
			timeout,
			runCtx.Err(),
			writeErr,
			exitErr,
			sessionID,
			streamTerminalState{
				lastAssistantText:   lastAssistantText,
				finalResultText:     finalResultText,
				sawResult:           sawResult,
				resultIsError:       resultIsError,
				scanErr:             scanErr,
				terminalReasonError: terminalReasonError,
			},
			completionGuardError,
		)

		stderrTail := warmProc.stderrBuf.Tail()
		if finalError != "" {
			finalError = withAgentStderr(finalError, "claude", stderrTail)
		}
		logStreamProtocolObservation(b.cfg.Logger, streamProtocolObservation{
			provider:                   "claude",
			cliVersion:                 b.cfg.CLIVersion,
			model:                      opts.Model,
			exitCode:                   streamProcessExitCode(exitErr),
			eventCount:                 eventCount,
			invalidEventCount:          invalidEventCount,
			assistantEventCount:        assistantEventCount,
			toolUseCount:               toolUseCount,
			sawResult:                  sawResult,
			resultIsError:              resultIsError,
			resultBytes:                len(finalResultText),
			lastAssistantBytes:         len(lastAssistantText),
			scannerError:               scanErr != nil,
			unreadableAssistantCount:   unreadableAssistantCount,
			anthropicBaseURLConfigured: strings.TrimSpace(b.cfg.Env["ANTHROPIC_BASE_URL"]) != "",
		})

		b.cfg.Logger.Info("claude finished",
			"pid", warmProc.cmd.Process.Pid,
			"warm_hit", warmHit,
			"parked", canPark,
			"status", finalStatus,
			"duration", duration.Round(time.Millisecond).String(),
		)

		resumeRejected := resumeWasRejected(opts.ResumeSessionID, sessionID, finalStatus == "failed", finalError, stderrTail)
		reportedSessionID := resolveSessionID(opts.ResumeSessionID, sessionID, finalStatus == "failed", finalError, stderrTail)
		if resumeRejected {
			if lastUsageResult != nil {
				if resultUsage, authoritative := claudeResultUsageSince(*lastUsageResult, opts.Model, nil); authoritative {
					usage = resultUsage
				}
			}
			b.cfg.Logger.Info("claude resume was rejected; dropping session id and signalling fresh-session retry",
				"requested_resume", opts.ResumeSessionID,
				"emitted_session", sessionID,
			)
		}

		resCh <- Result{
			Status:         finalStatus,
			Output:         finalOutput,
			Error:          finalError,
			DurationMs:     duration.Milliseconds(),
			SessionID:      reportedSessionID,
			Usage:          usage,
			ResumeRejected: resumeRejected,
		}
	}()

	session := &Session{Messages: msgCh, Result: resCh}
	if supplements != nil {
		session.Supplement = supplements.supplement
		session.SupplementReady = supplements.ready
	}
	return session, nil
}

func (b *claudeBackend) spawnWarmClaudeProcess(execPath string, opts ExecOptions, fingerprint string) (*warmClaudeProcess, *claudeUsageSnapshot, error) {
	sessionDir, err := os.MkdirTemp("", "multica-claude-warm-*")
	if err != nil {
		return nil, nil, fmt.Errorf("create warm claude session dir: %w", err)
	}
	sessionTempDir := filepath.Join(sessionDir, "tmp")
	if err := os.MkdirAll(sessionTempDir, 0o700); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, nil, fmt.Errorf("create warm claude temp dir: %w", err)
	}
	taskEnvFile := filepath.Join(sessionDir, "task-env.json")
	if err := WriteTaskEnvFile(taskEnvFile, b.cfg.Env); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, nil, fmt.Errorf("write warm claude task env overlay: %w", err)
	}

	args := buildClaudeArgs(opts, b.cfg.Logger)
	var mcpConfigPath string
	if hasManagedMcpConfig(opts.McpConfig) {
		path, err := writeMcpConfigToTemp(opts.McpConfig)
		if err != nil {
			_ = os.RemoveAll(sessionDir)
			return nil, nil, err
		}
		mcpConfigPath = path
		args = append(args, "--mcp-config", mcpConfigPath)
	}

	cleanupOnErr := func() {
		if mcpConfigPath != "" {
			cleanupMcpConfigTemp(mcpConfigPath)
		}
		_ = os.RemoveAll(sessionDir)
	}

	cmd := b.cfg.commandAt(execPath).exec(context.Background(), args...)
	hideAgentWindow(cmd)
	cmd.Cancel = func() error { return nil }
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args))
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(PreparePooledChildEnv(b.cfg.Env, taskEnvFile, sessionTempDir))
	if err := claudeRootSudoPreflight(args, cmd.Env); err != nil {
		cleanupOnErr()
		return nil, nil, err
	}

	var usageSnapshot *claudeUsageSnapshot
	if opts.ResumeSessionID != "" {
		snapshot, snapshotErr := captureClaudeUsageSnapshot(cmd.Env, cmd.Dir, opts.ResumeSessionID)
		if snapshotErr != nil {
			b.cfg.Logger.Warn("claude usage baseline unavailable; ignoring reported session totals, keeping per-run stream usage", "error", snapshotErr)
		} else {
			usageSnapshot = snapshot
		}
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cleanupOnErr()
		return nil, nil, fmt.Errorf("claude stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cleanupOnErr()
		return nil, nil, fmt.Errorf("claude stdin pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[claude:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		_ = stdin.Close()
		cleanupOnErr()
		return nil, nil, fmt.Errorf("start claude: %w", err)
	}

	b.cfg.Logger.Info("claude started", "warm_pool", true, "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	proc := &warmClaudeProcess{
		fingerprint:    fingerprint,
		cmd:            cmd,
		stdin:          stdin,
		inputWriter:    &claudeInputWriter{w: stdin},
		stdout:         stdout,
		stderrBuf:      stderrBuf,
		procDone:       make(chan struct{}),
		mcpConfigPath:  mcpConfigPath,
		sessionDir:     sessionDir,
		taskEnvFile:    taskEnvFile,
		sessionTempDir: sessionTempDir,
		logger:         b.cfg.Logger,
	}

	go func() {
		defer close(proc.procDone)
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var msg claudeSDKMessage
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				proc.turnMu.Lock()
				turn := proc.activeTurn
				proc.turnMu.Unlock()
				if turn != nil {
					turn.noteInvalidEvent()
				}
				continue
			}
			proc.turnMu.Lock()
			turn := proc.activeTurn
			proc.turnMu.Unlock()
			if turn != nil {
				turn.handleMessage(msg)
			}
		}
		scanErr := scanner.Err()
		if scanErr != nil {
			_ = stdout.Close()
		}
		_ = stdin.Close()
		exitErr := cmd.Wait()
		releaseProcessGroup(cmd)
		proc.turnMu.Lock()
		proc.scanErr = scanErr
		proc.exitErr = exitErr
		proc.turnMu.Unlock()
	}()

	return proc, usageSnapshot, nil
}
