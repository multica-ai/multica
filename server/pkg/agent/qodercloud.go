package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultQoderCloudBaseURL     = "https://api.qoder.com/api/v1/cloud"
	qoderCloudMaxResponseBytes   = 8 << 20
	qoderCloudMaxSSEFrameBytes   = 16 << 20
	qoderCloudMaxToolResultBytes = 64 << 10
	qoderCloudMaxReconnects      = 5
	qoderCloudCancelTimeout      = 5 * time.Second
)

var qoderCloudReconnectDelay = 100 * time.Millisecond

// qoderCloudBackend implements the built-in Qoder Cloud Agents provider over
// the hosted HTTP/SSE API. Unlike qoderBackend, it starts no local executable.
type qoderCloudBackend struct {
	cfg Config
}

type qoderCloudClient struct {
	baseURL    string
	pat        string
	httpClient *http.Client
}

type qoderCloudHTTPError struct {
	operation  string
	status     int
	body       string
	retryAfter time.Duration
}

func (e *qoderCloudHTTPError) Error() string {
	if e.body == "" {
		return fmt.Sprintf("qoder cloud %s returned HTTP %d", e.operation, e.status)
	}
	return fmt.Sprintf("qoder cloud %s returned HTTP %d: %s", e.operation, e.status, e.body)
}

type qoderCloudAgent struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type qoderCloudSession struct {
	ID         string          `json:"id"`
	Status     string          `json:"status"`
	ArchivedAt json.RawMessage `json:"archived_at"`
}

// qoderCloudUnrecoverableResumeError is positive provider evidence that a
// stored session can never accept another turn. The daemon uses this typed
// outcome to discard the poisoned pointer and retry once with a fresh session.
type qoderCloudUnrecoverableResumeError struct {
	reason string
}

func (e *qoderCloudUnrecoverableResumeError) Error() string {
	return "qoder cloud resume session cannot be continued: " + e.reason
}

// qoderCloudResumeBusyError is intentionally distinct from an unrecoverable
// resume. A running/canceling/rescheduling session may become idle later, so a
// platform retry should keep the session pointer instead of losing context.
type qoderCloudResumeBusyError struct {
	status string
}

func (e *qoderCloudResumeBusyError) Error() string {
	return fmt.Sprintf("qoder cloud resume session is temporarily unavailable (status %q)", e.status)
}

// qoderCloudUnsupportedPendingActionError means the hosted Agent paused for a
// permission decision Multica cannot safely answer. The backend must never
// auto-allow it: fail the turn and cancel the remote session best-effort.
type qoderCloudUnsupportedPendingActionError struct {
	reason string
}

func (e *qoderCloudUnsupportedPendingActionError) Error() string {
	return "qoder cloud requested an unsupported interactive action: " + e.reason
}

type qoderCloudTransportError struct {
	operation string
	message   string
}

func (e *qoderCloudTransportError) Error() string {
	return "qoder cloud " + e.operation + ": " + e.message
}

type qoderCloudMalformedSSEError struct {
	eventType string
}

func (e *qoderCloudMalformedSSEError) Error() string {
	return fmt.Sprintf("qoder cloud received malformed SSE JSON for event type %q", e.eventType)
}

type qoderCloudUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

type qoderCloudEventError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

type qoderCloudStopReason struct {
	Type     string   `json:"type"`
	EventIDs []string `json:"event_ids"`
}

type qoderCloudEvent struct {
	ID                   string               `json:"id"`
	Type                 string               `json:"type"`
	SessionID            string               `json:"session_id"`
	TurnID               string               `json:"turn_id"`
	Status               string               `json:"status"`
	Content              json.RawMessage      `json:"content"`
	Name                 string               `json:"name"`
	Input                json.RawMessage      `json:"input"`
	Result               json.RawMessage      `json:"result"`
	ToolName             string               `json:"tool_name"`
	ToolInput            json.RawMessage      `json:"tool_input"`
	ToolUseID            string               `json:"tool_use_id"`
	CustomToolUseID      string               `json:"custom_tool_use_id"`
	EvaluatedPermission  json.RawMessage      `json:"evaluated_permission"`
	RequiresConfirmation bool                 `json:"requires_confirmation"`
	ConfirmationRequired bool                 `json:"confirmation_required"`
	Usage                qoderCloudUsage      `json:"usage"`
	StopReason           qoderCloudStopReason `json:"stop_reason"`
	Error                qoderCloudEventError `json:"error"`
}

type qoderCloudSSEEvent struct {
	ID   string
	Type string
	Data []byte
}

type qoderCloudTurnState struct {
	turnID      string
	lastEventID string
	seen        map[string]struct{}
	output      strings.Builder
	usage       map[string]TokenUsage
	processed   int
	customTools map[string]*qoderCloudCachedCustomToolResult
}

