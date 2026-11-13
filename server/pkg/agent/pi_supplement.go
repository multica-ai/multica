package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"
)

// piSupplementSession owns one RPC run. A queued reply is not a delivery
// receipt: steer can race agent_end and stay queued for a future run.
//
// Delivery is the user message_start for the exact text Pi queued for this
// steer. Input handlers may rewrite that text, and the reply does not carry
// it, so the queued text is attributed from queue_update: Pi appends the
// post-handler text and announces the full queue before it replies. Pi also
// dequeues steering by exact message text, so the same match is used here.
type piSupplementSession struct {
	stdin    io.WriteCloser
	writeMu  sync.Mutex
	sendMu   sync.Mutex
	mu       sync.Mutex
	active   bool
	stopped  bool
	settled  bool
	next     uint64
	steering []string // last queue_update steering snapshot
	pending  *piSupplementPending
	done     chan struct{}
}

type piSupplementPending struct {
	id   string
	text string
	// replied is set by the queued reply; target is then the queued text.
	replied bool
	target  string
	// Evidence observed before the reply, kept so either order correlates.
	queued  []string // steering entries Pi added while awaiting the reply
	started []string // user message texts Pi started while awaiting the reply
	ack     chan struct{}
	result  chan error
}

// queuedText returns the text Pi queued for this steer. An unchanged copy of
// the request is preferred. Otherwise Pi queued a transformed text, which it
// appends immediately before replying, so the latest addition is this steer's.
// Runtimes without queue_update fall back to the request text.
func (p *piSupplementPending) queuedText() string {
	for _, text := range p.queued {
		if text == p.text {
			return text
		}
	}
	if n := len(p.queued); n > 0 {
		return p.queued[n-1]
	}
	return p.text
}

func (p *piSupplementPending) resolve(err error) {
	select {
	case p.result <- err:
	default:
	}
}

// piQueueAdditions returns entries in next that prev does not account for,
// in queue order. Pi appends new steering and removes consumed entries.
func piQueueAdditions(prev, next []string) []string {
	remaining := make(map[string]int, len(prev))
	for _, text := range prev {
		remaining[text]++
	}
	var added []string
	for _, text := range next {
		if remaining[text] > 0 {
			remaining[text]--
			continue
		}
		added = append(added, text)
	}
	return added
}

// piUserMessageText mirrors Pi's own steering dequeue key: text parts joined
// without a separator. Content may be a plain string.
func piUserMessageText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, part := range parts {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
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
		Steering []string `json:"steering"`
		Message  struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
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
	// Track the queue even without a pending steer so additions are diffed
	// against Pi's real state, including extension-injected steering.
	var queued []string
	if event.Type == "queue_update" {
		queued = piQueueAdditions(s.steering, event.Steering)
		s.steering = slices.Clone(event.Steering)
	}
	p := s.pending
	if p == nil {
		return nil
	}
	switch {
	case event.Type == "queue_update":
		if !p.replied {
			p.queued = append(p.queued, queued...)
		}
	case event.Type == "response" && event.ID == p.id && event.Command == "steer":
		if !event.Success {
			p.resolve(fmt.Errorf("pi steer rejected: %s", event.Error))
			return nil
		}
		switch event.Data.Disposition {
		case "handled":
			// An input handler consumed the steer; nothing was queued.
			p.resolve(nil)
		case "queued":
			p.replied = true
			p.target = p.queuedText()
			select {
			case p.ack <- struct{}{}:
			default:
			}
			if slices.Contains(p.started, p.target) {
				p.resolve(nil)
			}
		default:
			p.resolve(fmt.Errorf("pi steer returned an invalid disposition"))
		}
	case event.Type == "message_start" && event.Message.Role == "user":
		// Only the queued text proves delivery. Other user messages, such as
		// the initial prompt or extension-injected input, are not receipts.
		// Before the reply, only an unchanged copy is conclusive: the latest
		// queued entry may still be an extension's, with this steer to follow.
		text := piUserMessageText(event.Message.Content)
		switch {
		case p.replied && text == p.target, !p.replied && text == p.text:
			p.resolve(nil)
		case !p.replied:
			p.started = append(p.started, text)
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
