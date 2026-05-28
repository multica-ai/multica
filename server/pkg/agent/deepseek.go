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
	"time"
)

// deepseekBlockedArgs are flags hardcoded by the daemon that must not
// be overridden by user-configured custom_args. `app-server` is the
// subcommand and `--stdio` selects the JSON-RPC stdio transport;
// overriding either would break the daemon↔DeepSeek communication
// contract.
var deepseekBlockedArgs = map[string]blockedArgMode{
	"app-server": blockedStandalone,
	"--stdio":    blockedStandalone,
}

// deepseekBackend implements Backend by spawning `deepseek app-server
// --stdio` and communicating via DeepSeek-TUI's native JSON-RPC 2.0
// protocol over stdin/stdout.
//
// DeepSeek-TUI (https://github.com/Hmbown/DeepSeek-TUI) does NOT
// implement ACP. It uses a custom protocol with thread/* methods:
//   - thread/start   → creates a new thread (session)
//   - thread/message → sends user input and blocks until the LLM
//     finishes (content arrives as push events on stdout)
//   - thread/resume  → resumes an existing thread
//   - app/models     → returns the model catalog
//
// Push events are bare JSON lines on stdout (NOT JSON-RPC envelopes):
//   - {"type":"response_delta","response_id":"...","delta":"..."}
//   - {"type":"tool_lifecycle","response_id":"...","tool_name":"...","phase":"...","payload":{...}}
//
// These arrive DURING the thread/message call, before the JSON-RPC
// response is sent. DeepSeek-TUI builds observed so far do not emit token
// usage for every response; when a future build adds usage/token_usage fields
// to either push events or JSON-RPC results, the parser below records them.
type deepseekBackend struct {
	cfg Config
}

// deepseekClient manages the JSON-RPC transport for a single DeepSeek
// app-server process. It handles two types of stdout lines:
//   - JSON-RPC responses: have "jsonrpc"+"id"+"result"/"error"
//   - Push events: have "type" field, no "jsonrpc" field
type deepseekClient struct {
	cfg     Config
	stdin   interface{ Write([]byte) (int, error) }
	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int
	pending map[int]*pendingRPC

	usageMu sync.Mutex
	usage   TokenUsage

	onMessage func(Message)
}

func (c *deepseekClient) writeLine(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.stdin.Write(data)
	return err
}

func (c *deepseekClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: method}
	c.pending[id] = pr
	c.mu.Unlock()

	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	data = append(data, '\n')
	if err := c.writeLine(data); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("write %s: %w", method, err)
	}

	select {
	case res := <-pr.ch:
		return res.result, res.err
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *deepseekClient) closeAllPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, pr := range c.pending {
		pr.ch <- rpcResult{err: err}
		delete(c.pending, id)
	}
}

// handleLine dispatches a single stdout line as either a JSON-RPC
// response or a push event. DeepSeek-TUI mixes both on the same
// stream: JSON-RPC responses have "jsonrpc"+"id", push events have
// "type" without "jsonrpc".
func (c *deepseekClient) handleLine(line string) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return
	}

	// JSON-RPC response: has "id" + ("result" or "error").
	if _, hasID := raw["id"]; hasID {
		if _, hasResult := raw["result"]; hasResult {
			c.handleResponse(raw)
			return
		}
		if _, hasError := raw["error"]; hasError {
			c.handleResponse(raw)
			return
		}
	}

	// Push event: has "type" field.
	if _, hasType := raw["type"]; hasType {
		c.handlePushEvent(raw)
	}
}

