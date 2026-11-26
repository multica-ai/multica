package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// hermesSteerAckPrefix opens the reply Hermes sends when `/steer` lands on the
// running turn (acp_adapter/gateway_commands.py `_steer`, Hermes >= 0.22.3).
// The reply arrives as an agent_message_chunk before the command's own
// session/prompt response. It is the only proof the guidance joined the live
// turn: without an active turn Hermes runs the text as a new prompt instead.
const hermesSteerAckPrefix = "⏩ Steer queued for the active turn:"

// hermesSupplementSettleGrace bounds how long a `/steer` still in flight may
// take to settle once the primary prompt has answered. A var so tests can
// shorten it.
var hermesSupplementSettleGrace = 5 * time.Second

// hermesSupplementSession delivers guidance to a running Hermes turn through
// its standard `/steer` slash command, sent as a second session/prompt while
// the primary one is in flight.
//
// Observed contract (Hermes Agent 0.22.5, gateway ACP):
//   - During the turn, `/steer` answers at once with an agent_message_chunk
//     starting with hermesSteerAckPrefix. Hermes adds the guidance after the
//     current tool batch or before the next model call; guidance that arrives
//     after the last tool call runs as a follow-up inside the same admission,
//     and the primary session/prompt answers only after it.
//   - Without an active turn, `/steer <text>` is admitted as an ordinary
//     prompt and runs as a turn of its own. session/cancel stops it.
//
// Steering opens only after Hermes advertised the command for this session and
// produced output for the primary prompt, so the turn is known to be running.
// If the primary prompt answers while a `/steer` is still unacknowledged, the
// command missed the turn: the session is cancelled so the stray prompt cannot
// run, and the message is reported as not delivered.
type hermesSupplementSession struct {
	client *hermesClient

	// steerAdvertised mirrors the latest available_commands_update.
	steerAdvertised atomic.Bool
	// acked records the acknowledgement of the `/steer` in flight. Set from the
	// stdout reader, which must never block on mu.
	acked atomic.Bool
	// steered is set once any `/steer` was sent, so a late acknowledgement is
	// still kept out of the transcript.
	steered atomic.Bool

	mu        sync.Mutex
	sessionID string
	prompted  bool // primary session/prompt is on the wire
	started   bool // the primary turn produced a session update
	stopped   bool // the primary session/prompt answered
	inFlight  bool
	written   bool // the in-flight `/steer` frame reached Hermes' stdin
	settled   chan struct{}
	stopCh    chan struct{}
}

func newHermesSupplementSession() *hermesSupplementSession {
	return &hermesSupplementSession{stopCh: make(chan struct{})}
}

