package mask

import (
	"context"
)

// HandlerInvoker is the shape the upstream handler package's
// PersonaMaskInvoker interface accepts. Living here keeps the upstream
// handler from importing this package directly (which would cycle).
// The router wires a *Checker (via Invoker) into Handler.PersonaMask at
// startup.
type HandlerInvoker interface {
	Decide(
		ctx context.Context,
		actorID, action, kind, resourceID, rowClassification string,
		fieldClasses map[string]string,
	) Decision
}

// Invoker adapts *Checker to the upstream handler's PersonaMaskInvoker
// interface, translating the package's Decision struct to the
// upstream-facing handler.PersonaMaskDecision. Constructed by the
// router with the persona-config-aware Checker.
type Invoker struct {
	c *Checker
}

// NewInvoker wraps a Checker for use as Handler.PersonaMask.
func NewInvoker(c *Checker) *Invoker { return &Invoker{c: c} }

// Decide forwards to the embedded Checker. The HandlerInvoker contract
// returns Decision; the upstream handler shape returns
// PersonaMaskDecision — kept symmetric in field names so the bridge
// adapter in the cerebro router can convert without losing context.
func (i *Invoker) Decide(
	ctx context.Context,
	actorID, action, kind, resourceID, rowClassification string,
	fieldClasses map[string]string,
) Decision {
	if i == nil || i.c == nil {
		return FullAccess()
	}
	return i.c.Decide(ctx, actorID, action, kind, resourceID, rowClassification, fieldClasses)
}
