package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// piSupplementSession owns one RPC run. A queued reply is not a delivery
// receipt: steer can race agent_end and stay queued for a future run.
type piSupplementSession struct {
	stdin   io.WriteCloser
	writeMu sync.Mutex
	sendMu  sync.Mutex
	mu      sync.Mutex
	active  bool
	stopped bool
	settled bool
	next    uint64
	pending *piSupplementPending
	done    chan struct{}
}

type piSupplementPending struct {
	id       string
	text     string
	accepted bool
	consumed bool
	ack      chan struct{}
	result   chan error
}

func newPiSupplementSession(stdin io.WriteCloser) *piSupplementSession {
	return &piSupplementSession{stdin: stdin, done: make(chan struct{})}
}

func (s *piSupplementSession) isSettled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settled
}

func (s *piSupplementSession) ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active && !s.stopped
}

func (s *piSupplementSession) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stopped {
		s.stopped = true
		s.active = false
		close(s.done)
	}
}

func (s *piSupplementSession) write(ctx context.Context, command any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	// Bound writes even when the runtime stops reading stdin.
	if pipe, ok := s.stdin.(interface{ SetWriteDeadline(time.Time) error }); ok {
		deadline := time.Now().Add(8 * time.Second)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		_ = pipe.SetWriteDeadline(deadline)
		defer pipe.SetWriteDeadline(time.Time{})
	}
	return json.NewEncoder(s.stdin).Encode(command)
}

func (s *piSupplementSession) send(ctx context.Context, text string) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	s.mu.Lock()
	if !s.active || s.stopped {
		s.mu.Unlock()
		return fmt.Errorf("pi turn is no longer active")
	}
	s.next++
	p := &piSupplementPending{id: fmt.Sprintf("steer-%d", s.next), text: text, result: make(chan error, 1), ack: make(chan struct{}, 1)}
	s.pending = p
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.pending == p {
			s.pending = nil
		}
		s.mu.Unlock()
	}()
	if err := s.write(ctx, map[string]string{"id": p.id, "type": "steer", "message": text}); err != nil {
		return err
	}
	ackTimer := time.NewTimer(8 * time.Second)
	defer ackTimer.Stop()
	ack := p.ack
	for {
		select {
		case <-ack:
			ackTimer.Stop()
			ack = nil
		case <-ackTimer.C:
			return fmt.Errorf("pi steer acknowledgement timed out")
		case err := <-p.result:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-s.done:
			select {
			case err := <-p.result:
				return err
			default:
			}
			return fmt.Errorf("pi run ended before steering was consumed")
		}
	}
}

// observe runs only on the stdout reader and never waits for stdin writes.
func (s *piSupplementSession) observe(raw []byte) error {
	var event struct {
		Type    string `json:"type"`
		ID      string `json:"id"`
		Command string `json:"command"`
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Data    struct {
			Disposition string `json:"disposition"`
		} `json:"data"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.Type == "agent_end" {
		s.active = false
	}
	if event.Type == "agent_settled" {
		s.settled = true
		s.active = false
	}
	if event.Type == "response" && event.ID == "initial-prompt" && event.Command == "prompt" && event.Success && event.Data.Disposition == "handled" && !s.active {
		s.settled = true
	}
	if event.Type == "agent_start" && !s.stopped {
		s.active = true
	}
	if event.Type == "response" && event.ID == "initial-prompt" && event.Command == "prompt" && !event.Success {
		return fmt.Errorf("pi prompt rejected: %s", event.Error)
	}
	p := s.pending
	if p == nil {
		return nil
	}
	if event.Type == "response" && event.ID == p.id && event.Command == "steer" {
		if !event.Success {
			select {
			case p.result <- fmt.Errorf("pi steer rejected: %s", event.Error):
			default:
			}
			return nil
		}
		if event.Data.Disposition == "handled" {
			p.consumed = true
		}
		p.accepted = event.Data.Disposition == "queued" || event.Data.Disposition == "handled"
		if p.accepted {
			select {
			case p.ack <- struct{}{}:
			default:
			}
		}
		if !p.accepted {
			select {
			case p.result <- fmt.Errorf("pi steer returned an invalid disposition"):
			default:
			}
		}
	}
	if event.Type == "message_start" && event.Message.Role == "user" {
		for _, part := range event.Message.Content {
			if part.Type != "text" {
				continue
			}
			// Exact text match covers the message-first race before the
			// steer reply. After accepted (queued/handled), the next user
			// message_start is consumption evidence even when Pi's input
			// transform rewrites the steer text — do not require equality
			// or substring matching. Session has one pending at a time.
			if part.Text == p.text || p.accepted {
				p.consumed = true
				break
			}
		}
	}
	// A matching user event is stronger evidence than a queued RPC reply.
	// The runtime may settle before its asynchronous command reply is flushed.
	if p.consumed {
		select {
		case p.result <- nil:
		default:
		}
	}
	return nil
}

// piExtensionDialogResponse preserves non-interactive execution: no UI exists
// to answer extension dialogs, so dismiss them rather than block or approve.
func piExtensionDialogResponse(raw []byte) map[string]any {
	var request struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		Method string `json:"method"`
	}
	if json.Unmarshal(raw, &request) != nil || request.Type != "extension_ui_request" || request.ID == "" {
		return nil
	}
	switch request.Method {
	case "select", "confirm", "input", "editor":
		return map[string]any{"type": "extension_ui_response", "id": request.ID, "cancelled": true}
	default:
		return nil
	}
}
