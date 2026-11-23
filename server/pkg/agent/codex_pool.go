package agent

import (
	"context"
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

// codexSyncWriter serializes concurrent writes to a Codex app-server stdin pipe
// (lifecycle RPCs, server-request approval replies from the stdout reader, and
// mid-turn turn/steer calls).
type codexSyncWriter struct {
	mu sync.Mutex
	w  io.WriteCloser
}

func (w *codexSyncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

func (w *codexSyncWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Close()
}

type codexTurnHooks struct {
	cfg                      Config
	acceptNotification       func(method string, params map[string]any) bool
	onDiscardedNotification  func(method string, params map[string]any)
	onMessage                func(Message)
	onAgentMessageChunk      func(text string) bool
	onAgentMessage           func(text, phase string)
	onFinalAnswer            func(text string)
	onSemanticActivity       func(description string)
	onTurnDone               func(aborted bool)
	guardianRateLimitCircuit *codexGuardianRateLimitCircuit
	onGuardianRateLimit      func()
	rateLimits               *rateLimitTracker
	preserveUsageBaseline    bool
}

func (c *codexClient) attachTurnHooks(h codexTurnHooks) {
	c.hookMu.Lock()
	defer c.hookMu.Unlock()

	c.cfg = h.cfg
	c.acceptNotification = h.acceptNotification
	c.onDiscardedNotification = h.onDiscardedNotification
	c.onMessage = h.onMessage
	c.onAgentMessageChunk = h.onAgentMessageChunk
	c.onAgentMessage = h.onAgentMessage
	c.onFinalAnswer = h.onFinalAnswer
	c.onSemanticActivity = h.onSemanticActivity
	c.onTurnDone = h.onTurnDone
	c.guardianRateLimitCircuit = h.guardianRateLimitCircuit
	c.onGuardianRateLimit = h.onGuardianRateLimit
	c.rateLimits = h.rateLimits

	c.agentMessageStreams = nil
	c.agentMessageOrder = nil
	c.turnCompleted = false
	c.setActiveTurnID("")

	c.turnErrorMu.Lock()
	c.turnError = ""
	c.turnErrorMu.Unlock()

	c.usageMu.Lock()
	c.usage = TokenUsage{}
	if !h.preserveUsageBaseline {
		c.usageTotal = codexRawTokenUsage{}
		c.usageTotalSet = false
	}
	c.usageMu.Unlock()
}

func (c *codexClient) detachTurnHooks() {
	c.hookMu.Lock()
	defer c.hookMu.Unlock()

	c.flushAgentMessageDeltas()
	c.acceptNotification = func(string, map[string]any) bool { return false }
	c.onDiscardedNotification = nil
	c.onMessage = nil
	c.onAgentMessageChunk = nil
	c.onAgentMessage = nil
	c.onFinalAnswer = nil
	c.onSemanticActivity = nil
	c.onTurnDone = nil
	c.onGuardianRateLimit = nil
	c.rateLimits = nil
	c.agentMessageStreams = nil
	c.agentMessageOrder = nil
	c.setActiveTurnID("")
}

func (c *codexClient) flushAgentMessageDeltasLocked() {
	c.hookMu.Lock()
	c.flushAgentMessageDeltas()
	c.hookMu.Unlock()
}

type warmCodexProcess struct {
	fingerprint    string
	threadID       string
	cmd            *exec.Cmd
	stdin          *codexSyncWriter
	stderrBuf      *stderrTail
	client         *codexClient
	stopProcess    context.CancelFunc
	readerDone     chan struct{}
	sessionDir     string
	taskEnvFile    string
	sessionTempDir string
	launchStarted  time.Time
	logger         *slog.Logger

	closed           atomic.Bool
	closeOnce        sync.Once
	cleanupConfirmed bool
}

func (p *warmCodexProcess) Fingerprint() string { return p.fingerprint }

func (p *warmCodexProcess) Alive() bool {
	if p.closed.Load() {
		return false
	}
	if p.client.getProcessErr() != nil {
		return false
	}
	select {
	case <-p.readerDone:
		return false
	default:
		return true
	}
}

func (p *warmCodexProcess) Close() bool {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		_ = ClearTaskEnvFile(p.taskEnvFile)
		_ = p.stdin.Close()

		grace := codexGracefulShutdown()
		waitCh := make(chan struct{})
		var waitErr error
		var startWait sync.Once
		startProcessWait := func() {
			startWait.Do(func() {
				go func() {
					waitErr = p.cmd.Wait()
					close(waitCh)
				}()
			})
		}

		select {
		case <-p.readerDone:
		case <-time.After(grace):
			if p.logger != nil {
				p.logger.Warn("warm codex did not close stdout after stdin EOF; forcing shutdown",
					"pid", p.cmd.Process.Pid,
					"grace", grace.String(),
				)
			}
			p.stopProcess()
			startProcessWait()
			<-waitCh
			select {
			case <-p.readerDone:
			case <-time.After(grace):
			}
		}

		startProcessWait()
		var waitReturned bool
		select {
		case <-waitCh:
			waitReturned = true
		case <-time.After(grace):
			if p.logger != nil {
				p.logger.Warn("warm codex still alive after reader exited; forcing shutdown",
					"pid", p.cmd.Process.Pid,
					"grace", grace.String(),
				)
			}
			p.stopProcess()
			<-waitCh
			waitReturned = true
		}

		p.cleanupConfirmed = waitReturned && p.cmd.ProcessState != nil && waitProcessGroupGone(p.cmd, grace)
		if codexCleanupConfirmationOverride.Load() < 0 {
			p.cleanupConfirmed = false
		}
		if p.logger != nil {
			p.logger.Info("codex lifecycle",
				"phase", "cleanup",
				"warm_pool", true,
				"pid", p.cmd.Process.Pid,
				"process_group", p.cmd.Process.Pid,
				"latency", time.Since(p.launchStarted).Round(time.Millisecond).String(),
				"reaped", p.cleanupConfirmed,
				"exit_status", codexProcessExitStatus(p.cmd.ProcessState),
				"wait_error", waitErr,
				"stderr_bytes", p.stderrBuf.TotalBytes(),
				"stderr_truncated", p.stderrBuf.TotalBytes() > codexStderrTailBytes,
			)
		}
		releaseProcessGroup(p.cmd)
		p.stopProcess()
		activeCodexLaunches.Add(-1)
		if p.sessionDir != "" {
			_ = os.RemoveAll(p.sessionDir)
		}
	})
	return p.cleanupConfirmed
}