type qoderCloudCachedCustomToolResult struct {
	name        string
	input       map[string]any
	inputErr    error
	result      CustomToolResult
	resultReady bool
	sent        bool
}

func (b *qoderCloudBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	client, cfg, err := newQoderCloudClient(b.cfg.QoderCloud)
	if err != nil {
		return nil, err
	}

	runCtx, cancel := runContext(ctx, opts.Timeout)
	msgCh := make(chan Message, 256)
	resultCh := make(chan Result, 1)

	go func() {
		defer cancel()
		started := time.Now()
		result := b.executeTurn(runCtx, client, cfg, prompt, opts, msgCh)
		result.DurationMs = time.Since(started).Milliseconds()
		close(msgCh)
		resultCh <- result
		close(resultCh)
	}()

	return &Session{Messages: msgCh, Result: resultCh}, nil
}

func newQoderCloudClient(cfg QoderCloudConfig) (*qoderCloudClient, QoderCloudConfig, error) {
	cfg.PAT = strings.TrimSpace(cfg.PAT)
	cfg.AgentID = strings.TrimSpace(cfg.AgentID)
	cfg.EnvironmentID = strings.TrimSpace(cfg.EnvironmentID)
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	if cfg.PAT == "" {
		return nil, cfg, errors.New("qoder cloud PAT is required")
	}
	if cfg.AgentID == "" {
		return nil, cfg, errors.New("qoder cloud agent ID is required")
	}
	if cfg.EnvironmentID == "" {
		return nil, cfg, errors.New("qoder cloud environment ID is required")
	}
	if cfg.AgentVersion < 0 {
		return nil, cfg, errors.New("qoder cloud agent version must not be negative")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultQoderCloudBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, cfg, errors.New("qoder cloud base URL must be an http(s) URL without credentials, query, or fragment")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &qoderCloudClient{baseURL: cfg.BaseURL, pat: cfg.PAT, httpClient: httpClient}, cfg, nil
}

func (b *qoderCloudBackend) executeTurn(
	ctx context.Context,
	client *qoderCloudClient,
	cfg QoderCloudConfig,
	prompt string,
	opts ExecOptions,
	msgCh chan<- Message,
) Result {
	sessionID := strings.TrimSpace(opts.ResumeSessionID)
	if sessionID != "" {
		if err := client.getSession(ctx, sessionID); err != nil {
			var httpErr *qoderCloudHTTPError
			if errors.As(err, &httpErr) && httpErr.status == http.StatusNotFound {
				return Result{
					Status:         "failed",
					Error:          "qoder cloud resume session was not found",
					SessionID:      sessionID,
					ResumeRejected: true,
				}
			}
			var unrecoverable *qoderCloudUnrecoverableResumeError
			if errors.As(err, &unrecoverable) {
				return Result{
					Status:         "failed",
					Error:          unrecoverable.Error(),
					SessionID:      sessionID,
					ResumeRejected: true,
				}
			}
			return b.failureResult(ctx, client, sessionID, err)
		}
	} else {
		version := cfg.AgentVersion
		if version == 0 {
			var err error
			version, err = client.getAgentVersion(ctx, cfg.AgentID)
			if err != nil {
				return b.failureResult(ctx, client, "", err)
			}
		}
		var err error
		sessionID, err = client.createSession(ctx, cfg.AgentID, version, cfg.EnvironmentID)
		if err != nil {
			return b.failureResult(ctx, client, sessionID, err)
		}
	}

	trySend(msgCh, Message{Type: MessageStatus, Status: "session_started", SessionID: sessionID})

	eventID, turnID, err := client.sendMessages(ctx, sessionID, prompt, opts.SystemPrompt)
	if err != nil {
		return b.failureResult(ctx, client, sessionID, err)
	}

	state := &qoderCloudTurnState{
		turnID:      turnID,
		lastEventID: eventID,
		seen:        make(map[string]struct{}),
		customTools: make(map[string]*qoderCloudCachedCustomToolResult),
	}
	if err := b.streamTurn(ctx, client, sessionID, state, opts, msgCh); err != nil {
		// A stream-level failure can leave the hosted turn running or paused on
		// a permission request. Cancel with a fresh short-lived context so the
		// request still goes out when the run context itself is healthy. Context
		// cancellation is handled by failureResult to avoid a duplicate request.
		if ctx.Err() == nil {
			b.cancelSessionBestEffort(client, sessionID)
		}
		return b.failureResult(ctx, client, sessionID, err)
	}

	return Result{
		Status:    "completed",
		Output:    state.output.String(),
		SessionID: sessionID,
		Usage:     state.usage,
	}
}

func (b *qoderCloudBackend) cancelSessionBestEffort(client *qoderCloudClient, sessionID string) {
	if sessionID == "" {
		return
	}
	cancelCtx, cancel := context.WithTimeout(context.Background(), qoderCloudCancelTimeout)
	err := client.cancelSession(cancelCtx, sessionID)
	cancel()
	if err != nil {
		b.logger().Warn("qoder cloud session cancel failed", "error", err)
	}
}

func (b *qoderCloudBackend) failureResult(ctx context.Context, client *qoderCloudClient, sessionID string, err error) Result {
	if ctxErr := ctx.Err(); ctxErr != nil {
		if sessionID != "" {
			b.cancelSessionBestEffort(client, sessionID)
		}
		status := "cancelled"
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			status = "timeout"
		}
		return Result{Status: status, Error: ctxErr.Error(), SessionID: sessionID}
	}
	return Result{Status: "failed", Error: redactQoderCloudSecret(err.Error(), client.pat), SessionID: sessionID}
}

