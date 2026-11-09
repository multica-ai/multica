package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// rateLimitProbeTimeout bounds one idle usage check. Both CLIs answer in
// about a second; the rest covers a slow network call to the provider.
const rateLimitProbeTimeout = 45 * time.Second

// claudeUsageProbeArgs open a control-only session: no prompt is sent, so
// the check uses no quota. Hooks, MCP servers and slash commands stay off so
// the check does not run the user's session tooling every 15 minutes.
var claudeUsageProbeArgs = []string{
	"--print",
	"--verbose",
	"--input-format", "stream-json",
	"--output-format", "stream-json",
	"--no-session-persistence",
	"--strict-mcp-config",
	"--disable-slash-commands",
	"--settings", `{"disableAllHooks":true}`,
}

const (
	claudeUsageInitRequestID  = "multica-usage-init"
	claudeUsageUsageRequestID = "multica-usage"
)

// ProbeClaudeRateLimits asks Claude Code for the plan limits of the account
// it is logged in to, through the experimental get_usage control request.
func ProbeClaudeRateLimits(ctx context.Context, runtimeCmd Command) (RateLimitSnapshot, error) {
	if runtimeCmd.Path == "" {
		runtimeCmd.Path = "claude"
	}
	ctx, cancel := context.WithTimeout(ctx, rateLimitProbeTimeout)
	defer cancel()

	var input bytes.Buffer
	for _, request := range []map[string]any{
		{"type": "control_request", "request_id": claudeUsageInitRequestID, "request": map[string]any{"subtype": "initialize"}},
		{"type": "control_request", "request_id": claudeUsageUsageRequestID, "request": map[string]any{"subtype": "get_usage", "skip_behaviors": true}},
	} {
		line, err := json.Marshal(request)
		if err != nil {
			return RateLimitSnapshot{}, err
		}
		input.Write(append(line, '\n'))
	}

	cmd := runtimeCmd.exec(ctx, claudeUsageProbeArgs...)
	// Claude answers both requests before it acts on stdin EOF, so a reader
	// makes this a one-shot probe like list_models.
	cmd.Stdin = &input
	cmd.Env = mergeEnv(cmd.Environ(), map[string]string{"ENABLE_CLAUDEAI_MCP_SERVERS": "false"})
	hideAgentWindow(cmd)
	raw, err := outputOwned(cmd, runtimeCmd.logger)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return RateLimitSnapshot{}, fmt.Errorf("claude usage check: %w", ctxErr)
	}
	snapshot, parseErr := parseClaudeUsageProbeOutput(raw)
	if parseErr != nil {
		if err != nil {
			return RateLimitSnapshot{}, fmt.Errorf("claude usage check: %w", err)
		}
		return RateLimitSnapshot{}, parseErr
	}
	return snapshot, nil
}

type claudeUsageControlResponse struct {
	Type     string `json:"type"`
	Response struct {
		Subtype   string          `json:"subtype"`
		RequestID string          `json:"request_id"`
		Response  json.RawMessage `json:"response"`
		Error     string          `json:"error"`
	} `json:"response"`
}

func parseClaudeUsageProbeOutput(raw []byte) (RateLimitSnapshot, error) {
	var accountIdentity string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var message claudeUsageControlResponse
		if json.Unmarshal([]byte(line), &message) != nil || message.Type != "control_response" {
			continue
		}
		switch message.Response.RequestID {
		case claudeUsageInitRequestID:
			var init struct {
				Account struct {
					Email string `json:"email"`
				} `json:"account"`
			}
			if json.Unmarshal(message.Response.Response, &init) == nil {
				accountIdentity = init.Account.Email
			}
		case claudeUsageUsageRequestID:
			if message.Response.Subtype != "success" {
				return RateLimitSnapshot{}, fmt.Errorf("claude get_usage failed: %s", strings.TrimSpace(message.Response.Error))
			}
			usage, err := ParseClaudeUsageResponse(message.Response.Response)
			if err != nil {
				return RateLimitSnapshot{}, fmt.Errorf("claude get_usage: %w", err)
			}
			if !usage.Available {
				return RateLimitSnapshot{}, errors.New("claude login has no plan limits")
			}
			return RateLimitSnapshot{
				Provider:  "claude",
				AccountID: RateLimitAccountID("claude", accountIdentity),
				Plan:      usage.Plan,
				Windows:   usage.Windows,
				Source:    RateLimitSourceProbe,
			}, nil
		}
	}
	return RateLimitSnapshot{}, errors.New("claude usage check: no get_usage response")
}