func (c *deepseekClient) handleResponse(raw map[string]json.RawMessage) {
	var id int
	if err := json.Unmarshal(raw["id"], &id); err != nil {
		var fid float64
		if err := json.Unmarshal(raw["id"], &fid); err != nil {
			return
		}
		id = int(fid)
	}

	c.mu.Lock()
	pr, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.mu.Unlock()

	if !ok {
		return
	}

	if errData, hasErr := raw["error"]; hasErr {
		var rpcErr struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(errData, &rpcErr)
		pr.ch <- rpcResult{err: fmt.Errorf("%s: %s (code=%d)", pr.method, rpcErr.Message, rpcErr.Code)}
	} else {
		c.accumulateUsage(raw["result"])
		pr.ch <- rpcResult{result: raw["result"]}
	}
}

// handlePushEvent processes bare JSON push events from DeepSeek-TUI.
// These are emitted by the StdoutHookSink during thread/message
// processing. Format: {"type":"<event_type>", ...}
func (c *deepseekClient) handlePushEvent(raw map[string]json.RawMessage) {
	var eventType string
	_ = json.Unmarshal(raw["type"], &eventType)

	switch eventType {
	case "response_delta":
		var delta string
		if d, ok := raw["delta"]; ok {
			_ = json.Unmarshal(d, &delta)
		}
		// Skip status deltas like "queued" — these are lifecycle signals,
		// not content.
		if delta != "" && delta != "queued" && c.onMessage != nil {
			c.onMessage(Message{Type: MessageText, Content: delta})
		}

	case "tool_lifecycle":
		var evt struct {
			ResponseID string         `json:"response_id"`
			ToolName   string         `json:"tool_name"`
			Phase      string         `json:"phase"`
			Payload    map[string]any `json:"payload"`
		}
		b, _ := json.Marshal(raw)
		_ = json.Unmarshal(b, &evt)

		if c.onMessage == nil {
			return
		}

		toolName := deepseekToolName(evt.ToolName)
		switch evt.Phase {
		case "start", "running":
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   toolName,
				CallID: evt.ResponseID + ":" + evt.ToolName,
				Input:  evt.Payload,
			})
		case "complete", "error":
			output := ""
			if evt.Payload != nil {
				if o, ok := evt.Payload["output"]; ok {
					output = fmt.Sprintf("%v", o)
				} else if o, ok := evt.Payload["error"]; ok {
					output = fmt.Sprintf("error: %v", o)
				}
			}
			c.onMessage(Message{
				Type:   MessageToolResult,
				Tool:   toolName,
				CallID: evt.ResponseID + ":" + evt.ToolName,
				Output: output,
			})
		}
	case "usage", "usage_update", "token_usage":
		c.accumulateUsageFromRaw(raw)
	}
}

func (c *deepseekClient) accumulateUsage(raw json.RawMessage) {
	if u := tokenUsageFromRawMessage(raw); tokenUsageHasTokens(u) {
		c.usageMu.Lock()
		mergeTokenUsageMax(&c.usage, u)
		c.usageMu.Unlock()
	}
}

func (c *deepseekClient) accumulateUsageFromRaw(raw map[string]json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	data := make(map[string]any, len(raw))
	for k, v := range raw {
		var value any
		if err := json.Unmarshal(v, &value); err == nil {
			data[k] = value
		}
	}
	u := tokenUsageFromMap(data)
	if !tokenUsageHasTokens(u) {
		return
	}
	c.usageMu.Lock()
	mergeTokenUsageMax(&c.usage, u)
	c.usageMu.Unlock()
}