func (b *qoderCloudBackend) streamTurn(
	ctx context.Context,
	client *qoderCloudClient,
	sessionID string,
	state *qoderCloudTurnState,
	opts ExecOptions,
	msgCh chan<- Message,
) error {
	reconnects := 0
	for {
		beforeProcessed := state.processed
		body, err := client.openEventStream(ctx, sessionID, state.lastEventID)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			retryable, retryAfter := qoderCloudStreamRetry(err)
			if !retryable {
				return err
			}
			reconnects++
			if reconnects > qoderCloudMaxReconnects {
				return err
			}
			if err := waitQoderCloudReconnect(ctx, reconnects, retryAfter); err != nil {
				return err
			}
			continue
		}
		terminal, parseErr := b.readEventStream(ctx, client, sessionID, body, state, opts, msgCh)
		_ = body.Close()
		if terminal {
			return nil
		}
		if parseErr != nil && !errors.Is(parseErr, io.EOF) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			retryable, _ := qoderCloudStreamRetry(parseErr)
			if !retryable {
				return parseErr
			}
		}

		if state.processed > beforeProcessed {
			reconnects = 0
		} else {
			reconnects++
		}
		if reconnects > qoderCloudMaxReconnects {
			if parseErr != nil && !errors.Is(parseErr, io.EOF) {
				return parseErr
			}
			return errors.New("qoder cloud event stream ended before the current turn became idle")
		}
		if err := waitQoderCloudReconnect(ctx, reconnects, 0); err != nil {
			return err
		}
	}
}

func qoderCloudStreamRetry(err error) (bool, time.Duration) {
	var httpErr *qoderCloudHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.status == http.StatusRequestTimeout ||
			httpErr.status == http.StatusTooManyRequests ||
			httpErr.status >= http.StatusInternalServerError, httpErr.retryAfter
	}
	var transportErr *qoderCloudTransportError
	if errors.As(err, &transportErr) {
		return true, 0
	}
	var malformedErr *qoderCloudMalformedSSEError
	if errors.As(err, &malformedErr) {
		return true, 0
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true, 0
	}
	return errors.Is(err, io.ErrUnexpectedEOF), 0
}

