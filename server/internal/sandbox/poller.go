package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// SessionState represents the current state of an OpenCode session.
type SessionState string

const (
	SessionRunning SessionState = "running"
	SessionIdle    SessionState = "idle"    // completed
	SessionError   SessionState = "error"
	SessionTimeout SessionState = "timeout"
)

// TaskMessage represents a structured message from the agent execution,
// mapped to Multica's TaskMessagePayload format.
type TaskMessage struct {
	Seq     int    `json:"seq"`
	Type    string `json:"type"`    // "text", "tool-use", "tool-result", "error", "status"
	Tool    string `json:"tool"`
	Content string `json:"content"`
	Input   string `json:"input"`
	Output  string `json:"output"`
}

// TokenUsage tracks token consumption across model invocations.
type TokenUsage struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
}

// SessionStatus is the result of a single poll cycle.
type SessionStatus struct {
	State    SessionState
	Messages []TaskMessage
	Usage    TokenUsage
	Error    string
}

// SessionPoller polls an OpenCode serve instance for session status and messages.
type SessionPoller struct {
	Provider SandboxProvider
	Sandbox  *Sandbox

	// PollInterval between polls. Default: 10s.
	PollInterval time.Duration

	// Timeout for considering a session stale (no messages). Default: 9000s.
	Timeout time.Duration

	lastPartCount int  // total parts seen so far (for deduplication across polls)
	lastActivity  time.Time
}

// NewSessionPoller creates a poller for a specific sandbox session.
func NewSessionPoller(provider SandboxProvider, sb *Sandbox) *SessionPoller {
	return &SessionPoller{
		Provider:     provider,
		Sandbox:      sb,
		PollInterval: 10 * time.Second,
		Timeout:      9000 * time.Second,
		lastActivity: time.Now(),
	}
}

// PollOnce performs a single poll of the session status.
func (p *SessionPoller) PollOnce(ctx context.Context, sessionID string) (*SessionStatus, error) {
	stdout, err := p.Provider.Exec(ctx, p.Sandbox, []string{
		"curl", "-s", fmt.Sprintf("http://localhost:4096/session/%s/message", sessionID),
	})
	if err != nil {
		return nil, fmt.Errorf("poll session: %w", err)
	}

	return p.parseResponse(stdout)
}

// WatchSession polls the session in a loop until completion, error, timeout, or context cancellation.
// The callback is called with each new SessionStatus that contains messages.
// Returns the final status.
func (p *SessionPoller) WatchSession(ctx context.Context, sessionID string, callback func(*SessionStatus)) (*SessionStatus, error) {
	ticker := time.NewTicker(p.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return &SessionStatus{State: SessionError, Error: "context cancelled"}, ctx.Err()
		case <-ticker.C:
			status, err := p.PollOnce(ctx, sessionID)
			if err != nil {
				// Poll failure is transient — don't immediately fail the task
				if time.Since(p.lastActivity) > p.Timeout {
					return &SessionStatus{State: SessionTimeout, Error: "session timeout: no activity"}, nil
				}
				continue
			}

			if len(status.Messages) > 0 {
				p.lastActivity = time.Now()
				if callback != nil {
					callback(status)
				}
			}

			switch status.State {
			case SessionIdle:
				return status, nil
			case SessionError:
				return status, nil
			case SessionRunning:
				if time.Since(p.lastActivity) > p.Timeout {
					return &SessionStatus{State: SessionTimeout, Error: "session timeout: no activity"}, nil
				}
			}
		}
	}
}

