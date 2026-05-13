// JEH-1079: persona mask seam.
//
// PersonaMaskInvoker is the upstream-side seam that the cerebro mask
// service plugs into. Concrete implementation lives in
// server/internal/cerebro/persona/mask so the upstream handler package
// never imports the cerebro tree (which would create an import cycle).
//
// The invoker takes the resource shape Multica's API would otherwise
// return and asks persona "may this actor see this, and which fields
// must be redacted". The caller redacts the fields in-place before
// rendering. nil-invoker is the no-persona case — handlers continue
// returning the full resource, matching pre-JEH-1079 behaviour.

package handler

import (
	"context"
)

// PersonaMaskDecision is what the handler acts on. Allowed=false means
// "do not render this resource"; Allowed=true with a non-empty
// MaskedFields means "render but strip these fields before serialising".
type PersonaMaskDecision struct {
	Allowed      bool
	MaskedFields []string
	DecisionID   string
	Reason       string
}

// PersonaMaskInvoker is the interface the cerebro mask service
// implements. The signature mirrors persona's CheckResourceWithMasking
// SDK call: actor, action, kind, resource_id, per-row classification,
// per-field classifications.
type PersonaMaskInvoker interface {
	Decide(
		ctx context.Context,
		actorID, action, kind, resourceID, rowClassification string,
		fieldClasses map[string]string,
	) PersonaMaskDecision
}

// maskDecide is the small helper handlers call. Hides the nil-check so
// each call site is a one-liner — keeps CEREBRO-PATCH markers small.
func (h *Handler) maskDecide(
	ctx context.Context,
	actorID, action, kind, resourceID, rowClassification string,
	fieldClasses map[string]string,
) PersonaMaskDecision {
	if h == nil || h.PersonaMask == nil {
		return PersonaMaskDecision{Allowed: true}
	}
	return h.PersonaMask.Decide(ctx, actorID, action, kind, resourceID, rowClassification, fieldClasses)
}

// PersonaMaskAuditEvent is the upstream-side shape passed to the cerebro
// audit writer. workspace_id, actor identity, and the persona decision
// are all already known at the point the handler decides to redact.
//
// JEH-1173: the field set mirrors `cerebro_persona_mask_audit` so the
// writer is a thin INSERT.
type PersonaMaskAuditEvent struct {
	WorkspaceID    string
	ActorType      string
	ActorID        string
	Action         string
	ResourceKind   string
	ResourceID     string
	Classification string
	Decision       string // "deny" or "mask"
	MaskedFields   []string
	DecisionID     string
	Reason         string
}

// PersonaMaskAuditWriter is the upstream-side seam that the cerebro
// audit writer plugs into. Implementation lives in
// server/internal/cerebro/persona/mask alongside the mask Checker — kept
// out of the upstream handler tree to avoid an import cycle on the
// cerebrodb generated package.
//
// Write is fire-and-forget from the handler's point of view: a failed
// insert MUST NOT block the response. The implementation logs the
// failure but always returns nil to the caller.
type PersonaMaskAuditWriter interface {
	Write(ctx context.Context, evt PersonaMaskAuditEvent)
}

// recordPersonaMaskRedaction is the small helper handlers call after a
// non-pass decision (deny OR allow-with-MaskedFields). Hides the
// nil-check so each call site stays a one-liner. Skips allow-with-no-
// mask reads — the audit table is a redaction ledger, not a read log.
func (h *Handler) recordPersonaMaskRedaction(
	ctx context.Context,
	dec PersonaMaskDecision,
	workspaceID, actorType, actorID, action, kind, resourceID, classification string,
) {
	if h == nil || h.PersonaMaskAudit == nil {
		return
	}
	decision := ""
	switch {
	case !dec.Allowed:
		decision = "deny"
	case len(dec.MaskedFields) > 0:
		decision = "mask"
	default:
		return
	}
	h.PersonaMaskAudit.Write(ctx, PersonaMaskAuditEvent{
		WorkspaceID:    workspaceID,
		ActorType:      actorType,
		ActorID:        actorID,
		Action:         action,
		ResourceKind:   kind,
		ResourceID:     resourceID,
		Classification: classification,
		Decision:       decision,
		MaskedFields:   dec.MaskedFields,
		DecisionID:     dec.DecisionID,
		Reason:         dec.Reason,
	})
}