func waitQoderCloudReconnect(ctx context.Context, attempt int, retryAfter time.Duration) error {
	delay := retryAfter
	if delay <= 0 {
		exponent := attempt - 1
		if exponent < 0 {
			exponent = 0
		}
		if exponent > 4 {
			exponent = 4
		}
		delay = qoderCloudReconnectDelay * time.Duration(1<<exponent)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (b *qoderCloudBackend) readEventStream(
	ctx context.Context,
	client *qoderCloudClient,
	sessionID string,
	body io.Reader,
	state *qoderCloudTurnState,
	opts ExecOptions,
	msgCh chan<- Message,
) (bool, error) {
	return parseQoderCloudSSE(body, func(frame qoderCloudSSEEvent) (bool, error) {
		if frame.Type == "heartbeat" || len(bytes.TrimSpace(frame.Data)) == 0 || bytes.Equal(bytes.TrimSpace(frame.Data), []byte("[DONE]")) {
			return false, nil
		}

		var event qoderCloudEvent
		if err := json.Unmarshal(frame.Data, &event); err != nil {
			// Do not commit this frame's id: reconnect from the previous buffered
			// cursor so a truncated message can be replayed in full. Stop this
			// connection immediately as a later idle must not complete the turn
			// after silently losing the malformed message.
			eventType := redactQoderCloudSecret(frame.Type, strings.TrimSpace(b.cfg.QoderCloud.PAT))
			b.logger().Warn(
				"qoder cloud will reconnect after malformed SSE event",
				"event_type", eventType,
			)
			return false, &qoderCloudMalformedSSEError{eventType: eventType}
		}
		// Some delta envelopes describe the buffered payload in data.type.
		// Keep the SSE lifecycle type authoritative so event_start/event_delta
		// cannot collide with the final agent.message in the dedupe key.
		if frame.Type == "event_start" || frame.Type == "event_delta" {
			event.Type = frame.Type
		} else if event.Type == "" {
			event.Type = frame.Type
		}
		if event.ID == "" {
			event.ID = frame.ID
		}
		cursorID := frame.ID
		if cursorID == "" {
			cursorID = event.ID
		}
		// Delta lifecycle frames share their id with the eventual buffered
		// agent.message. Advancing Last-Event-ID on event_start/event_delta can
		// therefore skip the authoritative message after a disconnect. Only a
		// fully parsed buffered/standalone event commits the cursor.
		advancesCursor := frame.Type != "event_start" && frame.Type != "event_delta" &&
			event.Type != "event_start" && event.Type != "event_delta"
		commitCursor := func() {
			if advancesCursor && cursorID != "" {
				state.lastEventID = cursorID
			}
		}
		if event.ID != "" {
			key := event.Type + "\x00" + event.ID
			if _, duplicate := state.seen[key]; duplicate {
				switch event.Type {
				case "agent.custom_tool_use":
					// The request metadata is already buffered. Do not move the
					// cursor backwards from a later accepted result event.
					return false, b.bufferQoderCloudCustomToolUse(event, state, msgCh, true)
				case "session.status_idle":
					// Posting a custom result can fail after the local handler has
					// completed. A replayed requires_action status must retry the
					// cached result without executing that handler again.
					if strings.EqualFold(strings.TrimSpace(event.StopReason.Type), "requires_action") {
						return false, b.resolveQoderCloudRequiredActions(ctx, client, sessionID, event.StopReason.EventIDs, state, opts, msgCh)
					}
				}
				commitCursor()
				return false, nil
			}
			state.seen[key] = struct{}{}
		}

		// Last-Event-ID starts this stream after the just-sent user event, but
		// reconnect overlap and provider replay can still contain prior turns.
		// When Qoder supplies turn_id, pin the first one observed after that
		// cursor and reject mismatches. The current public status schema omits
		// turn_id, so a missing value remains valid and the cursor itself is the
		// isolation boundary.
		if state.turnID == "" && event.TurnID != "" {
			state.turnID = event.TurnID
		}
		if event.TurnID != "" && state.turnID != "" && event.TurnID != state.turnID {
			commitCursor()
			return false, nil
		}
		state.processed++

		switch event.Type {
		case "agent.message":
			text := qoderCloudContentText(event.Content)
			if text != "" {
				state.output.WriteString(text)
				trySend(msgCh, Message{Type: MessageText, Content: text})
			}
		case "agent.thinking", "event_start":
			// Qoder's buffered thinking event is a lifecycle marker. Deliberately
			// do not surface provider reasoning content.
			trySend(msgCh, Message{Type: MessageThinking})
		case "agent.custom_tool_use":
			if err := b.bufferQoderCloudCustomToolUse(event, state, msgCh, false); err != nil {
				return false, err
			}
		case "agent.tool_use", "agent.mcp_tool_use":
			if qoderCloudToolRequiresConfirmation(event) {
				return false, &qoderCloudUnsupportedPendingActionError{reason: "tool permission requires confirmation"}
			}
			toolName := event.Name
			if toolName == "" {
				toolName = event.ToolName
			}
			input, _ := qoderCloudToolInput(event)
			callID := event.ToolUseID
			if callID == "" {
				callID = event.ID
			}
			trySend(msgCh, Message{
				Type:   MessageToolUse,
				Tool:   toolName,
				CallID: callID,
				Input:  input,
			})
		case "agent.tool_result":
			callID := event.ToolUseID
			if callID == "" {
				callID = event.ID
			}
			output := qoderCloudContentText(event.Content)
			if output == "" {
				output = qoderCloudContentText(event.Result)
			}
			trySend(msgCh, Message{
				Type:   MessageToolResult,
				CallID: callID,
				Output: output,
			})
		case "session.status_running":
			trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: event.SessionID})
		case "session.status_rescheduled":
			trySend(msgCh, Message{Type: MessageStatus, Status: "rescheduled", SessionID: event.SessionID})
		case "session.status_idle":
			if strings.EqualFold(strings.TrimSpace(event.StopReason.Type), "requires_action") {
				if err := b.resolveQoderCloudRequiredActions(ctx, client, sessionID, event.StopReason.EventIDs, state, opts, msgCh); err != nil {
					return false, err
				}
				// Accepted outbound result events are the newest safe cursor.
				// Committing this older status would move Last-Event-ID backwards.
				return false, nil
			}
			if qoderCloudHasUnsentCustomTools(state.customTools) {
				return false, &qoderCloudUnsupportedPendingActionError{reason: "turn became idle with unresolved custom tool requests"}
			}
			state.usage = qoderCloudTokenUsage(event.Usage)
			trySend(msgCh, Message{Type: MessageStatus, Status: "idle", SessionID: event.SessionID})
			commitCursor()
			return true, nil
		case "session.error":
			message := strings.TrimSpace(event.Error.Message)
			if message == "" {
				message = "remote session failed"
			}
			return false, fmt.Errorf("qoder cloud session failed: %s", redactQoderCloudSecret(message, strings.TrimSpace(b.cfg.QoderCloud.PAT)))
		case "session.status_terminated", "session.deleted":
			return false, fmt.Errorf("qoder cloud session ended before the turn completed: %s", event.Type)
		}
		commitCursor()
		return false, nil
	})
}

