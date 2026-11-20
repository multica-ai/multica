package entitlement

import (
	"context"
	"time"

	"github.com/google/uuid"
)

const SchemaVersion = 1

type GateName string

const (
	GateAutopilotRuns GateName = "autopilot_runs"
	// GateMembers caps a workspace's members. Guests take no slot. It has no
	// period: the limit is a ceiling on headcount, not a budget that resets.
	GateMembers GateName = "members"
	// GateStorageBytes caps the attachment bytes a workspace holds. Like
	// members it has no period: the limit is what the workspace may hold now.
	GateStorageBytes GateName = "storage_bytes"
)

func (n GateName) valid() bool {
	switch n {
	case GateAutopilotRuns, GateMembers, GateStorageBytes:
		return true
	default:
		return false
	}
}

type Action string

// Cloud chooses one of these per gate. ActionObserve is also Multica's local
// downgrade of a stale cached ActionEnforce.
const (
	ActionOff     Action = "off"
	ActionObserve Action = "observe"
	ActionEnforce Action = "enforce"

	// NotificationFirstRejectionPerPeriod notifies on the first rejection of
	// a period only.
	NotificationFirstRejectionPerPeriod = "first_rejection_per_period"
	// NotificationFirstRejectionPerLimit notifies on the first rejection for
	// each limit within a period, so a limit raised by an add-on purchase
	// re-arms the notice (policy revision 6).
	NotificationFirstRejectionPerLimit = "first_rejection_per_limit"
)

type Reason string

const (
	ReasonDisabled          Reason = "disabled"
	ReasonInvalidWorkspace  Reason = "invalid_workspace"
	ReasonUnknownGate       Reason = "unknown_gate"
	ReasonCacheFresh        Reason = "cache_fresh"
	ReasonRefreshed         Reason = "refreshed"
	ReasonStale             Reason = "stale"
	ReasonUnavailable       Reason = "unavailable"
	ReasonInvalidPolicy     Reason = "invalid_policy"
	ReasonVersionRegression Reason = "version_regression"
	ReasonStub              Reason = "stub"
	// ReasonFeatureMissing and ReasonFeatureInvalid fail one paid feature open
	// without touching the rest of an otherwise valid policy.
	ReasonFeatureMissing Reason = "feature_missing"
	ReasonFeatureInvalid Reason = "feature_invalid"
	// ReasonGateMissing and ReasonGateInvalid fail one optional gate open the
	// same way, leaving autopilot_runs and the features untouched.
	ReasonGateMissing Reason = "gate_missing"
	ReasonGateInvalid Reason = "gate_invalid"
)

// Gate is the effective instruction for one generic enforcement point. Limits
// and period boundaries come from Cloud; this package does not derive them.
type Gate struct {
	Action        Action
	Limit         *int
	PeriodStart   *time.Time
	PeriodEnd     *time.Time
	ResetAt       *time.Time
	Notifications *NotificationPolicy
}

type NotificationPolicy struct {
	OnRejection string
}

// Decision carries enough source information for consumers to audit why a gate
// was enforced, observed, or disabled without exposing Cloud's private inputs.
type Decision struct {
	Gate                Gate
	Reason              Reason
	PolicyRevision      int64
	SubscriptionVersion int64
	CloudValidUntil     time.Time
}

// Provider is the only interface the autopilot consumer
// needs. It deliberately has no error return: every failure is represented by a
// fail-open Decision with ActionOff (or ActionObserve for a bounded stale
// snapshot that can no longer enforce).
//
// Consumers handle the three actions the same way for every gate: off skips
// the gate entirely, observe admits the operation and records an over-limit
// attempt, enforce rejects it.
type Provider interface {
	Gate(ctx context.Context, workspaceID uuid.UUID, name GateName) Decision
}

// Refresher re-reads one gate ahead of its cached lifetime. A consumer calls
// it only right before rejecting, so a plan change made moments ago (an
// upgrade that was just paid for) is not refused on a policy cached before it.
// A Provider without it rejects on the cached decision.
type Refresher interface {
	RefreshGate(ctx context.Context, workspaceID uuid.UUID, name GateName) Decision
}

// FreshRefresher re-reads one gate from Cloud now, skipping the short reuse
// window RefreshGate applies; concurrent callers for a workspace share one
// fetch. The autopilot quota calls it right before rejecting a run, so a limit
// Cloud raised moments ago (a paid add-on confirmed by Stripe's webhook) is
// never refused on a policy any instance cached before it.
type FreshRefresher interface {
	RefreshGateNow(ctx context.Context, workspaceID uuid.UUID, name GateName) Decision
}