// parseResponse parses the OpenCode /session/{id}/message response.
// The API returns a JSON array of messages directly (not wrapped in an object).
// Each message has {info: {role, tokens, finish}, parts: [...]}.
// State is determined by the last assistant message's step-finish part or info.finish field.
func (p *SessionPoller) parseResponse(raw string) (*SessionStatus, error) {
	if raw == "" || raw == "[]" {
		return &SessionStatus{State: SessionRunning}, nil
	}

	var messages []sessionMessage
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		return nil, fmt.Errorf("parse session response: %w", err)
	}

	status := &SessionStatus{State: SessionRunning}
	var taskMessages []TaskMessage

	// Count all assistant parts to compare against lastPartCount for deduplication.
	// The API returns all messages every time, so we skip parts we've already seen.
	partIndex := 0

	for _, msg := range messages {
		// Skip user messages for task message forwarding
		if msg.Info.Role == "user" {
			continue
		}

		for _, part := range msg.Parts {
			partIndex++
			isNew := partIndex > p.lastPartCount
			seq := partIndex

			switch part.Type {
			case "text":
				if isNew && part.Text != "" {
					taskMessages = append(taskMessages, TaskMessage{
						Seq:     seq,
						Type:    "text",
						Content: part.Text,
					})
				}
			case "reasoning":
				// OpenCode serve exposes reasoning/thinking content
				if isNew && part.Text != "" {
					taskMessages = append(taskMessages, TaskMessage{
						Seq:     seq,
						Type:    "text",
						Content: part.Text,
					})
				}
			case "tool":
				// "tool" type in serve API = tool invocation with state
				if isNew {
					inputStr := ""
					if part.Input != nil {
						b, _ := json.Marshal(part.Input)
						inputStr = string(b)
					}
					toolName := part.ToolName
					if toolName == "" {
						toolName = part.Name
					}
					msg := TaskMessage{
						Seq:  seq,
						Type: "tool-use",
						Tool: toolName,
					}
					if inputStr != "" {
						msg.Input = inputStr
					}
					// If tool has result, also include it
					if part.Result != nil {
						msg.Type = "tool-result"
						msg.Output = fmt.Sprintf("%v", part.Result)
					}
					taskMessages = append(taskMessages, msg)
				}
			case "tool-invocation":
				if isNew {
					inputStr := ""
					if part.Input != nil {
						b, _ := json.Marshal(part.Input)
						inputStr = string(b)
					}
					taskMessages = append(taskMessages, TaskMessage{
						Seq:   seq,
						Type:  "tool-use",
						Tool:  part.ToolName,
						Input: inputStr,
					})
				}
			case "tool-result":
				if isNew {
					taskMessages = append(taskMessages, TaskMessage{
						Seq:    seq,
						Type:   "tool-result",
						Tool:   part.ToolName,
						Output: fmt.Sprintf("%v", part.Result),
					})
				}
			case "step-start":
				if isNew {
					taskMessages = append(taskMessages, TaskMessage{
						Seq:  seq,
						Type: "status",
						Content: "running",
					})
				}
			case "step-finish":
				// Always process state + tokens (even for already-seen parts, to detect completion)
				// Token usage from step-finish part
				if part.Tokens != nil {
					status.Usage.InputTokens += part.Tokens.Input
					status.Usage.OutputTokens += part.Tokens.Output
					if part.Tokens.Cache != nil {
						status.Usage.CacheReadTokens += part.Tokens.Cache.Read
						status.Usage.CacheWriteTokens += part.Tokens.Cache.Write
					}
				}
				// State detection: "stop" or "end_turn" = completed, "error" = error
				switch part.FinishReason {
				case "stop", "end_turn":
					status.State = SessionIdle
				case "error":
					status.State = SessionError
					status.Error = "agent error"
				}
			}
		}

		// Also check info-level tokens (sometimes richer than step-finish tokens)
		if msg.Info.Tokens != nil && msg.Info.Role == "assistant" {
			// info.tokens is per-message total; step-finish.tokens is per-step.
			// We prefer step-finish (already accumulated above) so only use info as fallback.
			if status.Usage.InputTokens == 0 {
				status.Usage.InputTokens = msg.Info.Tokens.Input
				status.Usage.OutputTokens = msg.Info.Tokens.Output
				if msg.Info.Tokens.Cache != nil {
					status.Usage.CacheReadTokens = msg.Info.Tokens.Cache.Read
					status.Usage.CacheWriteTokens = msg.Info.Tokens.Cache.Write
				}
			}
		}

		// Check info.finish as fallback for state detection
		if msg.Info.Finish == "stop" && status.State == SessionRunning {
			status.State = SessionIdle
		}
	}

	// Update the high-water mark so next poll skips already-seen parts
	p.lastPartCount = partIndex

	status.Messages = taskMessages
	return status, nil
}

// --- JSON types for OpenCode serve /session/{id}/message response ---
// Verified against opencode v1.2.27 actual API output.

// sessionMessage represents a single message in the session.
// The API returns an array of these directly (not wrapped in an object).
type sessionMessage struct {
	Info  sessionMessageInfo `json:"info"`
	Parts []sessionPart      `json:"parts"`
}

type sessionMessageInfo struct {
	Role       string      `json:"role"` // "user" or "assistant"
	Finish     string      `json:"finish,omitempty"` // "stop", "error", etc.
	Tokens     *partTokens `json:"tokens,omitempty"`
	ID         string      `json:"id,omitempty"`
	SessionID  string      `json:"sessionID,omitempty"`
}

type sessionPart struct {
	Type         string      `json:"type"` // "text", "reasoning", "tool", "tool-invocation", "tool-result", "step-start", "step-finish"
	Text         string      `json:"text,omitempty"`
	Name         string      `json:"name,omitempty"`     // tool name for "tool" type parts
	ToolName     string      `json:"toolName,omitempty"` // tool name for "tool-invocation"/"tool-result"
	Input        any         `json:"input,omitempty"`
	Result       any         `json:"result,omitempty"`
	FinishReason string      `json:"reason,omitempty"` // "stop", "end_turn", "error", "tool-calls"
	Tokens       *partTokens `json:"tokens,omitempty"`
}

type partTokens struct {
	Input  int64        `json:"input"`
	Output int64        `json:"output"`
	Total  int64        `json:"total,omitempty"`
	Cache  *cacheTokens `json:"cache,omitempty"`
}

type cacheTokens struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}