func (b *qoderCloudBackend) bufferQoderCloudCustomToolUse(
	event qoderCloudEvent,
	state *qoderCloudTurnState,
	msgCh chan<- Message,
	replayed bool,
) error {
	callID := strings.TrimSpace(event.ID)
	if callID == "" {
		callID = strings.TrimSpace(event.CustomToolUseID)
	}
	if callID == "" {
		callID = strings.TrimSpace(event.ToolUseID)
	}
	if callID == "" {
		return &qoderCloudUnsupportedPendingActionError{reason: "custom tool request omitted its correlation id"}
	}

	cached := state.customTools[callID]
	if cached != nil {
		return nil
	}
	if replayed {
		return &qoderCloudUnsupportedPendingActionError{reason: "replayed custom tool request had no cached result"}
	}

	name := strings.TrimSpace(event.Name)
	if name == "" {
		name = strings.TrimSpace(event.ToolName)
	}
	input, inputErr := qoderCloudToolInput(event)
	state.customTools[callID] = &qoderCloudCachedCustomToolResult{
		name:     name,
		input:    input,
		inputErr: inputErr,
	}
	trySend(msgCh, Message{Type: MessageToolUse, Tool: name, CallID: callID, Input: input})
	return nil
}

func (b *qoderCloudBackend) resolveQoderCloudRequiredActions(
	ctx context.Context,
	client *qoderCloudClient,
	sessionID string,
	eventIDs []string,
	state *qoderCloudTurnState,
	opts ExecOptions,
	msgCh chan<- Message,
) error {
	if len(eventIDs) == 0 {
		return &qoderCloudUnsupportedPendingActionError{reason: "turn stopped with requires_action but omitted event_ids"}
	}

	callIDs := make([]string, 0, len(eventIDs))
	for _, rawCallID := range eventIDs {
		callID := strings.TrimSpace(rawCallID)
		if callID == "" || state.customTools[callID] == nil {
			return &qoderCloudUnsupportedPendingActionError{reason: "turn stopped with requires_action for an unknown custom tool request"}
		}
		callIDs = append(callIDs, callID)
	}

	lastAcceptedEventID := ""
	for _, callID := range callIDs {
		cached := state.customTools[callID]
		if !cached.resultReady {
			cached.result = b.executeQoderCloudCustomTool(ctx, callID, cached, opts.CustomToolHandler)
			cached.resultReady = true
		}
		if cached.sent {
			continue
		}

		acceptedEventID, _, err := client.sendCustomToolResult(ctx, sessionID, callID, cached.result)
		if err != nil {
			return err
		}
		cached.sent = true
		if acceptedEventID != "" {
			lastAcceptedEventID = acceptedEventID
		}
		trySend(msgCh, Message{
			Type:   MessageToolResult,
			Tool:   cached.name,
			CallID: callID,
			Output: cached.result.Content,
		})
	}
	if lastAcceptedEventID != "" {
		state.lastEventID = lastAcceptedEventID
	}
	return nil
}

func (b *qoderCloudBackend) executeQoderCloudCustomTool(
	ctx context.Context,
	callID string,
	cached *qoderCloudCachedCustomToolResult,
	handler CustomToolHandler,
) CustomToolResult {
	result := CustomToolResult{}
	switch {
	case cached.name == "":
		result = CustomToolResult{Content: "custom tool request omitted its name", IsError: true}
	case cached.inputErr != nil:
		result = CustomToolResult{Content: "custom tool input must be a JSON object", IsError: true}
	case handler == nil:
		result = CustomToolResult{Content: "no client-side custom tool handler is configured", IsError: true}
	default:
		var handlerErr error
		result, handlerErr = invokeQoderCloudCustomToolHandler(ctx, handler, CustomToolCall{
			Name:   cached.name,
			CallID: callID,
			Input:  cached.input,
		})
		if handlerErr != nil {
			result.IsError = true
			if strings.TrimSpace(result.Content) == "" {
				result.Content = "custom tool failed: " + handlerErr.Error()
			}
		}
	}
	result.Content = boundQoderCloudToolResult(redactQoderCloudSecret(result.Content, strings.TrimSpace(b.cfg.QoderCloud.PAT)))
	return result
}

