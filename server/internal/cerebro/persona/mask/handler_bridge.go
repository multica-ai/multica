package mask

import (
	"context"

	"github.com/multica-ai/multica/server/internal/handler"
)

// HandlerBridge implements the upstream handler.PersonaMaskInvoker
// contract on top of *Checker. Lives here (cerebro zone) rather than
// in handler/ so the upstream package never imports this one.
//
// The bridge translates this package's Decision struct to the
// handler-facing PersonaMaskDecision. The shapes are deliberately
// identical so the conversion is mechanical.
type HandlerBridge struct {
	c *Checker
}

// NewHandlerBridge wraps a Checker for use as Handler.PersonaMask.
func NewHandlerBridge(c *Checker) *HandlerBridge { return &HandlerBridge{c: c} }

// Decide forwards to the embedded Checker, converting Decision →
// handler.PersonaMaskDecision. A nil bridge returns Allowed=true with
// no mask — matches the no-persona fallback used elsewhere.
func (b *HandlerBridge) Decide(
	ctx context.Context,
	actorID, action, kind, resourceID, rowClassification string,
	fieldClasses map[string]string,
) handler.PersonaMaskDecision {
	if b == nil || b.c == nil {
		return handler.PersonaMaskDecision{Allowed: true}
	}
	d := b.c.Decide(ctx, actorID, action, kind, resourceID, rowClassification, fieldClasses)
	return handler.PersonaMaskDecision{
		Allowed:      d.Allowed,
		MaskedFields: d.MaskedFields,
		DecisionID:   d.DecisionID,
		Reason:       d.Reason,
	}
}
