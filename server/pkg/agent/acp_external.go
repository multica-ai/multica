package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// acpExternalBackend implements Backend for external runtime extensions
// loaded from runtime.json. It spawns the CLI with ACP protocol args
// (--acp) and drives the ACP JSON-RPC 2.0 handshake over stdin/stdout.
//
// This is a lightweight ACP client that only supports the subset of ACP
// needed for non-interactive task execution: initialize → session/new →
// session/prompt → drain events → session/close.
type acpExternalBackend struct {
	cfg Config
}

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

	args := append([]string{}, b.cfg.ACPArgs...)
	args = append(args, filterCustomArgs(opts.CustomArgs, nil, b.cfg.Logger)...)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("acp external command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

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
		var output strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)

		reader := bufio.NewReader(stdout)
		decoder := json.NewDecoder(reader)
		var reqID atomic.Int64

		writeJSON := func(payload any) error {
			data, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			data = append(data, '\n')
			_, err = stdin.Write(data)
			return err
		}

		// Phase 1: ACP initialize handshake.
		if err := writeJSON(map[string]any{
			"jsonrpc": "2.0",
			"id":      reqID.Add(1),
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "1.0",
				"clientInfo": map[string]string{
					"name":    "multica-daemon",
					"version": "0.1.0",
				},
				"capabilities": map[string]any{},
			},
		}); err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("acp initialize write: %v", err)
			goto finish
		}
		{
			// Read initialize response (skip notifications).
			var initResp map[string]any
			if err := b.skipToResponse(decoder, &initResp); err != nil {
				finalStatus = "failed"
				finalError = err.Error()
				goto finish
			}
		}

		// Phase 2: session/new.
		{
			sessionParams := map[string]any{"cwd": opts.Cwd}
			if opts.Model != "" {
				sessionParams["model"] = opts.Model
			}
			if err := writeJSON(map[string]any{
				"jsonrpc": "2.0",
				"id":      reqID.Add(1),
				"method":  "session/new",
				"params":  sessionParams,
			}); err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("acp session/new write: %v", err)
				goto finish
			}
		}

		// Phase 3: session/prompt.
		{
			if err := writeJSON(map[string]any{
				"jsonrpc": "2.0",
				"id":      reqID.Add(1),
				"method":  "session/prompt",
				"params": map[string]any{
					"prompt": prompt,
				},
			}); err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("acp session/prompt write: %v", err)
				goto finish
			}
		}

		// Phase 4: Read events from session until close.
		for {
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				if err == io.EOF {
					break
				}
				continue
			}

			var frame map[string]any
			if err := json.Unmarshal(raw, &frame); err != nil {
				continue
			}

			// Skip JSON-RPC responses (they have an "id").
			if _, hasID := frame["id"]; hasID {
				continue
			}

			method, _ := frame["method"].(string)
			params, _ := frame["params"].(map[string]any)
			if params == nil {
				params = map[string]any{}
			}

			switch method {
			case "session/update":
				if sid, ok := params["sessionId"].(string); ok && sid != "" {
					sessionID = sid
				}
				trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
			case "assistant/message":
				b.handleAssistantMessage(params, msgCh, &output, usage)
			case "tool/result":
				b.handleToolResult(params, msgCh)
			case "session/error":
				finalStatus = "failed"
				if msg, ok := params["message"].(string); ok {
					finalError = msg
				} else {
					finalError = "session error"
				}
				goto finish
			case "session/close":
				goto finish
			}
		}

	finish:
		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		switch {
		case runCtx.Err() == context.DeadlineExceeded:
			finalStatus = "timeout"
			finalError = fmt.Sprintf("acp external timed out after %s", timeout)
		case runCtx.Err() == context.Canceled:
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

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
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