func invokeQoderCloudCustomToolHandler(ctx context.Context, handler CustomToolHandler, call CustomToolCall) (result CustomToolResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = CustomToolResult{}
			err = fmt.Errorf("custom tool handler panicked")
		}
	}()
	return handler(ctx, call)
}

func qoderCloudToolInput(event qoderCloudEvent) (map[string]any, error) {
	raw := bytes.TrimSpace(event.Input)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		raw = bytes.TrimSpace(event.ToolInput)
	}
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return map[string]any{}, nil
	}
	var input map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&input); err != nil || input == nil {
		return nil, errors.New("tool input is not an object")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("tool input has trailing JSON")
	}
	return input, nil
}

func qoderCloudHasUnsentCustomTools(cached map[string]*qoderCloudCachedCustomToolResult) bool {
	for _, result := range cached {
		if !result.sent {
			return true
		}
	}
	return false
}

func boundQoderCloudToolResult(content string) string {
	if content == "" {
		return "Tool completed with no output."
	}
	if len(content) <= qoderCloudMaxToolResultBytes {
		return content
	}
	limit := qoderCloudMaxToolResultBytes - len("\n[output truncated]")
	for limit > 0 && !utf8.RuneStart(content[limit]) {
		limit--
	}
	return content[:limit] + "\n[output truncated]"
}

func qoderCloudToolRequiresConfirmation(event qoderCloudEvent) bool {
	if event.RequiresConfirmation || event.ConfirmationRequired {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(event.Status)) {
	case "awaiting_confirmation", "pending_confirmation", "requires_confirmation", "requires_action":
		return true
	}
	var permission any
	if len(bytes.TrimSpace(event.EvaluatedPermission)) == 0 || json.Unmarshal(event.EvaluatedPermission, &permission) != nil {
		return false
	}
	return qoderCloudPermissionIsAsk(permission)
}

func qoderCloudPermissionIsAsk(value any) bool {
	switch value := value.(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "ask")
	case []any:
		for _, item := range value {
			if qoderCloudPermissionIsAsk(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range value {
			if qoderCloudPermissionIsAsk(item) {
				return true
			}
		}
	}
	return false
}

func qoderCloudTokenUsage(usage qoderCloudUsage) map[string]TokenUsage {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CacheReadInputTokens == 0 && usage.CacheCreationInputTokens == 0 {
		return nil
	}
	return map[string]TokenUsage{
		"qodercloud": {
			InputTokens:      usage.InputTokens,
			OutputTokens:     usage.OutputTokens,
			CacheReadTokens:  usage.CacheReadInputTokens,
			CacheWriteTokens: usage.CacheCreationInputTokens,
		},
	}
}

func (b *qoderCloudBackend) logger() *slog.Logger {
	if b.cfg.Logger != nil {
		return b.cfg.Logger
	}
	return slog.Default()
}

func qoderCloudContentText(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	var out strings.Builder
	appendQoderCloudText(&out, value)
	return out.String()
}

func appendQoderCloudText(out *strings.Builder, value any) {
	switch value := value.(type) {
	case string:
		out.WriteString(value)
	case []any:
		for _, item := range value {
			appendQoderCloudText(out, item)
		}
	case map[string]any:
		if text, ok := value["text"].(string); ok {
			out.WriteString(text)
			return
		}
		if content, ok := value["content"]; ok {
			appendQoderCloudText(out, content)
			return
		}
		if output, ok := value["output"].(string); ok {
			out.WriteString(output)
		}
	}
}

func parseQoderCloudSSE(reader io.Reader, handle func(qoderCloudSSEEvent) (bool, error)) (bool, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), qoderCloudMaxSSEFrameBytes)
	var frame qoderCloudSSEEvent
	var dataLines []string

	dispatch := func() (bool, error) {
		if frame.Type == "" && frame.ID == "" && len(dataLines) == 0 {
			return false, nil
		}
		frame.Data = []byte(strings.Join(dataLines, "\n"))
		done, err := handle(frame)
		frame = qoderCloudSSEEvent{}
		dataLines = dataLines[:0]
		return done, err
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			done, err := dispatch()
			if done || err != nil {
				return done, err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			value = ""
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			frame.Type = value
		case "id":
			frame.ID = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("qoder cloud SSE parse: %w", err)
	}
	done, err := dispatch()
	if done || err != nil {
		return done, err
	}
	return false, io.EOF
}

func (c *qoderCloudClient) getAgentVersion(ctx context.Context, agentID string) (int, error) {
	body, err := c.doJSON(ctx, http.MethodGet, "/agents/"+url.PathEscape(agentID), "get agent", nil)
	if err != nil {
		return 0, err
	}
	var agent qoderCloudAgent
	if err := decodeQoderCloudObject(body, &agent); err != nil {
		return 0, fmt.Errorf("qoder cloud get agent response: %w", err)
	}
	if agent.Version <= 0 {
		return 0, errors.New("qoder cloud get agent response omitted a valid version")
	}
	return agent.Version, nil
}

