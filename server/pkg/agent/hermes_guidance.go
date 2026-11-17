package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Hermes advertises /steer through standard ACP available_commands_update.
// Acceptance hands input to Hermes; it does not prove model incorporation.
type hermesGuidanceSession struct {
	mu              sync.Mutex
	client          *hermesClient
	sessionID       string
	commandsSession string
	supportsSteer   bool
	active          bool
	stopped         bool
	pendingAcks     map[string]int
}

func (s *hermesGuidanceSession) notification(method string, data json.RawMessage) {
	if method != "session/update" && method != "session/notification" {
		return
	}
	var p struct {
		Session string          `json:"sessionId"`
		Update  json.RawMessage `json:"update"`
	}
	if json.Unmarshal(data, &p) != nil {
		return
	}
	kind, update := normalizeACPUpdate(p.Update)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	if kind == "available_commands_update" {
		var commands struct {
			Commands []struct {
				Name string `json:"name"`
			} `json:"availableCommands"`
		}
		if json.Unmarshal(update, &commands) != nil {
			return
		}
		s.commandsSession = p.Session
		s.supportsSteer = false
		for _, command := range commands.Commands {
			if command.Name == "steer" {
				s.supportsSteer = true
			}
		}
	}
	// Metadata/command advertisements alone cannot establish a running prompt.
	if p.Session == s.sessionID && s.sessionID != "" {
		switch kind {
		case "agent_message_chunk", "agent_thought_chunk", "tool_call", "tool_call_update":
			s.active = true
		}
	}
}

func (s *hermesGuidanceSession) ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.stopped && s.active && s.supportsSteer && s.commandsSession == s.sessionID
}

func (s *hermesGuidanceSession) finish(json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
}

func (s *hermesGuidanceSession) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	s.pendingAcks = nil
}

// The native slash acknowledgement is presentation, not the model's answer.
// Match this instruction's complete acknowledgement rather than a generic prefix.
func (s *hermesGuidanceSession) acceptUpdate(kind string, data json.RawMessage) bool {
	if kind != "agent_message_chunk" {
		return true
	}
	var update struct {
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(data, &update) != nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ack := strings.TrimSpace(update.Content.Text)
	if count := s.pendingAcks[ack]; count > 0 {
		if count == 1 {
			delete(s.pendingAcks, ack)
		} else {
			s.pendingAcks[ack] = count - 1
		}
		return false
	}
	return true
}

func (s *hermesGuidanceSession) send(ctx context.Context, text string) error {
	s.mu.Lock()
	if s.stopped || !s.active || !s.supportsSteer || s.commandsSession != s.sessionID {
		s.mu.Unlock()
		return fmt.Errorf("Hermes turn is not started or native steer is unavailable")
	}
	session, client := s.sessionID, s.client
	preview := []rune(text)
	suffix := ""
	if len(preview) > 80 {
		preview = preview[:80]
		suffix = "..."
	}
	if s.pendingAcks == nil {
		s.pendingAcks = make(map[string]int)
	}
	// A timeout cannot retract a written command. Keep each expected native
	// acknowledgement until observed or the session ends, including commands
	// with identical previews and commands whose RPC has already completed.
	s.pendingAcks["⏩ Steer queued for the active turn: "+string(preview)+suffix]++
	s.mu.Unlock()
	rpcCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// One ordinary ACP request, never retried. A refusal/timeout uses Multica's
	// existing queue fallback; an end_turn here belongs only to this command.
	result, err := client.request(rpcCtx, "session/prompt", map[string]any{
		"sessionId": session, "prompt": []map[string]any{{"type": "text", "text": "/steer " + text}},
	})
	if err != nil {
		return fmt.Errorf("Hermes native steer: %w", err)
	}
	var response struct {
		StopReason string `json:"stopReason"`
	}
	if json.Unmarshal(result, &response) != nil || response.StopReason != "end_turn" {
		return fmt.Errorf("Hermes native steer did not complete normally")
	}
	return nil
}
