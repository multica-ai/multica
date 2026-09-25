package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	claudeSupplementInitializeID     = "multica-supplement-initialize"
	claudeSupplementHandshakeTimeout = 3 * time.Second
)

// All four context events support additionalContext; Stop uses decision/reason.
// https://code.claude.com/docs/en/hooks#add-context-for-claude
var claudeSupplementEvents = []string{"UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure", "Stop"}

// Claude has no conditional, current-turn-only user input request. SDK hooks
// instead insert context while the provider is waiting at a turn boundary.
// A Stop hook with pending input continues the same loop; an empty Stop closes
// admission before releasing Claude. No supplement is ever a new user prompt.
// Wire contract: anthropics/claude-agent-sdk-python, _internal/query.py.
type claudeSupplementSession struct {
	ctx         context.Context
	mu          sync.Mutex
	active      bool
	started     bool
	ended       bool
	pending     []*claudeSupplementInput
	initialized chan error
}

type claudeSupplementInput struct {
	ctx     context.Context
	text    string
	done    chan error
	writing bool
}

func newClaudeSupplementSession(ctx context.Context) *claudeSupplementSession {
	return &claudeSupplementSession{ctx: ctx, initialized: make(chan error, 1)}
}

func (s *claudeSupplementSession) initialize(w io.Writer, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()
	hooks := make(map[string]any, len(claudeSupplementEvents))
	for _, event := range claudeSupplementEvents {
		hooks[event] = []any{map[string]any{"hookCallbackIds": []string{"multica-supplement-" + event}}}
	}
	if err := writeClaudeFrame(w, map[string]any{
		"type": "control_request", "request_id": claudeSupplementInitializeID,
		"request": map[string]any{"subtype": "initialize", "hooks": hooks},
	}); err != nil {
		return err
	}
	select {
	case err := <-s.initialized:
		return err
	case <-ctx.Done():
		return fmt.Errorf("claude supplement initialization: %w", ctx.Err())
	}
}

func (s *claudeSupplementSession) handleResponse(raw json.RawMessage) {
	var response struct {
		RequestID string `json:"request_id"`
		Subtype   string `json:"subtype"`
		Error     string `json:"error"`
	}
	if json.Unmarshal(raw, &response) != nil || response.RequestID != claudeSupplementInitializeID {
		return
	}
	var err error
	if response.Subtype != "success" {
		err = fmt.Errorf("claude supplement initialization rejected: %s", response.Error)
	}
	select {
	case s.initialized <- err:
	default:
	}
}

func (s *claudeSupplementSession) ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active && !s.ended && s.ctx.Err() == nil
}

func (s *claudeSupplementSession) supplement(ctx context.Context, text string) error {
	input := &claudeSupplementInput{ctx: ctx, text: text, done: make(chan error, 1)}
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return err
	}
	if s.ended || s.ctx.Err() != nil {
		s.mu.Unlock()
		return context.Canceled
	}
	if !s.active {
		started := s.started
		s.mu.Unlock()
		if started {
			return context.Canceled
		}
		return errors.New("claude turn has not started")
	}
	s.pending = append(s.pending, input)
	s.mu.Unlock()
	select {
	case err := <-input.done:
		return err
	case <-ctx.Done():
	case <-s.ctx.Done():
	}
	// Serialize cancellation with hook delivery. If delivery won, preserve its
	// outcome; otherwise remove the message so a later hook cannot replay it.
	s.mu.Lock()
	if input.writing {
		// Once a hook owns the frame, its write decides delivery. Cancellation
		// closes the process pipe; it must not report failure before that write.
		s.mu.Unlock()
		return <-input.done
	}
	defer s.mu.Unlock()
	select {
	case err := <-input.done:
		return err
	default:
	}
	for i, pending := range s.pending {
		if pending == input {
			s.pending = append(s.pending[:i], s.pending[i+1:]...)
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.ctx.Err()
}

func (s *claudeSupplementSession) end() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ended, s.active = true, false
	for _, input := range s.pending {
		input.done <- context.Canceled
	}
	s.pending = nil
	select {
	case s.initialized <- context.Canceled:
	default:
	}
}

// prepareHook claims pending input synchronously, but leaves pipe I/O to the
// caller's writer goroutine so stdout can keep draining under backpressure.
func (s *claudeSupplementSession) prepareHook(msg claudeSDKMessage) (func(io.Writer) error, bool) {
	var req struct {
		Subtype    string `json:"subtype"`
		CallbackID string `json:"callback_id"`
		Input      struct {
			Event   string `json:"hook_event_name"`
			AgentID string `json:"agent_id"`
		} `json:"input"`
	}
	if json.Unmarshal(msg.Request, &req) != nil || req.Subtype != "hook_callback" {
		return nil, false
	}
	output := map[string]any{}
	s.mu.Lock()
	// Hook registrations are inherited by subagents. Only the main loop may
	// consume instructions addressed to this Multica task.
	owned := slices.Contains(claudeSupplementEvents, req.Input.Event) && req.CallbackID == "multica-supplement-"+req.Input.Event
	var delivered []*claudeSupplementInput
	if owned && req.Input.AgentID == "" && msg.ParentToolUseID == "" && !s.ended && s.ctx.Err() == nil {
		s.started = true
		var texts []string
		for _, input := range s.pending {
			if err := input.ctx.Err(); err != nil {
				input.done <- err
				continue
			}
			delivered = append(delivered, input)
			input.writing = true
			texts = append(texts, input.text)
		}
		s.pending = nil
		s.active = req.Input.Event != "Stop" || len(delivered) > 0
		if len(delivered) > 0 {
			text := strings.Join(texts, "\n\n")
			if req.Input.Event == "Stop" {
				output = map[string]any{"decision": "block", "reason": text}
			} else {
				output["hookSpecificOutput"] = map[string]any{"hookEventName": req.Input.Event, "additionalContext": text}
			}
		}
	}
	frame := map[string]any{
		"type":     "control_response",
		"response": map[string]any{"subtype": "success", "request_id": msg.RequestID, "response": output},
	}
	s.mu.Unlock()
	return func(w io.Writer) error {
		err := writeClaudeFrame(w, frame)
		for _, input := range delivered {
			input.done <- err
		}
		return err
	}, true
}

func writeClaudeFrame(w io.Writer, frame any) error {
	return json.NewEncoder(w).Encode(frame)
}

// The initial prompt and SDK control replies share stdin. Serialize entire
// frames, including prompts larger than a pipe's atomic-write limit.
type claudeInputWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *claudeInputWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(data)
}
