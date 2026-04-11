package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// piBackend implements Backend by spawning pi-acp and communicating via
// ACP JSON-RPC over stdio.
type piBackend struct {
	cfg Config
}

func (b *piBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "pi-acp"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("pi-acp executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	cmd := exec.CommandContext(runCtx, execPath)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("pi-acp stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("pi-acp stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[pi-acp:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start pi-acp: %w", err)
	}

	b.cfg.Logger.Info("pi-acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)
	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		sendMsg := func(id string, method string, params map[string]any) error {
			msg := map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"method":  method,
				"params":  params,
			}
			return piWriteJSON(stdin, msg)
		}

		waitForID := func(id string) (map[string]any, error) {
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				var msg map[string]any
				if err := json.Unmarshal([]byte(line), &msg); err != nil {
					continue
				}
				if msgID, _ := msg["id"].(string); msgID == id {
					return msg, nil
				}
			}
			return nil, fmt.Errorf("scanner ended before response id=%s", id)
		}

		// Step 1: initialize
		if err := sendMsg("1", "initialize", map[string]any{
			"protocolVersion": 1,
			"clientInfo":      map[string]any{"name": "multica", "version": "0.1.0"},
		}); err != nil {
			resCh <- Result{Status: "failed", Error: fmt.Sprintf("pi-acp init: %v", err)}
			return
		}
		if _, err := waitForID("1"); err != nil {
			resCh <- Result{Status: "failed", Error: fmt.Sprintf("pi-acp init response: %v", err)}
			return
		}

		// Step 2: new session
		cwd := opts.Cwd
		if cwd == "" {
			cwd = "/tmp"
		}
		if err := sendMsg("2", "session/new", map[string]any{
			"cwd":        cwd,
			"mcpServers": []any{},
		}); err != nil {
			resCh <- Result{Status: "failed", Error: fmt.Sprintf("pi-acp session/new: %v", err)}
			return
		}
		sessionResp, err := waitForID("2")
		if err != nil {
			resCh <- Result{Status: "failed", Error: fmt.Sprintf("pi-acp session/new response: %v", err)}
			return
		}
		sessionID := ""
		if result, ok := sessionResp["result"].(map[string]any); ok {
			sessionID, _ = result["sessionId"].(string)
		}
		if sessionID == "" {
			resCh <- Result{Status: "failed", Error: "pi-acp did not return a session ID"}
			return
		}

		// Step 3: send prompt
		fullPrompt := prompt
		if opts.SystemPrompt != "" {
			fullPrompt = opts.SystemPrompt + "\n\n" + prompt
		}
		if err := sendMsg("3", "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt":    []map[string]any{{"type": "text", "text": fullPrompt}},
		}); err != nil {
			resCh <- Result{Status: "failed", Error: fmt.Sprintf("pi-acp prompt: %v", err)}
			return
		}

		// Step 4: stream notifications until agent is fully done.
		// We track two conditions that can arrive in either order:
		//   - promptResponseReceived: response to our prompt RPC (id "3")
		//   - agentDone: session_info_update with running == false
		// We exit only when BOTH are true.
		var output strings.Builder
		finalStatus := "completed"
		var finalError string
		promptResponseReceived := false
		agentDone := false

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var msg map[string]any
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				continue
			}

			// Prompt response -- note it but check if agent already done
			if msgID, _ := msg["id"].(string); msgID == "3" {
				promptResponseReceived = true
				if errObj, ok := msg["error"].(map[string]any); ok {
					errMsg, _ := errObj["message"].(string)
					finalStatus = "failed"
					finalError = errMsg
					break
				}
					if agentDone {
					goto done
				}
				continue
			}

			// Notifications
			method, _ := msg["method"].(string)
			if method != "session/update" {
				continue
			}

			params, _ := msg["params"].(map[string]any)
			if params == nil {
				continue
			}
			update, _ := params["update"].(map[string]any)
			if update == nil {
				continue
			}

			updateType, _ := update["sessionUpdate"].(string)
			switch updateType {
			case "agent_message_chunk":
				if content, ok := update["content"].(map[string]any); ok {
					if text, ok := content["text"].(string); ok && text != "" {
						output.WriteString(text)
						trySend(msgCh, Message{Type: MessageText, Content: text})
					}
				}

			case "tool_call":
				tool, _ := update["title"].(string)
				trySend(msgCh, Message{Type: MessageToolUse, Tool: tool})

			case "tool_call_update":
				tool, _ := update["title"].(string)
				if tool != "" {
					trySend(msgCh, Message{Type: MessageToolResult, Tool: tool})
				}

			case "session_info_update":
				// NOTE: _meta.piAcp.running is a Pi-specific field (not ACP standard).
				// Requires pi-acp v0.66.1+.
				b.cfg.Logger.Debug("pi-acp session_info_update", "payload", update)
				if meta, ok := update["_meta"].(map[string]any); ok {
					if piAcp, ok := meta["piAcp"].(map[string]any); ok {
						running, _ := piAcp["running"].(bool)
						if !running {
							agentDone = true
							if promptResponseReceived {
								goto done
							}
						}
					}
				}
			}
		}

	done:
		stdin.Close()
		cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("pi-acp timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		}

		b.cfg.Logger.Info("pi-acp finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond))

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func piWriteJSON(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}
