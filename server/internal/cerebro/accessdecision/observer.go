package accessdecision

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
	"github.com/multica-ai/multica/server/internal/cerebro/platformaccess"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

// PolicyResolver is the read-only slice of the unified permission engine used
// by the Gateway decision service, Settings, Capabilities and agent tool access.
type PolicyResolver interface {
	ResolvePermission(context.Context, toolpolicy.Query, platformaccess.Actor) (toolpolicy.Effective, error)
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

// Call is the complete server-owned context for one Gateway decision.
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
	IsSystem              bool
	RequestContext        toolpolicy.RequestContext
	EvidenceLevel         availabilityevidence.Level
}

// Service is the Policy Decision Service used by the Gateway. It combines
// canonical policy with live runtime evidence and records each enforced
// decision.
type Service struct {
	policy             PolicyResolver
	evidence           EvidenceLookup
	writer             Writer
	mandates           MandateAuthorizer
	mandateEnforcement func(context.Context, pgtype.UUID) bool
}

func NewService(policy PolicyResolver, evidence EvidenceLookup, writer Writer) *Service {
	return &Service{policy: policy, evidence: evidence, writer: writer}
}

func (s *Service) WithMandates(m MandateAuthorizer) *Service {
	s.mandates = m
	return s
}

func (s *Service) WithMandateEnforcement(enabled func(context.Context, pgtype.UUID) bool) *Service {
	s.mandateEnforcement = enabled
	return s
}

// Decide returns the authoritative fail-closed decision for one Gateway call.
// Policy lookup failures, missing canonical identities, absent runtime
// capabilities, Ask, and Deny all become Deny. Ledger persistence is
// best-effort and never changes the returned decision.
func (s *Service) Decide(ctx context.Context, call Call) Entry {
	policyDecision := PolicyError
	mandateEnforced := s != nil && s.mandateEnforcement != nil && s.mandateEnforcement(ctx, call.WorkspaceID)
	mandateDenied := mandateEnforced && call.TaskID.Valid && (s.mandates == nil || s.mandates.Authorize(ctx, call.TaskID, call.WorkspaceID, call.AgentID, call.ObservedToolName) != nil)
	// Never resolve a policy for an uncanonicalized input. For a known
	// capability, ObservedToolName is the catalog-validated policy form used by
	// the existing table; the stable capability ID remains the decision key.
	if s != nil && s.policy != nil && call.CanonicalCapabilityID != "" {
		effective, err := s.policy.ResolvePermission(ctx, toolpolicy.Query{
			WorkspaceID:    call.WorkspaceID,
			ToolKey:        call.ObservedToolName,
			RuntimeID:      call.RuntimeID,
			AgentID:        call.AgentID,
			UserID:         call.OwnerID,
			OnBehalfOfID:   call.OnBehalfOfID,
			SystemID:       call.SystemID,
			IsSystem:       call.IsSystem,
			RequestContext: call.RequestContext,
		}, platformaccess.Actor{
			Authenticated: call.AgentID.Valid || call.OwnerID.Valid,
			Agent:         call.AgentID.Valid,
		})
		if err == nil {
			policyDecision = policyDecisionFromSetting(effective.Setting)
		}
	}

	evidenceLevel := call.EvidenceLevel
	if evidenceLevel == "" {
		evidenceLevel = availabilityevidence.LevelDeclared
	}
	if call.EvidenceLevel == "" && s != nil && s.evidence != nil && call.CanonicalCapabilityID != "" {
		evidenceLevel = s.evidence.Lookup(
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
		PolicyDecision:        policyDecision,
		EvidenceLevel:         evidenceLevel,
	})
	if mandateDenied {
		entry.Decision = DecisionDeny
		entry.Reason = "task mandate denied the call"
	}
	if s != nil && s.writer != nil {
		_ = s.writer.Append(ctx, entry)
	}
	return entry
}

// RecordTaskMandateDenial appends the exact call-time mandate verdict before
// policy resolution. It is observability only: the caller has already denied
// the call, and a ledger write failure cannot alter that verdict.
func (s *Service) RecordTaskMandateDenial(ctx context.Context, call Call, verdictCode, message string) Entry {
	evidenceLevel := call.EvidenceLevel
	if evidenceLevel == "" {
		evidenceLevel = availabilityevidence.LevelDeclared
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
		PolicyDecision:        PolicyError,
		EvidenceLevel:         evidenceLevel,
	})
	entry.Decision = DecisionDeny
	entry.Reason = fmt.Sprintf("task mandate %s: %s", verdictCode, message)
	if s != nil && s.writer != nil {
		_ = s.writer.Append(ctx, entry)
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