// FeatureName is a stable paid-feature key. Unlike a GateName it meters
// nothing: it says whether a workspace may add to the feature's configuration.
type FeatureName string

const (
	FeaturePrivateProjects FeatureName = "private_projects"
	// FeatureGuests covers adding guests (MUL-9420): inviting one, making a
	// member a guest, and sharing more projects with a guest.
	FeatureGuests FeatureName = "guests"
	// FeatureAgentInvocationMembers covers letting specific members invoke an
	// agent: creating an agent with a member list, adding to the list, and
	// switching from "only me" or "whole workspace" to specific members.
	FeatureAgentInvocationMembers FeatureName = "agent_invocation_members"
	// FeatureRuntimeMemberSharing covers sharing a private runtime with
	// specific members: adding anyone to its shared_with list.
	FeatureRuntimeMemberSharing FeatureName = "runtime_member_sharing"
	// FeatureTeamAppAccounts covers team Apps accounts: connecting one for an
	// app that has none, giving an agent a team-account app or a higher level
	// on one, and custom team rules.
	FeatureTeamAppAccounts FeatureName = "team_app_accounts"
)

// Features lists every paid feature this consumer understands.
func Features() []FeatureName {
	return []FeatureName{
		FeaturePrivateProjects,
		FeatureGuests,
		FeatureAgentInvocationMembers,
		FeatureRuntimeMemberSharing,
		FeatureTeamAppAccounts,
	}
}

func (n FeatureName) Valid() bool {
	switch n {
	case FeaturePrivateProjects, FeatureGuests, FeatureAgentInvocationMembers, FeatureRuntimeMemberSharing, FeatureTeamAppAccounts:
		return true
	default:
		return false
	}
}

// FeatureDecision is the effective restriction on one paid feature:
//
//   - off: the feature is fully usable.
//   - observe: fully usable; each attempt to add to its configuration is
//     recorded. A stale cached enforce is also downgraded to observe, with
//     Downgraded set, so a Cloud outage never keeps blocking.
//   - enforce: existing configuration keeps working and can always be reduced
//     or removed; attempts to add to it are rejected.
//
// Reads never consult a FeatureDecision. Access control built on a paid
// feature (who sees a private project, who may invoke an agent) stays exactly
// as configured whatever the decision is.
type FeatureDecision struct {
	Action Action
	Reason Reason
	// Downgraded is set when Cloud sent enforce but the cached policy has gone
	// stale, so Action was lowered to observe. A stale policy whose feature
	// was already observe keeps Downgraded false: Reason stale alone does not
	// say whether anything would have been blocked.
	Downgraded          bool
	PolicyRevision      int64
	SubscriptionVersion int64
	CloudValidUntil     time.Time
}

// Restricted reports whether additions to the feature are rejected.
func (d FeatureDecision) Restricted() bool { return d.Action == ActionEnforce }

// FeatureProvider is the source of paid-feature decisions. The Cloud policy
// client implements it today; a self-hosted license provider can replace it
// without changing any caller. Like Provider it has no error return: every
// failure is a fail-open decision.
type FeatureProvider interface {
	Feature(ctx context.Context, workspaceID uuid.UUID, name FeatureName) FeatureDecision
}

// Observer is a low-cardinality instrumentation seam. Implementations must not
// attach workspace IDs or policy values as metric labels.
type Observer interface {
	RecordEntitlementCache(outcome string)
	RecordEntitlementRefresh(outcome string, durationSeconds float64)
	RecordEntitlementDecision(gate, action, reason string)
	RecordEntitlementVersionRegression()
}

func offDecision(reason Reason) Decision {
	return Decision{Gate: Gate{Action: ActionOff}, Reason: reason}
}

func cloneDecision(in Decision) Decision {
	in.Gate = cloneGate(in.Gate)
	return in
}

func cloneGate(in Gate) Gate {
	out := in
	if in.Limit != nil {
		value := *in.Limit
		out.Limit = &value
	}
	if in.PeriodStart != nil {
		value := *in.PeriodStart
		out.PeriodStart = &value
	}
	if in.PeriodEnd != nil {
		value := *in.PeriodEnd
		out.PeriodEnd = &value
	}
	if in.ResetAt != nil {
		value := *in.ResetAt
		out.ResetAt = &value
	}
	if in.Notifications != nil {
		out.Notifications = &NotificationPolicy{
			OnRejection: in.Notifications.OnRejection,
		}
	}
	return out
}