func (b *codexBackend) executeWarmOnce(ctx context.Context, prompt string, opts ExecOptions, attempt int) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "codex"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("codex executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	semanticInactivityTimeout := opts.SemanticInactivityTimeout
	if semanticInactivityTimeout == 0 {
		semanticInactivityTimeout = defaultCodexSemanticInactivityTimeout
	}
	handshakeTimeout, threadHandshakeTimeout := resolveCodexHandshakeTimeouts(opts)
	runCtx, cancel := runContext(ctx, timeout)

	codexHome := strings.TrimSpace(b.cfg.Env["CODEX_HOME"])
	if codexHome != "" {
		if err := ensureCodexMcpConfig(filepath.Join(codexHome, "config.toml"), opts.McpConfig, b.cfg.Logger); err != nil {
			cancel()
			return nil, fmt.Errorf("apply codex mcp_config: %w", err)
		}
	} else if hasManagedCodexMcpConfig(opts.McpConfig) {
		cancel()
		return nil, fmt.Errorf("codex: mcp_config is set but CODEX_HOME env var is not configured; cannot apply managed MCP")
	}

	runtimeCmd := b.cfg.commandAt(execPath)
	if codexHome != "" {
		opts.ExtraArgs = filterCodexShellEnvConfigOverrides(opts.ExtraArgs, b.cfg.Logger)
		opts.CustomArgs = filterCodexShellEnvConfigOverrides(opts.CustomArgs, b.cfg.Logger)
		runtimeCmd = runtimeCmd.withFilteredPrefix(func(prefix []string) []string {
			return filterCodexShellEnvConfigOverrides(prefix, b.cfg.Logger)
		})
	}
	if hasManagedCodexMcpConfig(opts.McpConfig) {
		runtimeCmd = runtimeCmd.withFilteredPrefix(func(prefix []string) []string {
			return filterCodexCustomConfigOverrides(prefix, b.cfg.Logger)
		})
	}
	if opts.ServiceTier == codexFastServiceTier {
		runtimeCmd = runtimeCmd.withFilteredPrefix(func(prefix []string) []string {
			return stripCodexFastModeConflicts(prefix, b.cfg.Logger)
		})
	}
	codexArgs := buildCodexArgs(opts, b.cfg.Logger)

	var configFiles []string
	if codexHome != "" {
		configFiles = append(configFiles, filepath.Join(codexHome, "config.toml"))
	}
	fingerprint := computeWarmSessionFingerprint(WarmSessionFingerprintInput{
		Provider:      "codex",
		Executable:    execPath,
		LaunchPrefix:  runtimeCmd.Prefix,
		Args:          codexArgs,
		Cwd:           opts.Cwd,
		Model:         opts.Model,
		ThinkingLevel: opts.ThinkingLevel,
		ServiceTier:   opts.ServiceTier,
		McpConfig:     opts.McpConfig,
		ConfigFiles:   configFiles,
		BriefHash:     opts.WarmPoolBriefHash,
		Env:           b.cfg.Env,
	})

	var warmProc *warmCodexProcess
	warmHit := false
	if opts.ResumeSessionID != "" {
		if acquired := b.cfg.WarmPool.Acquire("codex", opts.ResumeSessionID, fingerprint); acquired != nil {
			if candidate, ok := acquired.(*warmCodexProcess); ok {
				if err := WriteTaskEnvFile(candidate.taskEnvFile, b.cfg.Env); err != nil {
					b.cfg.Logger.Warn("warm codex: failed to refresh task env overlay; evicting", "error", err)
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

	var activeLaunches int64
	if !warmHit {
		var err error
		warmProc, activeLaunches, err = b.spawnWarmCodexProcess(runtimeCmd, codexArgs, opts, fingerprint, handshakeTimeout, threadHandshakeTimeout, attempt)
		if err != nil {
			cancel()
			return nil, err
		}
	} else {
		activeLaunches = activeCodexLaunches.Load()
		warmProc.client.handshakeTimeout = handshakeTimeout
		warmProc.client.threadHandshakeTimeout = threadHandshakeTimeout
		warmProc.client.attempt = attempt
		warmProc.client.activeLaunches = activeLaunches
	}

	codexVersion := strings.TrimSpace(b.cfg.CodexVersion)
	if codexVersion == "" {
		codexVersion = "unknown"
	}

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	semanticActivityCh := make(chan string, 256)
	guardianRateLimitTrip := make(chan struct{}, 1)
	var guardianRateLimitTripped atomic.Bool

	var outputMu sync.Mutex
	var output codexOutputSelection
	var semanticObserved atomic.Bool
	turnNotificationGate := &codexTurnNotificationGate{}
	firstItemWait := &codexFirstItemWaitObservation{}
	turnDone := make(chan bool, 1)

	c := warmProc.client
	observeMessage := func(msg Message) {
		logCodexAgentMessage(b.cfg.Logger, msg)
		activity := describeCodexSemanticActivity(msg)
		if activity == "status:running" {
			firstItemWait.start(time.Now(), c.activeTurnID())
		}
		trySendString(semanticActivityCh, activity)
		if activity != "" {
			semanticObserved.Store(true)
		}
	}

	c.attachTurnHooks(codexTurnHooks{
		cfg:                      b.cfg,
		acceptNotification:       turnNotificationGate.accept,
		guardianRateLimitCircuit: &codexGuardianRateLimitCircuit{},
		rateLimits:               newRateLimitTracker(opts.OnRateLimits),
		preserveUsageBaseline:    warmHit,
		onGuardianRateLimit: func() {
			guardianRateLimitTripped.Store(true)
			b.cfg.Logger.Warn("codex Guardian approval rate-limit circuit opened",
				"task_id", b.cfg.TaskID, "runtime_id", b.cfg.RuntimeID,
				"failure_count", codexGuardianRateLimitThreshold)
			select {
			case guardianRateLimitTrip <- struct{}{}:
			default:
			}
		},
		onDiscardedNotification: func(string, map[string]any) {
			semanticObserved.Store(true)
		},
		onMessage: func(msg Message) {
			observeMessage(msg)
			trySend(msgCh, msg)
		},
		onAgentMessageChunk: func(text string) bool {
			msg := Message{Type: MessageText, Content: text}
			observeMessage(msg)
			return trySend(msgCh, msg)
		},
		onAgentMessage: func(text, phase string) {
			outputMu.Lock()
			output.observeAgentMessage(text, phase)
			outputMu.Unlock()
		},
		onFinalAnswer: func(text string) {
			outputMu.Lock()
			output.observeFinalAnswer(text)
			outputMu.Unlock()
		},
		onSemanticActivity: func(description string) {
			semanticObserved.Store(true)
			b.cfg.Logger.Debug("codex semantic activity observed", "activity", description)
			trySendString(semanticActivityCh, description)
		},
		onTurnDone: func(aborted bool) {
			select {
			case turnDone <- aborted:
			default:
			}
		},
	})

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var threadID string
		var resumed bool

		closeWarmProc := func() bool {
			c.detachTurnHooks()
			return warmProc.Close()
		}

		if warmHit {
			threadID = warmProc.threadID
			resumed = true
			c.setThreadID(threadID)
			b.cfg.Logger.Info("codex warm session reused",
				"task_id", b.cfg.TaskID,
				"runtime_id", b.cfg.RuntimeID,
				"pid", warmProc.cmd.Process.Pid,
				"thread_id", threadID,
			)
			trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: threadID})
		} else {
			// 1. Initialize handshake
			initializeStarted := time.Now()
			b.cfg.Logger.Info("codex lifecycle", "phase", "initialize_sent", "warm_pool", true, "task_id", b.cfg.TaskID, "runtime_id", b.cfg.RuntimeID, "pid", warmProc.cmd.Process.Pid, "attempt", attempt, "active_launches", activeLaunches)
			_, err := c.request(runCtx, "initialize", map[string]any{
				"clientInfo": map[string]any{
					"name":    "multica-agent-sdk",
					"title":   "Multica Agent SDK",
					"version": "0.2.0",
				},
				"capabilities": map[string]any{
					"experimentalApi": true,
				},
			})
			if err != nil {
				initializeLatency := time.Since(initializeStarted)
				var handshakeErr *codexHandshakeTimeoutError
				timedOut := errors.As(err, &handshakeErr) && handshakeErr.Method == "initialize"
				if timedOut {
					signalProcessGroup(warmProc.cmd, syscall.SIGKILL)
				} else if runCtx.Err() != nil {
					warmProc.stopProcess()
				}
				cleanupConfirmed := closeWarmProc()
				finalStatus = "failed"
				finalError = fmt.Sprintf("codex initialize failed: %v", err)
				contextEnded := errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
				if !timedOut && !contextEnded {
					finalError = withAgentStderr(finalError, "codex", sanitizeCodexDiagnostic(warmProc.stderrBuf.Tail()))
				}
				retrySafe := timedOut && !semanticObserved.Load() && cleanupConfirmed && codexInitializeRetrySupported()
				if timedOut && !cleanupConfirmed {
					finalError += "; retry suppressed: process cleanup/reap not confirmed"
				} else if timedOut && cleanupConfirmed && !codexInitializeRetrySupported() {
					finalError += "; retry suppressed: process-tree cleanup cannot be confirmed on this platform"
				}
				b.cfg.Logger.Warn("codex lifecycle", "phase", "initialize_failure", "warm_pool", true, "task_id", b.cfg.TaskID, "runtime_id", b.cfg.RuntimeID, "pid", warmProc.cmd.Process.Pid, "attempt", attempt, "latency", initializeLatency.Round(time.Millisecond).String(), "semantic_activity", semanticObserved.Load(), "cleanup_confirmed", cleanupConfirmed, "retry_safe", retrySafe)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), codexInitializeRetrySafe: retrySafe}
				return
			}
			b.cfg.Logger.Info("codex lifecycle", "phase", "initialize_response", "warm_pool", true, "task_id", b.cfg.TaskID, "runtime_id", b.cfg.RuntimeID, "pid", warmProc.cmd.Process.Pid, "attempt", attempt, "latency", time.Since(initializeStarted).Round(time.Millisecond).String())
			c.notify("initialized")

			// 2. Start or resume thread
			threadID, resumed, err = c.startOrResumeThread(runCtx, opts, b.cfg.Logger)
			if err != nil {
				var handshakeErr *codexHandshakeTimeoutError
				timedOut := errors.As(err, &handshakeErr) && isCodexThreadSetupRPC(handshakeErr.Method)
				if timedOut {
					signalProcessGroup(warmProc.cmd, syscall.SIGKILL)
				} else if runCtx.Err() != nil {
					warmProc.stopProcess()
				}
				cleanupConfirmed := closeWarmProc()
				finalStatus = "failed"
				stderrTail := sanitizeCodexDiagnostic(warmProc.stderrBuf.Tail())
				finalError = err.Error()
				retrySafe := timedOut && cleanupConfirmed
				if retrySafe {
					finalStatus = "timeout"
				} else if timedOut {
					finalError += "; retry suppressed: process cleanup/reap not confirmed"
				}
				if c.threadSetupMethod != "" {
					classification := classifyCodexStartupStderr(stderrTail, timedOut)
					b.cfg.Logger.Warn("codex lifecycle",
						"phase", strings.ReplaceAll(c.threadSetupMethod, "/", "_")+"_failure",
						"warm_pool", true,
						"task_id", b.cfg.TaskID,
						"runtime_id", b.cfg.RuntimeID,
						"pid", warmProc.cmd.Process.Pid,
						"attempt", attempt,
						"active_launches", activeLaunches,
						"method", c.threadSetupMethod,
						"latency", time.Since(c.threadSetupStarted).Round(time.Millisecond).String(),
						"latency_ms", time.Since(c.threadSetupStarted).Milliseconds(),
						"cleanup_confirmed", cleanupConfirmed,
						"reaped", cleanupConfirmed,
						"retry_safe", retrySafe,
						"retry_attempted", false,
						"stderr_model_refresh_failure_count", classification.modelRefreshFailure,
						"stderr_model_refresh_timeout_count", classification.modelRefreshTimeout,
						"stderr_mcp_init_transport_count", classification.mcpInitTransport,
						"stderr_bare_timeout_count", classification.bareTimeout,
					)
				}
				resCh <- Result{
					Status:         finalStatus,
					Error:          finalError,
					DurationMs:     time.Since(startTime).Milliseconds(),
					ResumeRejected: isCodexResumeOverflow(opts, err),
				}
				return
			}
			c.setThreadID(threadID)
			if resumed {
				b.cfg.Logger.Info("codex thread resumed", "thread_id", threadID)
			} else {
				b.cfg.Logger.Info("codex thread started", "thread_id", threadID)
			}
		}

		// 3. Send turn and wait for completion
		turnPrompt := b.skillTurnPrompt(runCtx, c, prompt, opts)
		turnParams := map[string]any{
			"threadId": threadID,
			"input":    codexTurnInput(turnPrompt, opts.ResumeExpected, resumed, opts.ResumeContinuityNotice),
		}
		applyCodexReasoningEffort(turnParams, opts.ThinkingLevel)
		applyCodexServiceTier(turnParams, opts.ServiceTier)
		waitingForTurn := true
		var timeoutDiagnostic codexTimeoutDiagnostic
		var processExitErr error
		finishFirstItemWait := func(outcome string) {
			firstItemWait.finish(
				time.Now(),
				outcome,
				classifyCodexStartupStderr(warmProc.stderrBuf.Tail(), strings.HasSuffix(outcome, "_timeout")),
			)
		}
		finishTurn := func(aborted bool) {
			waitingForTurn = false
			switch {
			case aborted:
				finishFirstItemWait("turn_aborted")
				finalStatus = "aborted"
				if errMsg := c.getTurnError(); errMsg != "" {
					finalError = errMsg
				} else {
					finalError = "turn was aborted"
				}
			default:
				if errMsg := c.getTurnError(); errMsg != "" {
					finishFirstItemWait("turn_failed")
					finalStatus = "failed"
					finalError = errMsg
				} else {
					finishFirstItemWait("turn_completed")
				}
			}
		}
		finishRunContextDone := func() {
			waitingForTurn = false
			if runCtx.Err() == context.DeadlineExceeded {
				finishFirstItemWait("execution_timeout")
				finalStatus = "timeout"
				finalError = fmt.Sprintf("codex timed out after %s", timeout)
			} else {
				finishFirstItemWait("cancelled")
				finalStatus = "aborted"
				finalError = "execution cancelled"
			}
		}
		turnNotificationGate.arm()
		_, err := c.request(runCtx, "turn/start", turnParams)
		if err != nil {
			if runCtx.Err() != nil {
				finishRunContextDone()
				if !interruptCodexTurn(c, threadID, turnDone, opts.TurnInterruptTimeout, b.cfg.Logger) {
					warmProc.stopProcess()
				}
			} else {
				select {
				case aborted := <-turnDone:
					finishTurn(aborted)
				default:
					closeWarmProc()
					finalStatus = "failed"
					finalError = withAgentStderr(fmt.Sprintf("codex turn/start failed: %v", err), "codex", sanitizeCodexDiagnostic(warmProc.stderrBuf.Tail()))
					resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
					return
				}
			}
		}

		lastSemanticActivity := time.Now()
		lastSemanticActivityDescription := "turn/start"
		semanticTimer := time.NewTimer(semanticInactivityTimeout)
		defer semanticTimer.Stop()

		firstTurnNoProgressTimeout := codexFirstTurnNoProgressTimeout(semanticInactivityTimeout, opts.FirstTurnNoProgressTimeout)
		var firstTurnNoProgressTimer *time.Timer
		var firstTurnNoProgressTimerC <-chan time.Time
		firstTurnStarted := false
		firstTurnProgressObserved := false
		stopFirstTurnNoProgressTimer := func() {
			if firstTurnNoProgressTimer == nil {
				return
			}
			stopTimer(firstTurnNoProgressTimer)
			firstTurnNoProgressTimerC = nil
		}
		defer stopFirstTurnNoProgressTimer()

		for waitingForTurn {
			select {
			case aborted := <-turnDone:
				finishTurn(aborted)
			case <-guardianRateLimitTrip:
				waitingForTurn = false
				finishFirstItemWait("guardian_rate_limit")
				finalStatus = "failed"
				finalError = codexGuardianRateLimitError
				if !interruptCodexTurn(c, threadID, turnDone, opts.TurnInterruptTimeout, b.cfg.Logger) {
					warmProc.stopProcess()
				}
			case activity := <-semanticActivityCh:
				lastSemanticActivity = time.Now()
				lastSemanticActivityDescription = activity
				resetTimer(semanticTimer, semanticInactivityTimeout)
				if activity == "status:running" && !firstTurnStarted {
					firstTurnStarted = true
					firstItemWait.start(time.Now(), c.activeTurnID())
					firstTurnNoProgressTimer = time.NewTimer(firstTurnNoProgressTimeout)
					firstTurnNoProgressTimerC = firstTurnNoProgressTimer.C
				} else if firstTurnStarted && !firstTurnProgressObserved && isCodexFirstTurnProgressActivity(activity) {
					firstTurnProgressObserved = true
					if activity == "error:terminal" {
						finishFirstItemWait("turn_failed")
					} else {
						finishFirstItemWait("progress")
					}
					stopFirstTurnNoProgressTimer()
				}
			case <-firstTurnNoProgressTimerC:
				waitingForTurn = false
				finishFirstItemWait("no_progress_timeout")
				finalStatus = "timeout"
				turnID := c.activeTurnID()
				timeoutDiagnostic = codexTimeoutDiagnostic{
					Kind:         codexTimeoutFirstTurnNoProgress,
					Timeout:      firstTurnNoProgressTimeout,
					LastActivity: lastSemanticActivityDescription,
					ThreadID:     threadID,
					TurnID:       turnID,
					Model:        opts.Model,
				}
				b.cfg.Logger.Warn(CodexFirstTurnNoProgressMarker,
					"pid", warmProc.cmd.Process.Pid,
					"thread_id", threadID,
					"turn_id", turnID,
					"timeout", firstTurnNoProgressTimeout.String(),
					"last_activity", lastSemanticActivityDescription,
				)
			case <-semanticTimer.C:
				waitingForTurn = false
				finishFirstItemWait("semantic_inactivity_timeout")
				finalStatus = "timeout"
				turnID := c.activeTurnID()
				timeoutDiagnostic = codexTimeoutDiagnostic{
					Kind:         codexTimeoutSemanticInactivity,
					Timeout:      semanticInactivityTimeout,
					LastActivity: lastSemanticActivityDescription,
					ThreadID:     threadID,
					TurnID:       turnID,
					Model:        opts.Model,
				}
				b.cfg.Logger.Warn(CodexSemanticInactivityMarker,
					"pid", warmProc.cmd.Process.Pid,
					"thread_id", threadID,
					"turn_id", turnID,
					"timeout", semanticInactivityTimeout.String(),
					"last_activity", lastSemanticActivityDescription,
					"idle_for", time.Since(lastSemanticActivity).Round(time.Millisecond).String(),
				)
			case <-runCtx.Done():
				finishRunContextDone()
				if !interruptCodexTurn(c, threadID, turnDone, opts.TurnInterruptTimeout, b.cfg.Logger) {
					warmProc.stopProcess()
				}
			case <-c.processDone:
				select {
				case aborted := <-turnDone:
					finishTurn(aborted)
				default:
					if runCtx.Err() != nil {
						finishRunContextDone()
					} else {
						waitingForTurn = false
						finishFirstItemWait("process_exit")
						finalStatus = "failed"
						processExitErr = c.getProcessErr()
						if processExitErr == nil {
							processExitErr = errCodexProcessExited
						}
						finalError = processExitErr.Error()
					}
				}
			}
		}
		if guardianRateLimitTripped.Load() {
			finalStatus = "failed"
			finalError = codexGuardianRateLimitError
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("codex finished", "pid", warmProc.cmd.Process.Pid, "warm_hit", warmHit, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		// Flush any buffered agent message deltas and capture outputs/usage
		// before detaching hooks and parking or closing the process.
		c.detachTurnHooks()
		_ = ClearTaskEnvFile(warmProc.taskEnvFile)

		outputMu.Lock()
		finalOutput := codexDeliverableOutput(output)
		outputMu.Unlock()

		c.usageMu.Lock()
		u := c.usage
		c.usageMu.Unlock()

		canPark := finalStatus == "completed" &&
			finalError == "" &&
			!guardianRateLimitTripped.Load() &&
			processExitErr == nil &&
			threadID != "" &&
			warmProc.Alive() &&
			runCtx.Err() == nil

		var cleanupConfirmed bool
		if canPark {
			warmProc.threadID = threadID
			b.cfg.WarmPool.Park("codex", threadID, warmProc)
		} else {
			cleanupConfirmed = warmProc.Close()
		}

		if processExitErr != nil {
			finalError = withAgentStderr(processExitErr.Error(), "codex", sanitizeCodexDiagnostic(warmProc.stderrBuf.Tail()))
		}
		stderrTail := sanitizeCodexDiagnostic(warmProc.stderrBuf.Tail())
		if timeoutDiagnostic.Kind != codexTimeoutNone {
			timeoutDiagnostic.CodexVersion = detectCodexVersionForDiagnostics(context.Background(), b.cfg.commandAt(execPath), warmProc.cmd.Env, b.cfg.Logger)
			finalError = buildCodexTimeoutDiagnosticError(timeoutDiagnostic, stderrTail)
		}

		startupRefreshRetrySafe := !canPark &&
			timeoutDiagnostic.Kind == codexTimeoutFirstTurnNoProgress &&
			!firstTurnProgressObserved &&
			strings.Contains(stderrTail, codexModelCatalogRefreshFailureSignal) &&
			cleanupConfirmed && codexInitializeRetrySupported()

		if waitLatency, firstTurnID, outcome, classification, ok := firstItemWait.snapshot(); ok {
			if waitLatency < 0 {
				waitLatency = 0
			}
			if strings.HasSuffix(outcome, "_timeout") {
				classification = classifyCodexStartupStderr(stderrTail, true)
			}
			fields := []any{
				"phase", "first_item_wait",
				"warm_pool", true,
				"warm_hit", warmHit,
				"task_id", b.cfg.TaskID,
				"runtime_id", b.cfg.RuntimeID,
				"pid", warmProc.cmd.Process.Pid,
				"attempt", attempt,
				"active_launches", activeLaunches,
				"method", "turn/start",
				"thread_id", threadID,
				"turn_id", firstTurnID,
				"outcome", outcome,
				"latency", waitLatency.Round(time.Millisecond).String(),
				"latency_ms", waitLatency.Milliseconds(),
				"timeout", firstTurnNoProgressTimeout.String(),
				"semantic_inactivity_timeout", semanticInactivityTimeout.String(),
				"codex_version", codexVersion,
				"daemon_version", b.cfg.DaemonVersion,
				"cleanup_confirmed", cleanupConfirmed,
				"reaped", cleanupConfirmed,
				"retry_safe", startupRefreshRetrySafe,
				"stderr_model_refresh_failure_count", classification.modelRefreshFailure,
				"stderr_model_refresh_timeout_count", classification.modelRefreshTimeout,
				"stderr_mcp_init_transport_count", classification.mcpInitTransport,
				"stderr_bare_timeout_count", classification.bareTimeout,
			}
			switch outcome {
			case "progress", "turn_completed":
				b.cfg.Logger.Info("codex lifecycle", fields...)
			default:
				b.cfg.Logger.Warn("codex lifecycle", fields...)
			}
		}

		taskCodexHome := strings.TrimSpace(b.cfg.Env["CODEX_HOME"])
		usageMap := codexResultUsageMap(startTime, taskCodexHome, threadID, resumed, opts.Model, u)

		resCh <- Result{
			Status:                       finalStatus,
			Output:                       finalOutput,
			Error:                        finalError,
			SessionID:                    threadID,
			DurationMs:                   duration.Milliseconds(),
			Usage:                        usageMap,
			codexStartupRefreshRetrySafe: startupRefreshRetrySafe,
		}
	}()

	supplement := func(supplementCtx context.Context, instruction string) error {
		return supplementCodexTurn(supplementCtx, c, instruction)
	}
	supplementReady := func() bool {
		return c.getThreadID() != "" && c.activeTurnID() != ""
	}
	return &Session{Supplement: supplement, SupplementReady: supplementReady, Messages: msgCh, Result: resCh}, nil
}

func (b *codexBackend) spawnWarmCodexProcess(
	runtimeCmd Command,
	codexArgs []string,
	opts ExecOptions,
	fingerprint string,
	handshakeTimeout time.Duration,
	threadHandshakeTimeout time.Duration,
	attempt int,
) (*warmCodexProcess, int64, error) {
	sessionDir, err := os.MkdirTemp("", "multica-codex-warm-*")
	if err != nil {
		return nil, 0, fmt.Errorf("create warm codex session dir: %w", err)
	}
	sessionTempDir := filepath.Join(sessionDir, "tmp")
	if err := os.MkdirAll(sessionTempDir, 0o700); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, 0, fmt.Errorf("create warm codex temp dir: %w", err)
	}
	taskEnvFile := filepath.Join(sessionDir, "task-env.json")
	if err := WriteTaskEnvFile(taskEnvFile, b.cfg.Env); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, 0, fmt.Errorf("write warm codex task env overlay: %w", err)
	}

	processCtx, stopProcess := context.WithCancel(context.Background())
	cmd := runtimeCmd.exec(processCtx, codexArgs...)
	hideAgentWindow(cmd)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			signalProcessGroup(cmd, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = codexProcessWaitDelay()
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(codexArgs, trustAgentCommandPositional(0, "app-server")))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(PreparePooledChildEnv(b.cfg.Env, taskEnvFile, sessionTempDir))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stopProcess()
		_ = os.RemoveAll(sessionDir)
		return nil, 0, fmt.Errorf("codex stdout pipe: %w", err)
	}
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		stopProcess()
		_ = os.RemoveAll(sessionDir)
		return nil, 0, fmt.Errorf("codex stdin pipe: %w", err)
	}
	stdin := &codexSyncWriter{w: stdinPipe}
	stderrBuf := newStderrTail(io.Discard, codexStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		stopProcess()
		_ = os.RemoveAll(sessionDir)
		return nil, 0, fmt.Errorf("start codex: %w", err)
	}
	activeLaunches := activeCodexLaunches.Add(1)
	for {
		maxSeen := maxActiveCodexLaunchesObserved.Load()
		if activeLaunches <= maxSeen || maxActiveCodexLaunchesObserved.CompareAndSwap(maxSeen, activeLaunches) {
			break
		}
	}
	launchStarted := time.Now()
	codexVersion := strings.TrimSpace(b.cfg.CodexVersion)
	if codexVersion == "" {
		codexVersion = "unknown"
	}
	b.cfg.Logger.Info("codex lifecycle",
		"phase", "spawn",
		"warm_pool", true,
		"task_id", b.cfg.TaskID,
		"runtime_id", b.cfg.RuntimeID,
		"pid", cmd.Process.Pid,
		"process_group", cmd.Process.Pid,
		"cwd", opts.Cwd,
		"attempt", attempt,
		"active_launches", activeLaunches,
		"codex_version", codexVersion,
		"daemon_version", b.cfg.DaemonVersion,
	)

	c := &codexClient{
		cfg:                    b.cfg,
		stdin:                  stdin,
		pending:                make(map[int]*pendingRPC),
		processDone:            make(chan struct{}),
		handshakeTimeout:       handshakeTimeout,
		threadHandshakeTimeout: threadHandshakeTimeout,
		pid:                    cmd.Process.Pid,
		attempt:                attempt,
		activeLaunches:         activeLaunches,
		notificationProtocol:   "unknown",
		acceptNotification:     func(string, map[string]any) bool { return false },
	}

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			c.handleLine(line)
		}
		c.flushAgentMessageDeltasLocked()
		if err := scanner.Err(); err != nil {
			c.markProcessExited(fmt.Errorf("%w: %w", errCodexProcessExited, err))
			return
		}
		c.markProcessExited(errCodexProcessExited)
	}()

	return &warmCodexProcess{
		fingerprint:    fingerprint,
		cmd:            cmd,
		stdin:          stdin,
		stderrBuf:      stderrBuf,
		client:         c,
		stopProcess:    stopProcess,
		readerDone:     readerDone,
		sessionDir:     sessionDir,
		taskEnvFile:    taskEnvFile,
		sessionTempDir: sessionTempDir,
		launchStarted:  launchStarted,
		logger:         b.cfg.Logger,
	}, activeLaunches, nil
}
