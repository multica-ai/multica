package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type aoneCloudCLIBackend struct {
	cfg Config
}

type aoneRuntimeExecuteRequest struct {
	Prompt         string            `json:"prompt"`
	Workspace      aoneWorkspace     `json:"workspace"`
	SessionID      string            `json:"sessionId,omitempty"`
	AgentProfile   string            `json:"agentProfile,omitempty"`
	ModelHint      string            `json:"modelHint,omitempty"`
	SystemPrompt   string            `json:"systemPrompt,omitempty"`
	PermissionMode string            `json:"permissionMode,omitempty"`
	McpConfig      json.RawMessage   `json:"mcpConfig,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type aoneWorkspace struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

type aoneRuntimeEvent struct {
	Type       string                     `json:"type"`
	Message    json.RawMessage            `json:"message"`
	Status     string                     `json:"status"`
	Output     string                     `json:"output"`
	Error      string                     `json:"error"`
	SessionID  string                     `json:"sessionId"`
	DurationMs int64                      `json:"durationMs"`
	Usage      map[string]json.RawMessage `json:"usage"`
}

type aoneNormalizedMessage struct {
	Kind            string `json:"kind"`
	Content         string `json:"content"`
	Text            string `json:"text"`
	Status          string `json:"status"`
	SessionID       string `json:"sessionId"`
	NewSessionID    string `json:"newSessionId"`
	ActualSessionID string `json:"actualSessionId"`
	ToolName        string `json:"toolName"`
	ToolID          string `json:"toolId"`
	ToolInput       any    `json:"toolInput"`
	IsError         bool   `json:"isError"`
}

func (b *aoneCloudCLIBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	baseURL, err := normalizeAoneRuntimeBaseURL(b.cfg.ExecutablePath)
	if err != nil {
		return nil, err
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		var output strings.Builder
		var sessionID string
		var lastError string

		reqBody := aoneRuntimeExecuteRequest{
			Prompt: prompt,
			Workspace: aoneWorkspace{
				Kind: "local_path",
				Path: opts.Cwd,
			},
			SessionID:      opts.ResumeSessionID,
			AgentProfile:   b.cfg.Env["AONE_RUNTIME_PROFILE"],
			ModelHint:      opts.Model,
			SystemPrompt:   opts.SystemPrompt,
			PermissionMode: "controlled",
			McpConfig:      opts.McpConfig,
			Metadata:       aoneMetadataFromEnv(b.cfg.Env),
		}

		body, err := json.Marshal(reqBody)
		if err != nil {
			resCh <- Result{Status: "failed", Error: fmt.Sprintf("marshal aone runtime request: %v", err), DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		req, err := http.NewRequestWithContext(runCtx, http.MethodPost, baseURL+"/api/runtime/execute", bytes.NewReader(body))
		if err != nil {
			resCh <- Result{Status: "failed", Error: fmt.Sprintf("build aone runtime request: %v", err), DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/x-ndjson")
		setAoneRuntimeAuthHeaders(req, b.cfg.Env["AONE_RUNTIME_TOKEN"])

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			resCh <- aoneContextResult(runCtx, "call aone runtime", err, time.Since(startTime))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			tail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resCh <- Result{
				Status:     "failed",
				Error:      fmt.Sprintf("aone runtime returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(tail))),
				DurationMs: time.Since(startTime).Milliseconds(),
			}
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			result, done := b.handleRuntimeLine(line, msgCh, &output, &sessionID, &lastError, startTime)
			if done {
				resCh <- result
				return
			}
		}
		if err := scanner.Err(); err != nil {
			resCh <- aoneContextResult(runCtx, "read aone runtime stream", err, time.Since(startTime))
			return
		}

		status := "completed"
		if lastError != "" {
			status = "failed"
		}
		resCh <- Result{
			Status:     status,
			Output:     output.String(),
			Error:      lastError,
			DurationMs: time.Since(startTime).Milliseconds(),
			SessionID:  sessionID,
			Usage:      map[string]TokenUsage{},
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func (b *aoneCloudCLIBackend) handleRuntimeLine(line string, msgCh chan<- Message, output *strings.Builder, sessionID *string, lastError *string, started time.Time) (Result, bool) {
	var event aoneRuntimeEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		*lastError = fmt.Sprintf("parse aone runtime event: %v", err)
		trySend(msgCh, Message{Type: MessageError, Content: *lastError})
		return Result{}, false
	}

	switch event.Type {
	case "message":
		b.handleRuntimeMessage(event.Message, msgCh, output, sessionID, lastError)
		return Result{}, false
	case "result":
		if event.SessionID != "" {
			*sessionID = event.SessionID
		}
		status := event.Status
		if status == "" {
			status = "completed"
		}
		resultOutput := event.Output
		if resultOutput == "" {
			resultOutput = output.String()
		}
		resultError := event.Error
		if resultError == "" {
			resultError = *lastError
		}
		duration := event.DurationMs
		if duration == 0 {
			duration = time.Since(started).Milliseconds()
		}
		return Result{
			Status:     status,
			Output:     resultOutput,
			Error:      resultError,
			DurationMs: duration,
			SessionID:  *sessionID,
			Usage:      parseAoneUsage(event.Usage),
		}, true
	default:
		b.handleRuntimeMessage([]byte(line), msgCh, output, sessionID, lastError)
		return Result{}, false
	}
}

func (b *aoneCloudCLIBackend) handleRuntimeMessage(raw json.RawMessage, msgCh chan<- Message, output *strings.Builder, sessionID *string, lastError *string) {
	if len(raw) == 0 {
		return
	}
	var msg aoneNormalizedMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		var text string
		if err := json.Unmarshal(raw, &text); err == nil && text != "" {
			output.WriteString(text)
			trySend(msgCh, Message{Type: MessageText, Content: text})
		}
		return
	}

	if msg.ActualSessionID != "" {
		*sessionID = msg.ActualSessionID
	} else if msg.NewSessionID != "" {
		*sessionID = msg.NewSessionID
	} else if msg.SessionID != "" {
		*sessionID = msg.SessionID
	}

	switch msg.Kind {
	case "session_created":
		trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: *sessionID})
	case "status":
		status := msg.Text
		if status == "" {
			status = msg.Status
		}
		if status == "" {
			status = "running"
		}
		trySend(msgCh, Message{Type: MessageStatus, Status: status, SessionID: *sessionID})
	case "text", "stream_delta":
		if msg.Content != "" {
			output.WriteString(msg.Content)
			trySend(msgCh, Message{Type: MessageText, Content: msg.Content, SessionID: *sessionID})
		}
	case "thinking":
		if msg.Content != "" {
			trySend(msgCh, Message{Type: MessageThinking, Content: msg.Content, SessionID: *sessionID})
		}
	case "tool_use":
		trySend(msgCh, Message{
			Type:      MessageToolUse,
			Tool:      msg.ToolName,
			CallID:    msg.ToolID,
			Input:     aoneToolInput(msg.ToolInput),
			SessionID: *sessionID,
		})
	case "tool_result":
		trySend(msgCh, Message{
			Type:      MessageToolResult,
			CallID:    msg.ToolID,
			Output:    msg.Content,
			SessionID: *sessionID,
		})
	case "error":
		*lastError = msg.Content
		trySend(msgCh, Message{Type: MessageError, Content: msg.Content, SessionID: *sessionID})
	}
}

func DetectAoneRuntimeVersion(ctx context.Context, endpoint, token string) (string, error) {
	baseURL, err := normalizeAoneRuntimeBaseURL(endpoint)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/runtime/health", nil)
	if err != nil {
		return "", err
	}
	setAoneRuntimeAuthHeaders(req, token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		tail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("aone runtime health returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(tail)))
	}

	var health struct {
		Version string `json:"version"`
		Runtime struct {
			Version string `json:"version"`
		} `json:"runtime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return "", err
	}
	if strings.TrimSpace(health.Version) != "" {
		return strings.TrimSpace(health.Version), nil
	}
	if strings.TrimSpace(health.Runtime.Version) != "" {
		return strings.TrimSpace(health.Runtime.Version), nil
	}
	return "unknown", nil
}

func normalizeAoneRuntimeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("aone_cloud_cli runtime URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid aone_cloud_cli runtime URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("aone_cloud_cli runtime URL must use http or https")
	}
	u.RawQuery = ""
	u.Fragment = ""
	base := strings.TrimRight(u.String(), "/")
	base = strings.TrimSuffix(base, "/api/runtime")
	base = strings.TrimSuffix(base, "/api")
	return base, nil
}

func setAoneRuntimeAuthHeaders(req *http.Request, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Runtime-Token", token)
	req.Header.Set("X-API-Key", token)
}

func aoneMetadataFromEnv(env map[string]string) map[string]string {
	metadata := map[string]string{"source": "multica"}
	for _, key := range []string{
		"MULTICA_TASK_ID",
		"MULTICA_AGENT_ID",
		"MULTICA_AGENT_NAME",
		"MULTICA_WORKSPACE_ID",
		"MULTICA_AUTOPILOT_RUN_ID",
		"MULTICA_AUTOPILOT_ID",
	} {
		if value := strings.TrimSpace(env[key]); value != "" {
			metadata[key] = value
		}
	}
	return metadata
}

func aoneToolInput(value any) map[string]any {
	if value == nil {
		return nil
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{"value": value}
}

func parseAoneUsage(raw map[string]json.RawMessage) map[string]TokenUsage {
	if len(raw) == 0 {
		return map[string]TokenUsage{}
	}
	out := make(map[string]TokenUsage, len(raw))
	for model, data := range raw {
		var fields map[string]int64
		if err := json.Unmarshal(data, &fields); err != nil {
			continue
		}
		out[model] = TokenUsage{
			InputTokens:      firstInt64(fields, "inputTokens", "input_tokens"),
			OutputTokens:     firstInt64(fields, "outputTokens", "output_tokens"),
			CacheReadTokens:  firstInt64(fields, "cacheReadTokens", "cache_read_tokens"),
			CacheWriteTokens: firstInt64(fields, "cacheWriteTokens", "cache_write_tokens"),
		}
	}
	return out
}

func firstInt64(fields map[string]int64, keys ...string) int64 {
	for _, key := range keys {
		if value, ok := fields[key]; ok {
			return value
		}
	}
	return 0
}

func aoneContextResult(ctx context.Context, prefix string, err error, duration time.Duration) Result {
	status := "failed"
	message := fmt.Sprintf("%s: %v", prefix, err)
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = "timeout"
			message = fmt.Sprintf("%s timed out: %v", prefix, err)
		} else if errors.Is(ctx.Err(), context.Canceled) {
			status = "cancelled"
			message = fmt.Sprintf("%s cancelled: %v", prefix, err)
		}
	}
	return Result{
		Status:     status,
		Error:      message,
		DurationMs: duration.Milliseconds(),
		Usage:      map[string]TokenUsage{},
	}
}