func (b *deepseekBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "deepseek"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("deepseek executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	dsArgs := append([]string{"app-server", "--stdio"}, filterCustomArgs(opts.CustomArgs, deepseekBlockedArgs, b.cfg.Logger)...)
	cmd := exec.CommandContext(runCtx, execPath, dsArgs...)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", dsArgs)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("deepseek stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("deepseek stdin pipe: %w", err)
	}
	providerErr := newACPProviderErrorSniffer("DeepSeek-TUI")
	cmd.Stderr = io.MultiWriter(newLogWriter(b.cfg.Logger, "[deepseek:stderr] "), providerErr)

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start deepseek: %w", err)
	}

	b.cfg.Logger.Info("deepseek started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var outputMu sync.Mutex
	var output strings.Builder

	c := &deepseekClient{
		cfg:     b.cfg,
		stdin:   stdin,
		pending: make(map[int]*pendingRPC),
		onMessage: func(msg Message) {
			if msg.Type == MessageText {
				outputMu.Lock()
				output.WriteString(msg.Content)
				outputMu.Unlock()
			}
			trySend(msgCh, msg)
		},
	}

	// Read stdout lines in background.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			c.handleLine(line)
		}
		c.closeAllPending(fmt.Errorf("deepseek process exited"))
	}()

	// Drive the native thread lifecycle in a goroutine.
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer func() {
			stdin.Close()
			_ = cmd.Wait()
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var threadID string

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		// 1. Create or resume a thread.
		if opts.ResumeSessionID != "" {
			// Resume an existing thread.
			_, err := c.request(runCtx, "thread/resume", map[string]any{
				"thread_id": opts.ResumeSessionID,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("deepseek thread/resume failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			threadID = opts.ResumeSessionID
		} else {
			result, err := c.request(runCtx, "thread/start", map[string]any{
				"cwd": cwd,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("deepseek thread/start failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			threadID = extractDeepseekThreadID(result)
			if threadID == "" {
				finalStatus = "failed"
				finalError = "deepseek thread/start returned no thread_id"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
		}

		b.cfg.Logger.Info("deepseek thread created", "thread_id", threadID)

		// 2. Build the prompt content.
		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		// 3. Send the message. This blocks until the LLM finishes.
		//    Push events (response_delta, tool_lifecycle) arrive on
		//    stdout DURING this call and are handled by the reader
		//    goroutine.
		_, err = c.request(runCtx, "thread/message", map[string]any{
			"thread_id": threadID,
			"input":     userText,
		})
		if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("deepseek timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("deepseek thread/message failed: %v", err)
			}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("deepseek finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		// Shut down the process cleanly.
		_ = c.writeLine([]byte(`{"jsonrpc":"2.0","id":99999,"method":"shutdown","params":{}}` + "\n"))
		stdin.Close()
		cancel()

		<-readerDone

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		if finalStatus == "completed" && finalOutput == "" {
			if msg := providerErr.message(); msg != "" {
				finalStatus = "failed"
				finalError = msg
			}
		}

		c.usageMu.Lock()
		u := c.usage
		c.usageMu.Unlock()

		var usageMap map[string]TokenUsage
		if tokenUsageHasTokens(u) {
			model := opts.Model
			if model == "" {
				model = "unknown"
			}
			usageMap = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  threadID,
			Usage:      usageMap,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// extractDeepseekThreadID pulls the thread_id from a thread/start
// JSON-RPC result.
func extractDeepseekThreadID(data json.RawMessage) string {
	var result struct {
		ThreadID string `json:"thread_id"`
	}
	_ = json.Unmarshal(data, &result)
	return result.ThreadID
}

// deepseekToolName normalises tool names from DeepSeek-TUI's
// tool_lifecycle events into the snake_case identifiers the Multica
// UI expects. DeepSeek-TUI tool names are already snake_case (e.g.
// "read_file", "shell"), but we normalise common variants.
func deepseekToolName(name string) string {
	n := strings.TrimSpace(name)
	if n == "" {
		return ""
	}

	lower := strings.ToLower(n)
	switch lower {
	case "read", "read_file":
		return "read_file"
	case "write", "write_file":
		return "write_file"
	case "edit", "edit_file", "patch":
		return "edit_file"
	case "shell", "bash", "terminal", "run_command":
		return "terminal"
	case "search", "grep", "find", "search_files":
		return "search_files"
	case "glob":
		return "glob"
	case "web_search":
		return "web_search"
	case "web_fetch", "fetch":
		return "web_fetch"
	case "todo", "todo_write":
		return "todo_write"
	}

	return strings.ReplaceAll(lower, " ", "_")
}
