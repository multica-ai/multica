package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

const hermesGuidanceCapability = "ai.nous/active-turn-guidance"

// Hermes uses a wire capability, never the executable's name or version.
func hermesHasGuidance(data json.RawMessage) bool {
	var result struct {
		Capabilities struct {
			Meta map[string]struct {
				Version          int  `json:"version"`
				StatusLookup     bool `json:"statusLookup"`
				LiteralText      bool `json:"literalText"`
				TerminalReceipts bool `json:"terminalReceipts"`
			} `json:"_meta"`
		} `json:"agentCapabilities"`
	}
	if json.Unmarshal(data, &result) != nil {
		return false
	}
	c := result.Capabilities.Meta[hermesGuidanceCapability]
	return c.Version == 1 && c.StatusLookup && c.LiteralText && c.TerminalReceipts
}

type hermesGuidanceReceipt struct {
	Instance    string `json:"runtimeInstanceId"`
	Session     string `json:"sessionId"`
	Turn        string `json:"turnId"`
	Prompt      string `json:"promptId"`
	Message     string `json:"messageId"`
	Disposition string `json:"disposition"`
	State       string `json:"state"`
}

type hermesGuidanceSession struct {
	mu        sync.Mutex
	client    *hermesClient
	promptID  string
	sessionID string
	identity  hermesGuidanceReceipt
	enabled   bool
	stopped   bool
	// Completed primary results retain final receipts if the process exits before lookup.
	final map[string]hermesGuidanceReceipt
}

func (s *hermesGuidanceSession) notification(method string, data json.RawMessage) {
	if method != "_ai.nous/guidanceTurn" {
		return
	}
	var receipt hermesGuidanceReceipt
	if json.Unmarshal(data, &receipt) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled || s.stopped || receipt.Prompt != s.promptID || receipt.Session != s.sessionID || receipt.State != "started" || receipt.Instance == "" || receipt.Turn == "" {
		return
	}
	// One primary prompt has one immutable identity, even if another event arrives late.
	if s.identity.Turn == "" {
		s.identity = receipt
	}
}

func (s *hermesGuidanceSession) ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled && !s.stopped && s.identity.Turn != ""
}

func (s *hermesGuidanceSession) finish(data json.RawMessage) {
	var result struct {
		Meta map[string]struct {
			Receipts []hermesGuidanceReceipt `json:"receipts"`
		} `json:"_meta"`
	}
	_ = json.Unmarshal(data, &result)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	s.final = make(map[string]hermesGuidanceReceipt)
	for _, receipt := range result.Meta[hermesGuidanceCapability].Receipts {
		s.final[receipt.Message] = receipt
	}
}

func (s *hermesGuidanceSession) outcome(receipt hermesGuidanceReceipt, target hermesGuidanceReceipt, id string) (bool, error) {
	if receipt.Instance != target.Instance || receipt.Session != target.Session || receipt.Turn != target.Turn || receipt.Message != id {
		return true, fmt.Errorf("%w: uncorrelated Hermes guidance receipt", ErrSupplementDeliveryUnknown)
	}
	if receipt.Disposition != "accepted_active" {
		switch receipt.Disposition {
		case "rejected_turn_ended", "rejected_turn_mismatch", "rejected_cancelled",
			"rejected_session_mismatch", "invalid_input", "message_id_conflict", "receipt_capacity_reached":
			if receipt.State == "rejected" {
				return true, fmt.Errorf("Hermes guidance rejected: %s", receipt.Disposition)
			}
		}
		return true, fmt.Errorf("%w: unrecognized or inconsistent Hermes receipt", ErrSupplementDeliveryUnknown)
	}
	switch receipt.State {
	case "incorporated":
		return true, nil
	case "pending_incorporation":
		return false, nil
	case "unconsumed_at_turn_end", "cancelled_before_incorporation":
		return true, fmt.Errorf("Hermes guidance was not incorporated: %s", receipt.State)
	default:
		return true, fmt.Errorf("%w: unknown Hermes guidance state", ErrSupplementDeliveryUnknown)
	}
}

func (s *hermesGuidanceSession) send(ctx context.Context, text string) error {
	return s.sendID(ctx, uuid.NewString(), text)
}

func (s *hermesGuidanceSession) sendID(ctx context.Context, id, text string) error {
	s.mu.Lock()
	target, client, ready := s.identity, s.client, s.enabled && !s.stopped && s.identity.Turn != ""
	s.mu.Unlock()
	if !ready {
		return fmt.Errorf("Hermes turn is not started or has ended")
	}
	params := map[string]any{"runtimeInstanceId": target.Instance, "sessionId": target.Session, "expectedTurnId": target.Turn, "messageId": id, "text": text}
	// Never resend steer, including after a lost response. Only status lookup recovers it.
	data, err := hermesGuidanceRequest(ctx, client, "_ai.nous/steer", params)
	for {
		if err == nil {
			var receipt hermesGuidanceReceipt
			if json.Unmarshal(data, &receipt) != nil {
				return fmt.Errorf("%w: invalid Hermes receipt", ErrSupplementDeliveryUnknown)
			}
			if done, outcome := s.outcome(receipt, target, id); done {
				return outcome
			}
		}
		s.mu.Lock()
		final, found := s.final[id]
		stopped := s.stopped
		s.mu.Unlock()
		if found {
			done, outcome := s.outcome(final, target, id)
			if done {
				return outcome
			}
			return fmt.Errorf("%w: unfinished Hermes terminal receipt", ErrSupplementDeliveryUnknown)
		}
		if stopped {
			return fmt.Errorf("%w: primary ended without a final receipt", ErrSupplementDeliveryUnknown)
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: %v", ErrSupplementDeliveryUnknown, ctx.Err())
		case <-timer.C:
		}
		data, err = hermesGuidanceRequest(ctx, client, "_ai.nous/guidanceStatus", params)
		if err != nil {
			s.mu.Lock()
			final, found := s.final[id]
			s.mu.Unlock()
			if found {
				if done, outcome := s.outcome(final, target, id); done {
					return outcome
				}
			}
			return fmt.Errorf("%w: Hermes status unavailable: %v", ErrSupplementDeliveryUnknown, err)
		}
	}
}

func hermesGuidanceRequest(ctx context.Context, client *hermesClient, method string, params any) (json.RawMessage, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return client.request(rpcCtx, method, params)
}
