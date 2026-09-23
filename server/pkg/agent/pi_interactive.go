package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// piRPC owns the protocol on one goroutine. request pumps events while waiting
// for a response, so a tool-output burst cannot block an abort acknowledgement.
type piRPC struct {
	ctx      context.Context
	stdin    io.Writer
	lines    <-chan []byte
	onEvent  func([]byte) error
	sequence uint64
}

type piInteractiveResponse struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Success bool            `json:"success"`
	Error   string          `json:"error"`
	Data    json.RawMessage `json:"data"`
}

func (p *piRPC) request(kind string, fields map[string]any) (json.RawMessage, error) {
	if err := p.ctx.Err(); err != nil {
		return nil, err
	}
	p.sequence++
	id := fmt.Sprintf("multica-%d", p.sequence)
	if fields == nil {
		fields = make(map[string]any)
	}
	fields["id"], fields["type"] = id, kind
	data, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	// Writing and reading concurrently avoids pipe-buffer deadlock for large
	// prompts. Cancellation closes the process pipes and releases this writer.
	written := make(chan error, 1)
	go func() { _, err := p.stdin.Write(append(data, '\n')); written <- err }()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case err := <-written:
			if err != nil {
				return nil, fmt.Errorf("pi RPC write: %w", err)
			}
			written = nil
		case line, ok := <-p.lines:
			if !ok {
				return nil, errors.New("pi RPC closed before acknowledgement")
			}
			var response piInteractiveResponse
			if json.Unmarshal(line, &response) != nil {
				return nil, errors.New("invalid Pi RPC JSON")
			}
			if response.Type != "response" {
				if err := p.onEvent(line); err != nil {
					return nil, err
				}
				continue
			}
			if response.ID != id {
				continue
			}
			if !response.Success {
				return nil, fmt.Errorf("pi %s rejected: %s", kind, response.Error)
			}
			return response.Data, nil
		case <-timer.C:
			return nil, fmt.Errorf("pi %s acknowledgement timed out; delivery is uncertain", kind)
		case <-p.ctx.Done():
			return nil, p.ctx.Err()
		}
	}
}