func (c *qoderCloudClient) createSession(ctx context.Context, agentID string, version int, environmentID string) (string, error) {
	payload := struct {
		Agent struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Version int    `json:"version"`
		} `json:"agent"`
		EnvironmentID string `json:"environment_id"`
	}{}
	payload.Agent.ID = agentID
	payload.Agent.Type = "agent"
	payload.Agent.Version = version
	payload.EnvironmentID = environmentID

	body, err := c.doJSON(ctx, http.MethodPost, "/sessions", "create session", payload)
	if err != nil {
		return "", err
	}
	var session qoderCloudSession
	if err := decodeQoderCloudObject(body, &session); err != nil {
		return "", fmt.Errorf("qoder cloud create session response: %w", err)
	}
	if strings.TrimSpace(session.ID) == "" {
		return "", errors.New("qoder cloud create session response omitted the session ID")
	}
	return session.ID, nil
}

func (c *qoderCloudClient) getSession(ctx context.Context, sessionID string) error {
	body, err := c.doJSON(ctx, http.MethodGet, "/sessions/"+url.PathEscape(sessionID), "get session", nil)
	if err != nil {
		return err
	}
	var session qoderCloudSession
	if err := decodeQoderCloudObject(body, &session); err != nil {
		return fmt.Errorf("qoder cloud get session response: %w", err)
	}
	if qoderCloudHasArchivedAt(session.ArchivedAt) {
		return &qoderCloudUnrecoverableResumeError{reason: "session is archived"}
	}
	status := strings.ToLower(strings.TrimSpace(session.Status))
	switch status {
	case "terminated", "archived", "deleted":
		return &qoderCloudUnrecoverableResumeError{reason: "session status is " + status}
	case "running", "canceling", "rescheduling":
		return &qoderCloudResumeBusyError{status: status}
	default:
		return nil
	}
}

func qoderCloudHasArchivedAt(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte(`""`))
}

type qoderCloudOutgoingContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type qoderCloudOutgoingEvent struct {
	Type    string                      `json:"type"`
	Content []qoderCloudOutgoingContent `json:"content"`
}

type qoderCloudCustomToolResultEvent struct {
	Type            string                      `json:"type"`
	CustomToolUseID string                      `json:"custom_tool_use_id"`
	Content         []qoderCloudOutgoingContent `json:"content"`
	IsError         bool                        `json:"is_error"`
}

func (c *qoderCloudClient) sendMessages(ctx context.Context, sessionID, prompt, systemPrompt string) (string, string, error) {
	events := []qoderCloudOutgoingEvent{{
		Type:    "user.message",
		Content: []qoderCloudOutgoingContent{{Type: "text", Text: prompt}},
	}}
	if strings.TrimSpace(systemPrompt) != "" {
		events = append(events, qoderCloudOutgoingEvent{
			Type:    "system.message",
			Content: []qoderCloudOutgoingContent{{Type: "text", Text: systemPrompt}},
		})
	}
	payload := struct {
		Events []qoderCloudOutgoingEvent `json:"events"`
	}{Events: events}

	body, err := c.doJSON(ctx, http.MethodPost, "/sessions/"+url.PathEscape(sessionID)+"/events", "send event", payload)
	if err != nil {
		return "", "", err
	}
	processedEvents, err := decodeQoderCloudEventResponse(body)
	if err != nil {
		return "", "", err
	}
	var eventID, turnID string
	for _, event := range processedEvents {
		if event.ID != "" {
			eventID = event.ID
		}
		if turnID == "" && event.TurnID != "" {
			turnID = event.TurnID
		}
	}
	if eventID == "" {
		return "", "", errors.New("qoder cloud send event response omitted the event ID")
	}
	return eventID, turnID, nil
}

func (c *qoderCloudClient) sendCustomToolResult(
	ctx context.Context,
	sessionID string,
	callID string,
	result CustomToolResult,
) (string, string, error) {
	payload := struct {
		Events []qoderCloudCustomToolResultEvent `json:"events"`
	}{Events: []qoderCloudCustomToolResultEvent{{
		Type:            "user.custom_tool_result",
		CustomToolUseID: callID,
		Content:         []qoderCloudOutgoingContent{{Type: "text", Text: result.Content}},
		IsError:         result.IsError,
	}}}
	body, err := c.doJSON(ctx, http.MethodPost, "/sessions/"+url.PathEscape(sessionID)+"/events", "send custom tool result", payload)
	if err != nil {
		return "", "", err
	}
	processedEvents, err := decodeQoderCloudEventResponse(body)
	if err != nil {
		return "", "", err
	}
	var eventID, turnID string
	for _, event := range processedEvents {
		if event.ID != "" {
			eventID = event.ID
		}
		if turnID == "" && event.TurnID != "" {
			turnID = event.TurnID
		}
	}
	if eventID == "" {
		return "", "", errors.New("qoder cloud custom tool result response omitted the event ID")
	}
	return eventID, turnID, nil
}