const codexRateLimitsRequestID = 2

// ProbeCodexRateLimits reads the rate limits of the Codex login through
// account/rateLimits/read on a short-lived app-server.
func ProbeCodexRateLimits(ctx context.Context, runtimeCmd Command) (RateLimitSnapshot, error) {
	if runtimeCmd.Path == "" {
		runtimeCmd.Path = "codex"
	}
	ctx, cancel := context.WithTimeout(ctx, rateLimitProbeTimeout)
	defer cancel()

	// app-server exits on stdin EOF without finishing an in-flight read, so
	// stdin stays open until the answer arrives.
	stdinReader, stdinWriter := io.Pipe()
	output := newCodexProbeOutput(codexRateLimitsRequestID)
	cmd := runtimeCmd.exec(ctx, "app-server")
	cmd.Stdin = stdinReader
	cmd.Stdout = output
	hideAgentWindow(cmd)

	go func() {
		for _, request := range []map[string]any{
			{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]any{"name": "multica-usage", "version": "1"}}},
			{"method": "initialized"},
			{"id": codexRateLimitsRequestID, "method": "account/rateLimits/read"},
		} {
			line, _ := json.Marshal(request)
			if _, err := stdinWriter.Write(append(line, '\n')); err != nil {
				return
			}
		}
	}()
	go func() {
		select {
		case <-output.done:
		case <-ctx.Done():
		}
		_ = stdinWriter.Close()
	}()

	runErr := runOwned(cmd, runtimeCmd.logger)
	_ = stdinWriter.Close()
	result, rpcErr, answered := output.answer()
	switch {
	case !answered && ctx.Err() != nil:
		return RateLimitSnapshot{}, fmt.Errorf("codex usage check: %w", ctx.Err())
	case !answered && runErr != nil:
		return RateLimitSnapshot{}, fmt.Errorf("codex usage check: %w", runErr)
	case !answered:
		return RateLimitSnapshot{}, errors.New("codex usage check: no account/rateLimits/read response")
	case rpcErr != "":
		return RateLimitSnapshot{}, fmt.Errorf("codex account/rateLimits/read failed: %s", rpcErr)
	}
	limits, ok := ParseCodexRateLimits(result)
	if !ok {
		return RateLimitSnapshot{}, errors.New("codex usage check: response has no rate-limit windows")
	}
	return RateLimitSnapshot{
		Provider:  "codex",
		AccountID: limits.AccountID,
		Plan:      limits.Plan,
		Windows:   limits.Windows,
		Source:    RateLimitSourceProbe,
	}, nil
}

// codexProbeOutput scans app-server stdout for one JSON-RPC response.
type codexProbeOutput struct {
	requestID int
	done      chan struct{}

	mu       sync.Mutex
	pending  []byte
	answered bool
	result   json.RawMessage
	rpcError string
}

func newCodexProbeOutput(requestID int) *codexProbeOutput {
	return &codexProbeOutput{requestID: requestID, done: make(chan struct{})}
}

func (o *codexProbeOutput) Write(chunk []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.answered {
		return len(chunk), nil
	}
	o.pending = append(o.pending, chunk...)
	for {
		newline := bytes.IndexByte(o.pending, '\n')
		if newline < 0 {
			return len(chunk), nil
		}
		line := o.pending[:newline]
		o.pending = o.pending[newline+1:]
		var message struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(line, &message) != nil || message.ID == nil || *message.ID != o.requestID {
			continue
		}
		o.answered = true
		o.result = append(json.RawMessage(nil), message.Result...)
		if message.Error != nil {
			o.rpcError = message.Error.Message
			if o.rpcError == "" {
				o.rpcError = "unknown error"
			}
		}
		o.pending = nil
		close(o.done)
		return len(chunk), nil
	}
}

func (o *codexProbeOutput) answer() (json.RawMessage, string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.result, o.rpcError, o.answered
}