func (b *piBackend) ExecuteInteractive(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	version, versionErr := parseSemver(b.cfg.CLIVersion)
	if versionErr != nil || version.lessThan(semver{Minor: 87}) {
		return nil, errors.New("interactive Pi requires a detected Pi version >= 0.87.0 (agent_settled and clear_queue); wrappers must forward --version")
	}
	if b.providerLabel != "" && b.providerLabel != "pi" {
		return nil, errors.New("interactive mode is supported only for Pi, not Pi forks")
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("pi prompt must not be empty")
	}
	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = "pi"
	}
	lookedUp, err := exec.LookPath(execName)
	if err != nil {
		return nil, fmt.Errorf("pi executable: %w", err)
	}
	path := opts.ResumeSessionID
	if path != "" {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return nil, errors.New("saved Pi session is unavailable or empty; refusing to start an unrelated conversation")
		}
	}
	if path == "" {
		path, err = newPiSessionPath()
		if err != nil {
			return nil, err
		}
	}
	if err := ensurePiSessionFile(path); err != nil {
		return nil, err
	}
	lock, locked, err := tryLockPiSessionFile(path)
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errors.New("pi session is already in use")
	}
	runCtx, cancel := runContext(ctx, opts.Timeout)
	args := buildPiModeArgs(path, opts, b.cfg.Logger, true)
	cmd, _, _ := b.cfg.commandAt(execName).execVia(runCtx, choosePiInvocation, lookedUp, args, b.cfg.Logger)
	hideAgentWindow(cmd)
	cmd.Dir, cmd.Env, cmd.WaitDelay = opts.Cwd, buildEnv(b.cfg.Env), 10*time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		releasePiSessionFileLock(lock)
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		closePiReadPipe(stdout)
		releasePiSessionFileLock(lock)
		return nil, err
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[pi:stderr] ")
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		_ = stdin.Close()
		closePiReadPipe(stdout)
		releasePiSessionFileLock(lock)
		return nil, err
	}
	control := newLiveControl()
	control.beforeFinish = opts.InteractiveBeforeFinish
	messages := make(chan Message, 256)
	results := make(chan Result, 1)
	var terminal atomic.Bool
	lines := make(chan []byte, 32)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer close(lines)
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			select {
			case lines <- line:
			case <-runCtx.Done():
				return
			}
		}
	}()
	go func() { <-runCtx.Done(); _ = stdin.Close(); closePiReadPipe(stdout) }()
	go func() {
		start := time.Now()
		result := Result{Status: "completed", SessionID: path, Usage: make(map[string]TokenUsage)}
		defer func() {
			control.close()
			cancel()
			_ = stdin.Close()
			closePiReadPipe(stdout)
			<-readerDone
			_ = cmd.Wait()
			releaseProcessGroup(cmd)
			releasePiSessionFileLock(lock)
			result.DurationMs = time.Since(start).Milliseconds()
			close(messages)
			results <- result
			close(results)
		}()
		emit := func(m Message) {
			select {
			case messages <- m:
			case <-runCtx.Done():
			}
		}
		var output, textBuffer strings.Builder
		settled, stopReason, activityError := false, "", ""
		onEvent := func(line []byte) error {
			var event piStreamEvent
			if json.Unmarshal(line, &event) != nil {
				return errors.New("invalid Pi RPC event JSON")
			}
			switch event.Type {
			case "agent_start", "turn_start":
				settled, stopReason, activityError = false, "", ""
			case "message_update":
				if event.AssistantMessageEvent == nil {
					return nil
				}
				switch event.AssistantMessageEvent.Type {
				case "text_delta":
					if text := drainPiTextBuffer(&textBuffer, event.AssistantMessageEvent.Delta); text != "" {
						output.WriteString(text)
						emit(Message{Type: MessageText, Content: text})
					}
				case "thinking_delta":
					emit(Message{Type: MessageThinking, Content: event.AssistantMessageEvent.Delta})
				}
			case "tool_execution_start":
				var input map[string]any
				_ = json.Unmarshal(event.Args, &input)
				emit(Message{Type: MessageToolUse, Tool: event.ToolName, CallID: event.ToolCallID, Input: input})
			case "tool_execution_end":
				emit(Message{Type: MessageToolResult, Tool: event.ToolName, CallID: event.ToolCallID, Output: decodePiResult(event.Result)})
			case "tool_execution_update":
				emit(Message{Type: MessageToolProgress, Tool: event.ToolName, CallID: event.ToolCallID, Output: decodePiResult(event.PartialResult)})
			case "turn_end":
				message := decodePiMessage(event.Message)
				if message == nil {
					return nil
				}
				stopReason, activityError = message.StopReason, message.ErrorMessage
				if message.Usage != nil {
					model := message.Model
					if model == "" {
						model = opts.Model
					}
					if model == "" {
						model = "unknown"
					}
					u := result.Usage[model]
					u.InputTokens += message.Usage.Input
					u.OutputTokens += message.Usage.Output
					u.CacheReadTokens += message.Usage.CacheRead
					u.CacheWriteTokens += message.Usage.CacheWrite
					result.Usage[model] = u
				}
			case "error":
				activityError = decodePiString(event.Message)
			case "auto_retry_end":
				if !event.Success {
					activityError = event.FinalError
				}
			case "agent_settled":
				settled = true
				if text := flushPiTextBuffer(&textBuffer); text != "" {
					output.WriteString(text)
					emit(Message{Type: MessageText, Content: text})
				}
			case "extension_ui_request":
				var dialog struct {
					Method string `json:"method"`
				}
				_ = json.Unmarshal(line, &dialog)
				switch dialog.Method {
				case "select", "confirm", "input", "editor":
					return errors.New("Pi extension requested a dialog; dialog routing is not supported by issue conversations (no approval was granted)")
				}
			}
			return nil
		}
		rpc := &piRPC{ctx: runCtx, stdin: stdin, lines: lines, onEvent: onEvent}
		fail := func(err error) {
			result.Status, result.Error = "failed", err.Error()
			if runCtx.Err() == context.Canceled {
				result.Status = "aborted"
			}
			if runCtx.Err() == context.DeadlineExceeded {
				result.Status = "timeout"
			}
		}
		// A harmless query verifies that this is an RPC peer before submitting
		// paid work. Version-specific settled semantics are validated by fixtures.
		if _, err := rpc.request("get_state", nil); err != nil {
			fail(err)
			return
		}
		emit(Message{Type: MessageStatus, Status: "running", SessionID: path})
		if _, err := rpc.request("prompt", map[string]any{"message": prompt}); err != nil {
			fail(err)
			return
		}
		control.setState(InteractionWorking)
		finishPoll := time.NewTicker(500 * time.Millisecond)
		defer finishPoll.Stop()
		finishRequested := false
		for {
			result.Output = output.String()
			if settled && control.Snapshot().State != InteractionAwaitingInput && (activityError != "" || stopReason == "error" || stopReason == "aborted") {
				result.Status, result.Error = "failed", activityError
				if result.Error == "" {
					result.Error = "pi activity ended with " + stopReason
				}
				terminal.Store(true)
				return
			}
			if finishRequested && settled && control.finish() {
				terminal.Store(true)
				return
			}
			if settled && control.Snapshot().State != InteractionAwaitingInput {
				if opts.KeepInteractiveOpen {
					control.setState(InteractionAwaitingInput)
				} else if control.finish() {
					terminal.Store(true)
					return
				}
			}
			select {
			case <-finishPoll.C:
			case <-runCtx.Done():
				fail(runCtx.Err())
				return
			case line, ok := <-lines:
				if !ok {
					fail(errors.New("Pi RPC process exited before run completion"))
					return
				}
				if err := onEvent(line); err != nil {
					fail(err)
					return
				}
			case request := <-control.queue:
				receipt := InteractionReceipt{Outcome: "applied"}
				if request.command.Kind == "finish" || request.command.Kind == "complete" {
					finishRequested = true
					control.acknowledge(request, receipt, nil)
					continue
				}
				// A human correction or interruption cancels the agent's intent
				// to finish. It must request completion again after the next work.
				finishRequested = false
				if request.command.Kind == "interrupt" {
					if settled || control.Snapshot().State == InteractionAwaitingInput {
						receipt.Outcome = "already_idle"
						control.acknowledge(request, receipt, nil)
						continue
					}
					control.setState(InteractionInterrupting)
					cleared, err := rpc.request("clear_queue", nil)
					if err != nil {
						control.acknowledge(request, receipt, err)
						fail(err)
						return
					}
					var queue struct {
						Steering []string `json:"steering"`
						FollowUp []string `json:"followUp"`
					}
					if err := json.Unmarshal(cleared, &queue); err != nil {
						control.acknowledge(request, receipt, err)
						fail(err)
						return
					}
					receipt.ClearedSteering, receipt.ClearedFollowUp = queue.Steering, queue.FollowUp
					if settled {
						receipt.Outcome = "already_idle"
						control.setState(InteractionWorking)
						control.acknowledge(request, receipt, nil)
						continue
					}
					_, err = rpc.request("abort", nil)
					if err != nil {
						control.acknowledge(request, receipt, err)
						fail(err)
						return
					}
					// abort waits for idle. Compaction/tool aborts need not emit
					// a new assistant message with stopReason=aborted, so the
					// acknowledged request (not the previous message) owns pause.
					settled = true
					control.setState(InteractionAwaitingInput)
					control.acknowledge(request, receipt, nil)
				} else {
					kind := "steer"
					if settled || control.Snapshot().State == InteractionAwaitingInput {
						kind = "prompt"
						settled, stopReason, activityError = false, "", ""
						output.Reset()
						control.nextActivity()
					}
					_, err := rpc.request(kind, map[string]any{"message": request.command.Text})
					control.acknowledge(request, receipt, err)
					if err != nil {
						fail(err)
						return
					}
				}
			}
		}
	}()
	return &Session{Messages: messages, Result: results, Control: control, TerminalObserved: terminal.Load}, nil
}
