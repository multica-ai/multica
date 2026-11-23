package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"
)

// OpenCodeSupportsTaskSupplement excludes runs that use the non-steerable run
// transport. The ACP handshake must additionally advertise steering support.
func OpenCodeSupportsTaskSupplement(cfg Config, opts ExecOptions) bool {
	acp, _ := opencodeUsesACP(cfg, opts)
	return acp && SupportsTaskSupplement("opencode", cfg.CLIVersion)
}

type opencodeSupplementSession struct {
	ctx       context.Context
	request   acpRequestFn
	sessionID string
	supported bool
	sent      atomic.Bool
	ended     atomic.Bool
}

func opencodeSteeringSupported(result json.RawMessage) bool {
	var init struct {
		Meta struct {
			Steering struct {
				Supported bool `json:"supported"`
			} `json:"steering"`
		} `json:"_meta"`
	}
	return json.Unmarshal(result, &init) == nil && init.Meta.Steering.Supported
}

func (s *opencodeSupplementSession) ready() bool {
	// sent publishes sessionID and supported; ended never resets, including
	// when the prompt response arrives before the write callback runs.
	return s.sent.Load() && !s.ended.Load() && s.ctx.Err() == nil && s.supported
}

func (s *opencodeSupplementSession) send(ctx context.Context, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.ready() {
		return fmt.Errorf("opencode turn is not active: %w", context.Canceled)
	}
	rpcCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	response, err := s.request(rpcCtx, "_session/steering", map[string]any{
		"sessionId": s.sessionID,
		"prompt":    []map[string]any{{"type": "text", "text": text}},
	})
	if err != nil {
		return fmt.Errorf("opencode _session/steering failed: %w", err)
	}
	var result struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		return fmt.Errorf("opencode _session/steering returned an invalid result: %w", err)
	}
	switch result.Outcome {
	case "injected":
		return nil
	case "promptRequired":
		// Steering an idle session must not start a replacement turn.
		s.ended.Store(true)
		return fmt.Errorf("opencode has no running turn: %w", context.Canceled)
	default:
		return fmt.Errorf("opencode _session/steering returned unexpected outcome %q", result.Outcome)
	}
}