func (c *qoderCloudClient) cancelSession(ctx context.Context, sessionID string) error {
	_, err := c.doJSON(ctx, http.MethodPost, "/sessions/"+url.PathEscape(sessionID)+"/cancel", "cancel session", nil)
	return err
}

func (c *qoderCloudClient) deleteSession(ctx context.Context, sessionID string) error {
	_, err := c.doJSON(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(sessionID), "delete session", nil)
	var httpErr *qoderCloudHTTPError
	if errors.As(err, &httpErr) && httpErr.status == http.StatusNotFound {
		return nil
	}
	return err
}

func (c *qoderCloudClient) openEventStream(ctx context.Context, sessionID, lastEventID string) (io.ReadCloser, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/sessions/"+url.PathEscape(sessionID)+"/events/stream", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &qoderCloudTransportError{
			operation: "open event stream",
			message:   redactQoderCloudSecret(err.Error(), c.pat),
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, qoderCloudMaxResponseBytes))
		return nil, &qoderCloudHTTPError{
			operation:  "open event stream",
			status:     resp.StatusCode,
			body:       redactQoderCloudSecret(strings.TrimSpace(string(body)), c.pat),
			retryAfter: parseQoderCloudRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if contentType != "" && !strings.HasPrefix(contentType, "text/event-stream") {
		defer resp.Body.Close()
		return nil, fmt.Errorf(
			"qoder cloud open event stream returned unexpected content type %q",
			redactQoderCloudSecret(resp.Header.Get("Content-Type"), c.pat),
		)
	}
	return resp.Body, nil
}

func parseQoderCloudRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := time.ParseDuration(value + "s"); err == nil && seconds > 0 {
		return seconds
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	if delay := time.Until(when); delay > 0 {
		return delay
	}
	return 0
}

func (c *qoderCloudClient) doJSON(ctx context.Context, method, path, operation string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("qoder cloud %s request: %w", operation, err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &qoderCloudTransportError{
			operation: operation,
			message:   redactQoderCloudSecret(err.Error(), c.pat),
		}
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, qoderCloudMaxResponseBytes+1))
	if readErr != nil {
		return nil, fmt.Errorf("qoder cloud %s response: %s", operation, redactQoderCloudSecret(readErr.Error(), c.pat))
	}
	if len(responseBody) > qoderCloudMaxResponseBytes {
		return nil, fmt.Errorf("qoder cloud %s response exceeded %d bytes", operation, qoderCloudMaxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &qoderCloudHTTPError{
			operation: operation,
			status:    resp.StatusCode,
			body:      redactQoderCloudSecret(strings.TrimSpace(string(responseBody)), c.pat),
		}
	}
	return responseBody, nil
}

func (c *qoderCloudClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("qoder cloud create request: %s", redactQoderCloudSecret(err.Error(), c.pat))
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	return req, nil
}

func decodeQoderCloudObject(body []byte, target any) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return errors.New("empty JSON response")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return err
	}
	if data, wrapped := fields["data"]; wrapped {
		if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
			return errors.New("JSON response contained empty data")
		}
		body = data
	}
	return json.Unmarshal(body, target)
}

func decodeQoderCloudEventResponse(body []byte) ([]qoderCloudEvent, error) {
	var direct qoderCloudEvent
	if err := json.Unmarshal(body, &direct); err == nil && direct.ID != "" {
		return []qoderCloudEvent{direct}, nil
	}
	var envelope struct {
		Data   json.RawMessage   `json:"data"`
		Events []qoderCloudEvent `json:"events"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("qoder cloud send event response: %w", err)
	}
	if len(envelope.Events) > 0 {
		return envelope.Events, nil
	}
	var events []qoderCloudEvent
	if err := json.Unmarshal(envelope.Data, &events); err == nil && len(events) > 0 {
		return events, nil
	}
	var event qoderCloudEvent
	if err := json.Unmarshal(envelope.Data, &event); err == nil && event.ID != "" {
		return []qoderCloudEvent{event}, nil
	}
	return nil, errors.New("qoder cloud send event response omitted an event")
}

func redactQoderCloudSecret(value, pat string) string {
	if pat == "" {
		return value
	}
	value = strings.ReplaceAll(value, "Bearer "+pat, "Bearer [REDACTED]")
	return strings.ReplaceAll(value, pat, "[REDACTED]")
}
