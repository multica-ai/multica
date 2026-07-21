package accessdecision

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

// PolicyResolver is the read-only slice of the unified permission engine used
// by the shadow. It is deliberately the same resolver as the Gateway's live
// tool-policy path, but Observe never returns its verdict to the caller.
type PolicyResolver interface {
	ResolveGeneral(context.Context, toolpolicy.Query, bool) (toolpolicy.Effective, error)
	MemberOverrideEnabled(context.Context, pgtype.UUID) bool
}

// EvidenceLookup reads the runtime proof produced by the availability model.
type EvidenceLookup interface {
	Lookup(string, availabilityevidence.RuntimeType) availabilityevidence.Evidence
}

// Writer appends one immutable Decision Ledger entry.
type Writer interface {
	Append(context.Context, Entry) error
}

// MandateAuthorizer enforces the immutable per-task contract at every call.
type MandateAuthorizer interface {
	Authorize(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID, string) error
}

// Call is the complete server-owned context for one observed Gateway call.
// CanonicalCapabilityID is empty when the concrete callable tool cannot be
// resolved through the canonical catalog; that state fails closed in Evaluate.
type Call struct {
	WorkspaceID           pgtype.UUID
	AgentID               pgtype.UUID
	RuntimeID             pgtype.UUID
	OwnerID               pgtype.UUID
	OnBehalfOfID          pgtype.UUID
	SystemID              pgtype.UUID
	TaskID                pgtype.UUID
	IssueID               pgtype.UUID
	CanonicalCapabilityID string
	ObservedToolName      string
	LegacyDecision        Decision
	LegacyPath            string
	IsSystem              bool
	RequestContext        toolpolicy.RequestContext
	EvidenceLevel         availabilityevidence.Level
}

// Observer is the Policy Decision Service used by the Gateway. It combines the
// canonical policy result with live runtime-surface evidence and records every
// decision in the append-only Decision Ledger.
type Observer struct {
	policy   PolicyResolver
	evidence EvidenceLookup
	writer   Writer
	mandates MandateAuthorizer
}

func NewObserver(policy PolicyResolver, evidence EvidenceLookup, writer Writer) *Observer {
	return &Observer{policy: policy, evidence: evidence, writer: writer}
}

func (o *Observer) WithMandates(m MandateAuthorizer) *Observer {
	o.mandates = m
	return o
}

// Decide returns the authoritative fail-closed decision for one Gateway call.
// Policy lookup failures, missing canonical identities, absent runtime
// capabilities, Ask, and Deny all become Deny. Ledger persistence is best-effort and
// never changes the returned decision.
func (o *Observer) Decide(ctx context.Context, call Call) Entry {
	return o.evaluate(ctx, call, true)
}

// Observe resolves, compares, and attempts to append one call. It returns the
// entry for deterministic tests and diagnostics only; Gateway enforcement must
// ignore it.
func (o *Observer) Observe(ctx context.Context, call Call) Entry {
	return o.evaluate(ctx, call, false)
}

func (o *Observer) evaluate(ctx context.Context, call Call, authoritative bool) Entry {
	policyDecision := PolicyError
	mandateDenied := authoritative && call.TaskID.Valid && (o == nil || o.mandates == nil || o.mandates.Authorize(ctx, call.TaskID, call.WorkspaceID, call.AgentID, call.ObservedToolName) != nil)
	// Never resolve a policy for an uncanonicalized input. For a known
	// capability, ObservedToolName is the catalog-validated policy form used by
	// the existing table; the stable capability ID remains the decision key.
	if o != nil && o.policy != nil && call.CanonicalCapabilityID != "" {
		effective, err := o.policy.ResolveGeneral(ctx, toolpolicy.Query{
			WorkspaceID:    call.WorkspaceID,
			ToolKey:        call.ObservedToolName,
			RuntimeID:      call.RuntimeID,
			AgentID:        call.AgentID,
			UserID:         call.OwnerID,
			OnBehalfOfID:   call.OnBehalfOfID,
			SystemID:       call.SystemID,
			IsSystem:       call.IsSystem,
			RequestContext: call.RequestContext,
		}, o.policy.MemberOverrideEnabled(ctx, call.WorkspaceID))
		if err == nil {
			policyDecision = policyDecisionFromSetting(effective.Setting)
		}
	}

	evidenceLevel := call.EvidenceLevel
	if evidenceLevel == "" {
		evidenceLevel = availabilityevidence.LevelDeclared
	}
	if call.EvidenceLevel == "" && o != nil && o.evidence != nil && call.CanonicalCapabilityID != "" {
		evidenceLevel = o.evidence.Lookup(
			call.CanonicalCapabilityID,
			availabilityevidence.RuntimeFirtalGateway,
		).Level
	}

	entry := NewEntry(Observation{
		WorkspaceID:           util.UUIDToString(call.WorkspaceID),
		AgentID:               util.UUIDToString(call.AgentID),
		RuntimeID:             util.UUIDToString(call.RuntimeID),
		OnBehalfOfUserID:      util.UUIDToString(call.OnBehalfOfID),
		TaskID:                util.UUIDToString(call.TaskID),
		IssueID:               util.UUIDToString(call.IssueID),
		ObservedToolName:      call.ObservedToolName,
		CanonicalCapabilityID: call.CanonicalCapabilityID,
		LegacyDecision:        call.LegacyDecision,
		LegacyPath:            call.LegacyPath,
		PolicyDecision:        policyDecision,
		EvidenceLevel:         evidenceLevel,
	})
	if mandateDenied {
		entry.ShadowDecision = DecisionDeny
		entry.Reason = "task mandate denied the call"
	}
	if authoritative {
		entry.LegacyDecision = entry.ShadowDecision
		entry.LegacyPath = "policy_decision_service"
		entry.Differs = false
	}
	if o != nil && o.writer != nil {
		_ = o.writer.Append(ctx, entry)
	}
	return entry
}

func policyDecisionFromSetting(setting toolpolicy.Setting) PolicyDecision {
	switch setting {
	case toolpolicy.SettingAllow:
		return PolicyAllow
	case toolpolicy.SettingAsk:
		return PolicyAsk
	case toolpolicy.SettingDeny, toolpolicy.SettingDisable:
		return PolicyDeny
	default:
		return PolicyError
	}
}