// observeNotification tracks whether this session advertises `/steer`.
func (s *hermesSupplementSession) observeNotification(method string, params json.RawMessage) {
	if method != "session/update" {
		return
	}
	var p struct {
		Update struct {
			SessionUpdate     string `json:"sessionUpdate"`
			AvailableCommands []struct {
				Name string `json:"name"`
			} `json:"availableCommands"`
		} `json:"update"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Update.SessionUpdate != "available_commands_update" {
		return
	}
	advertised := false
	for _, command := range p.Update.AvailableCommands {
		if command.Name == "steer" {
			advertised = true
			break
		}
	}
	s.steerAdvertised.Store(advertised)
}

// acceptUpdate marks the primary turn as started and drops `/steer`
// acknowledgements, which are Hermes' reply to the command rather than part of
// the agent's answer.
func (s *hermesSupplementSession) acceptUpdate(updateType string, update json.RawMessage) bool {
	if updateType == "" {
		return true
	}
	if updateType == "agent_message_chunk" && s.steered.Load() && hermesSteerAck(update) {
		s.acked.Store(true)
		return false
	}
	if !hermesTurnActivity(updateType) {
		return true
	}
	s.mu.Lock()
	if s.prompted {
		s.started = true
	}
	s.mu.Unlock()
	return true
}

// hermesTurnActivity reports whether an update proves the primary turn is
// running. Hermes sends available_commands_update and usage_update
// asynchronously after attach (gateway_server.py `_after_attach`), so they can
// arrive after session/prompt is written but before the turn starts; a `/steer`
// sent then finds no running admission and is admitted as a new prompt.
func hermesTurnActivity(updateType string) bool {
	switch updateType {
	case "agent_message_chunk", "agent_thought_chunk", "tool_call", "tool_call_update":
		return true
	}
	return false
}

func hermesSteerAck(update json.RawMessage) bool {
	var msg struct {
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	return json.Unmarshal(update, &msg) == nil && strings.HasPrefix(strings.TrimSpace(msg.Content.Text), hermesSteerAckPrefix)
}

// open is called once the primary session/prompt is written.
func (s *hermesSupplementSession) open(sessionID string) {
	s.mu.Lock()
	s.sessionID = sessionID
	s.prompted = true
	s.mu.Unlock()
}

func (s *hermesSupplementSession) ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readyLocked()
}

func (s *hermesSupplementSession) readyLocked() bool {
	return s.prompted && s.started && !s.stopped && !s.inFlight && s.steerAdvertised.Load()
}

func (s *hermesSupplementSession) send(ctx context.Context, text string) error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return fmt.Errorf("hermes: %w", ErrSupplementTurnEnded)
	}
	if !s.readyLocked() {
		s.mu.Unlock()
		return errors.New("hermes turn is not ready for steering")
	}
	s.inFlight, s.written = true, false
	s.acked.Store(false)
	s.steered.Store(true)
	settled := make(chan struct{})
	s.settled = settled
	sessionID := s.sessionID
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inFlight, s.written = false, false
		s.mu.Unlock()
		close(settled)
	}()

	// While the turn runs, an acknowledged `/steer` answers at once and an
	// unacknowledged one waits behind the turn, so the wait is bounded by the
	// turn itself plus a grace after it ends.
	reqCtx, cancelReq := context.WithCancel(ctx)
	defer cancelReq()
	go func() {
		select {
		case <-s.stopCh:
		case <-reqCtx.Done():
			return
		}
		grace := time.NewTimer(hermesSupplementSettleGrace)
		defer grace.Stop()
		select {
		case <-grace.C:
			cancelReq()
		case <-reqCtx.Done():
		}
	}()

	response, err := s.client.commandAndNotifySent(reqCtx, map[string]any{
		"sessionId": sessionID,
		"prompt":    []map[string]any{{"type": "text", "text": "/steer " + text}},
	}, func() {
		s.mu.Lock()
		s.written = true
		cancelStray := s.stopped && !s.acked.Load()
		s.mu.Unlock()
		if cancelStray {
			s.cancel(sessionID)
		}
	})
	// The reader handles the acknowledgement chunk before the response frame.
	if s.acked.Load() {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		return fmt.Errorf("hermes /steer: %w", ErrSupplementTurnEnded)
	}
	if err != nil {
		return fmt.Errorf("hermes /steer failed: %w", err)
	}
	return fmt.Errorf("hermes /steer was not applied to the running turn: %s", strings.TrimSpace(string(response)))
}

// stop closes steering when the primary session/prompt answers, cancels a
// `/steer` that missed the turn, and waits for it to settle so stdin is not
// closed under it.
func (s *hermesSupplementSession) stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	close(s.stopCh)
	cancelStray := s.inFlight && s.written && !s.acked.Load()
	settled := s.settled
	inFlight := s.inFlight
	sessionID := s.sessionID
	s.mu.Unlock()
	if cancelStray {
		s.cancel(sessionID)
	}
	if !inFlight {
		return
	}
	timer := time.NewTimer(hermesSupplementSettleGrace + time.Second)
	defer timer.Stop()
	select {
	case <-settled:
	case <-timer.C:
	}
}

func (s *hermesSupplementSession) cancel(sessionID string) {
	data, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/cancel",
		"params":  map[string]any{"sessionId": sessionID},
	})
	if err != nil {
		return
	}
	if err := s.client.writeLine(append(data, '\n')); err != nil {
		s.client.cfg.Logger.Warn("hermes: could not cancel a /steer that missed the turn", "error", err)
		return
	}
	s.client.cfg.Logger.Info("hermes: cancelled a /steer that arrived after the turn ended", "session_id", sessionID)
}
