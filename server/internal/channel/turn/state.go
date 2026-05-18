// This file parses and normalizes durable channel turn state.
package turn

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	stateBlockStart = "<multica_channel_state>"
	stateBlockEnd   = "</multica_channel_state>"
)

// PendingAction is the durable state for one unresolved channel turn
// clarification.
type PendingAction struct {
	Kind       string            `json:"kind"`
	Params     map[string]string `json:"params,omitempty"`
	Missing    []string          `json:"missing,omitempty"`
	Candidates []string          `json:"candidates,omitempty"`
	Question   string            `json:"question,omitempty"`
	CreatedAt  string            `json:"created_at,omitempty"`
	ExpiresAt  string            `json:"expires_at,omitempty"`
}

// ContextReset records a user-requested boundary for automatic channel
// context injection.
type ContextReset struct {
	Reason    string `json:"reason,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// Active reports whether a pending action should still be offered to the next
// channel turn.
func (p PendingAction) Active(now time.Time) bool {
	if strings.TrimSpace(p.Kind) == "" {
		return false
	}
	if strings.TrimSpace(p.ExpiresAt) == "" {
		return true
	}
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(p.ExpiresAt))
	if err != nil {
		return false
	}
	return now.Before(expiresAt)
}

// StatePayload is the machine-readable channel turn state stored in
// channel_turn.result_payload.
type StatePayload struct {
	PendingAction *PendingAction `json:"pending_action,omitempty"`
	ContextReset  *ContextReset  `json:"context_reset,omitempty"`
}

// Empty reports whether the payload carries any state worth persisting.
func (s StatePayload) Empty() bool {
	return s.PendingAction == nil && s.ContextReset == nil
}

// AgentResult is the parsed agent output after hidden state metadata is
// stripped from the user-visible reply.
type AgentResult struct {
	Reply string
	State StatePayload
}

// ParseAgentOutput strips the optional hidden channel state block from agent
// output and returns the visible reply plus structured state.
func ParseAgentOutput(output string) (AgentResult, error) {
	raw := strings.TrimSpace(output)
	result := AgentResult{Reply: raw}
	start := strings.LastIndex(raw, stateBlockStart)
	if start < 0 {
		return result, nil
	}
	bodyStart := start + len(stateBlockStart)
	rest := raw[bodyStart:]
	relEnd := strings.Index(rest, stateBlockEnd)
	if relEnd < 0 {
		result.Reply = strings.TrimSpace(raw[:start])
		return result, errors.New("unterminated channel state block")
	}
	body := strings.TrimSpace(rest[:relEnd])
	after := rest[relEnd+len(stateBlockEnd):]
	result.Reply = strings.TrimSpace(raw[:start] + after)
	if body == "" {
		return result, nil
	}
	var state StatePayload
	if err := json.Unmarshal([]byte(body), &state); err != nil {
		return result, err
	}
	if state.PendingAction != nil {
		NormalizePendingAction(state.PendingAction)
	}
	if state.ContextReset != nil {
		NormalizeContextReset(state.ContextReset)
	}
	result.State = state
	return result, nil
}

// ParseStatePayload reads channel_turn.result_payload into StatePayload.
func ParseStatePayload(payload json.RawMessage) (StatePayload, error) {
	if len(payload) == 0 {
		return StatePayload{}, nil
	}
	var state StatePayload
	if err := json.Unmarshal(payload, &state); err != nil {
		return StatePayload{}, err
	}
	if state.PendingAction != nil {
		NormalizePendingAction(state.PendingAction)
	}
	if state.ContextReset != nil {
		NormalizeContextReset(state.ContextReset)
	}
	return state, nil
}

// MarshalStatePayload returns JSON for the non-empty state payload.
func MarshalStatePayload(state StatePayload) (json.RawMessage, bool) {
	if state.Empty() {
		return nil, false
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return nil, false
	}
	return payload, true
}

// NormalizePendingAction makes pending state stable enough to match across
// turns without relying on language-specific phrasing.
func NormalizePendingAction(p *PendingAction) {
	if p == nil {
		return
	}
	p.Kind = strings.TrimSpace(p.Kind)
	if p.Params != nil {
		normalized := make(map[string]string, len(p.Params))
		for k, v := range p.Params {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			normalized[key] = strings.TrimSpace(v)
		}
		p.Params = normalized
	}
	p.Missing = normalizeStringSlice(p.Missing, false)
	p.Candidates = normalizeStringSlice(p.Candidates, true)
	p.Question = strings.TrimSpace(p.Question)
	p.CreatedAt = strings.TrimSpace(p.CreatedAt)
	p.ExpiresAt = strings.TrimSpace(p.ExpiresAt)
}

// NormalizeContextReset makes reset state stable before persistence.
func NormalizeContextReset(r *ContextReset) {
	if r == nil {
		return
	}
	r.Reason = strings.TrimSpace(r.Reason)
	r.CreatedAt = strings.TrimSpace(r.CreatedAt)
}

func normalizeStringSlice(in []string, upper bool) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, item := range in {
		item = strings.TrimSpace(item)
		if upper {
			item = strings.ToUpper(item)
		}
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
