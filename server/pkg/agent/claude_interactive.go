package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// claudeInputWriter keeps user messages and CLI control responses from
// interleaving on the shared stream-json stdin pipe.
type claudeInputWriter struct {
	mu sync.Mutex
	w  io.Writer
}

type claudeInteractiveWriteResult struct {
	request *interactionRequest
	receipt InteractionReceipt
	err     error
}

func (w *claudeInputWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(data)
}

func (b *claudeBackend) ExecuteInteractive(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("claude prompt must not be empty")
	}
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "claude"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("claude executable not found at %q: %w", execPath, err)
	}
	runCtx, cancel := runContext(ctx, opts.Timeout)
	args := buildClaudeArgs(opts, b.cfg.Logger)
	var mcpConfigPath string
	if hasManagedMcpConfig(opts.McpConfig) {
		var err error
		mcpConfigPath, err = writeMcpConfigToTemp(opts.McpConfig)
		if err != nil {
			cancel()
			return nil, err
		}
		args = append(args, "--mcp-config", mcpConfigPath)
	}
	cleanupMCP := func() {
		if mcpConfigPath != "" {
			cleanupMcpConfigTemp(mcpConfigPath)
		}
	}
	cmd := b.cfg.commandAt(execPath).exec(runCtx, args...)
	hideAgentWindow(cmd)
	cmd.Cancel = func() error { return nil }
	cmd.WaitDelay = 10 * time.Second
	cmd.Dir = opts.Cwd
	cmd.Env = buildEnv(b.cfg.Env)
	if err := claudeRootSudoPreflight(args, cmd.Env); err != nil {
		cancel()
		cleanupMCP()
		return nil, err
	}
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		cleanupMCP()
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		cleanupMCP()
		return nil, err
	}
	stderr := newStderrTail(newLogWriter(b.cfg.Logger, "[claude:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderr
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		_ = stdin.Close()
		cancel()
		cleanupMCP()
		return nil, fmt.Errorf("start claude: %w", err)
	}

	control := newLiveControl()
	control.beforeFinish = opts.InteractiveBeforeFinish
	messages := make(chan Message, 256)
	results := make(chan Result, 1)
	var terminal atomic.Bool
	var closeOnce sync.Once
	closeStdin := func() { closeOnce.Do(func() { _ = stdin.Close() }) }
	writer := &claudeInputWriter{w: stdin}
	lines := make(chan []byte, 64)
	readerDone := make(chan error, 1)
	procDone := make(chan struct{})
	go func() {
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			select {
			case lines <- line:
			case <-runCtx.Done():
				readerDone <- runCtx.Err()
				close(lines)
				return
			}
		}
		readerDone <- scanner.Err()
		close(lines)
	}()
	go func() {
		select {
		case <-procDone:
			return
		case <-runCtx.Done():
		}
		closeStdin()
		signalProcessGroup(cmd, syscall.SIGTERM)
		if !waitProcessGroupGone(cmd, claudeTerminateGrace()) {
			signalProcessGroup(cmd, syscall.SIGKILL)
		}
		_ = stdout.Close()
	}()
	// Claude may write a startup banner before reading the first input frame.
	// The reader above must be live before the initial write begins.
	writeDone := make(chan error, 1)
	go func() { writeDone <- writeClaudeInput(writer, prompt) }()

	go func() {
		start := time.Now()
		result := Result{Status: "completed", Usage: make(map[string]TokenUsage)}
		graceful := false
		defer func() {
			control.close()
			if !graceful {
				cancel()
			}
			closeStdin()
			exitErr := cmd.Wait()
			close(procDone)
			releaseProcessGroup(cmd)
			cancel()
			cleanupMCP()
			if exitErr != nil && result.Status == "completed" {
				result.Status, result.Error = "failed", fmt.Sprintf("claude exited with error: %v", exitErr)
			}
			if graceful && result.Status == "completed" {
				terminal.Store(true)
			}
			if result.Error != "" {
				result.Error = withAgentStderr(result.Error, "claude", stderr.Tail())
			}
			result.DurationMs = time.Since(start).Milliseconds()
			close(messages)
			results <- result
			close(results)
		}()
		fail := func(err error) {
			result.Status, result.Error = "failed", err.Error()
			if runCtx.Err() == context.Canceled {
				result.Status = "aborted"
			} else if runCtx.Err() == context.DeadlineExceeded {
				result.Status = "timeout"
			}
		}
		var output, lastText string
		seenUsage := make(map[string]struct{})
		turnUsage := make(map[string]TokenUsage)
		settled, sawResult, sawAsyncLaunch := false, false, false
		queuedInput := false
		finishRequested, closeRequested := false, false
		var finishTimer <-chan time.Time
		var interruptTimer <-chan time.Time
		var interruptRequest *interactionRequest
		interruptID, interruptAck, interruptResult := "", false, false
		writeResults := make(chan claudeInteractiveWriteResult, 1)
		poll := time.NewTicker(500 * time.Millisecond)
		defer poll.Stop()
		for {
			if finishRequested && settled && !closeRequested && control.finish() {
				closeRequested = true
				closeStdin()
				finishTimer = time.After(10 * time.Second)
			}
			select {
			case err := <-writeDone:
				writeDone = nil
				if err != nil {
					fail(fmt.Errorf("write claude input: %w", err))
					return
				}
				if control.Snapshot().State == InteractionStarting {
					control.setState(InteractionWorking)
				}
			case written := <-writeResults:
				if written.err != nil {
					control.acknowledge(written.request, written.receipt, written.err)
					fail(fmt.Errorf("write claude interaction: %w", written.err))
					return
				}
				if written.request.command.Kind == "input" {
					control.acknowledge(written.request, written.receipt, nil)
				}
			case <-poll.C:
			case <-finishTimer:
				fail(errors.New("claude did not exit after finishing the interactive run"))
				return
			case <-interruptTimer:
				err := errors.New("claude interrupt did not settle within 60 seconds")
				if interruptRequest != nil {
					control.acknowledge(interruptRequest, InteractionReceipt{Outcome: "failed"}, err)
				}
				fail(err)
				return
			case <-runCtx.Done():
				fail(runCtx.Err())
				return
			case line, ok := <-lines:
				if !ok {
					scanErr := <-readerDone
					if scanErr != nil {
						fail(fmt.Errorf("read claude stream: %w", scanErr))
					} else if interruptRequest != nil {
						fail(errors.New("claude exited during interrupt"))
					} else if !sawResult {
						fail(errors.New("claude exited without a result"))
					} else if !closeRequested {
						fail(errors.New("claude process exited before run completion"))
					} else {
						graceful = true
					}
					return
				}
				var msg claudeSDKMessage
				if json.Unmarshal(line, &msg) != nil {
					continue
				}
				switch msg.Type {
				case "assistant":
					turn := b.handleAssistant(msg, messages, turnUsage, seenUsage)
					lastText = turn.resolveFallback(lastText)
				case "user":
					if b.handleUser(msg, messages) {
						sawAsyncLaunch = true
					}
				case "system":
					if msg.SessionID != "" {
						result.SessionID = msg.SessionID
					}
					trySend(messages, Message{Type: MessageStatus, Status: "running", SessionID: result.SessionID})
				case "log":
					if msg.Log != nil {
						trySend(messages, Message{Type: MessageLog, Level: msg.Log.Level, Content: msg.Log.Message})
					}
				case "control_request":
					go b.handleControlRequest(msg, writer)
				case "control_response":
					var response struct {
						Subtype   string `json:"subtype"`
						RequestID string `json:"request_id"`
						Error     string `json:"error"`
					}
					if json.Unmarshal(msg.Response, &response) != nil || response.RequestID != interruptID || interruptRequest == nil {
						continue
					}
					if response.Subtype != "success" {
						control.acknowledge(interruptRequest, InteractionReceipt{Outcome: "failed"}, fmt.Errorf("claude interrupt: %s", response.Error))
						fail(fmt.Errorf("claude interrupt: %s", response.Error))
						return
					}
					interruptAck = true
				case "result":
					sawResult, settled = true, true
					if msg.SessionID != "" {
						result.SessionID = msg.SessionID
					}
					if opts.ResumeSessionID != "" && opts.RequireSessionResume && result.SessionID != opts.ResumeSessionID {
						fail(errors.New("claude did not resume the requested session"))
						return
					}
					if msg.ResultText != "" {
						output = msg.ResultText
					} else {
						output = lastText
					}
					result.Output = output
					if u := claudeResultUsage(msg, opts.Model); len(u) > 0 {
						turnUsage = u
					}
					for model, usage := range turnUsage {
						total := result.Usage[model]
						total.InputTokens += usage.InputTokens
						total.OutputTokens += usage.OutputTokens
						total.CacheReadTokens += usage.CacheReadTokens
						total.CacheWriteTokens += usage.CacheWriteTokens
						result.Usage[model] = total
					}
					turnUsage = make(map[string]TokenUsage)
					if sawAsyncLaunch {
						fail(errors.New("claude launched an async background task; Multica-managed runs require foreground execution"))
						return
					}
					if interruptRequest != nil {
						interruptResult = true
					} else if reason := claudeTerminalReasonFailure(msg.TerminalReason, msg.ResultText); reason != "" {
						fail(errors.New(reason))
						return
					} else if msg.IsError {
						fail(fmt.Errorf("claude turn failed: %s", msg.ResultText))
						return
					}
					if interruptRequest == nil {
						if queuedInput {
							queuedInput, settled = false, false
							lastText, output = "", ""
							control.nextActivity()
						} else if opts.KeepInteractiveOpen {
							control.setState(InteractionAwaitingInput)
						} else {
							finishRequested = true
						}
					}
				}
				if interruptRequest != nil && interruptAck && interruptResult {
					control.setState(InteractionAwaitingInput)
					control.acknowledge(interruptRequest, InteractionReceipt{Outcome: "applied"}, nil)
					interruptRequest, interruptID = nil, ""
					interruptTimer = nil
				}
			case request := <-control.queue:
				receipt := InteractionReceipt{Outcome: "applied"}
				switch request.command.Kind {
				case "finish", "complete":
					finishRequested = true
					control.acknowledge(request, receipt, nil)
				case "interrupt":
					finishRequested = false
					if settled || control.Snapshot().State == InteractionAwaitingInput {
						receipt.Outcome = "already_idle"
						control.acknowledge(request, receipt, nil)
						continue
					}
					interruptID = "multica_" + request.command.ID
					data, _ := json.Marshal(map[string]any{"type": "control_request", "request_id": interruptID, "request": map[string]string{"subtype": "interrupt"}})
					interruptRequest, interruptAck, interruptResult = request, false, false
					interruptTimer = time.After(60 * time.Second)
					control.setState(InteractionInterrupting)
					go func() {
						_, err := writer.Write(append(data, '\n'))
						writeResults <- claudeInteractiveWriteResult{request: request, receipt: receipt, err: err}
					}()
				case "input":
					finishRequested = false
					wasIdle := settled || control.Snapshot().State == InteractionAwaitingInput
					if wasIdle {
						settled = false
						lastText, output = "", ""
						control.nextActivity()
					} else {
						receipt.Outcome = "queued"
						queuedInput = true
					}
					go func() {
						err := writeClaudeInput(writer, request.command.Text)
						writeResults <- claudeInteractiveWriteResult{request: request, receipt: receipt, err: err}
					}()
				}
			}
		}
	}()
	return &Session{Messages: messages, Result: results, Control: control, TerminalObserved: terminal.Load}, nil
}
