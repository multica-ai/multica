package inbound

import (
	"context"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

// normalizeStep is the first step of the inbound pipeline. The adapter
// layer has already mapped the platform-specific payload into a
// port.InboundEvent and stripped mention markers, so this step is a
// defensive validator rather than a transformer.
type normalizeStep struct{}

// NewNormalizeStep returns the normalize Step.
func NewNormalizeStep() Step { return &normalizeStep{} }

// Name returns the stable telemetry label.
func (normalizeStep) Name() string { return "normalize" }

// Run rejects malformed events with Skip. The fields enforced here
// (EventID, ChatID, SenderID) are the keys downstream steps use to
// dedupe, route, and reply; an event missing any of them is unsafe
// to forward and is dropped silently.
func (normalizeStep) Run(_ context.Context, evt port.InboundEvent) (port.InboundEvent, Decision, error) {
	if evt.EventID == "" || evt.ChatID == "" || evt.SenderID == "" {
		return evt, DecisionSkip, nil
	}
	return evt, DecisionContinue, nil
}

// Compile-time interface conformance.
var _ Step = (*normalizeStep)(nil)
