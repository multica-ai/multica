// Package service: MemoryHub dependent-stage claim gate (Plan v1.3 A1).
// Owner: ALL-16. This is the SOLE policy evaluator for binding, credential,
// Memory Docket, and attachment prerequisites at claim time. It returns a
// typed MemoryHubClaimPreparation and never a bearer token or plaintext
// secret.
//
// The gate runs BEFORE ClaimAgentTask changes the queue row to dispatched.
// Required failures keep the queue queued; optional degradation is permitted
// only when the execution snapshot explicitly says optional AND all three
// durable fields are present.
package service

import (
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// MemoryGateState is the frozen gate state enum (A1.2).
type MemoryGateState string

const (
	GatePreparing           MemoryGateState = "preparing"
	GateReady               MemoryGateState = "ready"
	GateDegraded            MemoryGateState = "degraded"
	GateBlockedRequired     MemoryGateState = "blocked_required"
	GateBlockedControlPlane MemoryGateState = "blocked_control_plane"
)

// MemoryPrerequisite is one of the four claim-time prerequisite classes.
type MemoryPrerequisite string

const (
	PrereqBinding    MemoryPrerequisite = "binding"
	PrereqCredential MemoryPrerequisite = "credential"
	PrereqDocket     MemoryPrerequisite = "docket"
	PrereqAttachment MemoryPrerequisite = "attachment"
)

// Error codes for the four required-failure classes (A1.2).
const (
	ErrCodeBindingRequiredUnavailable    = "memoryhub_binding_required_unavailable"
	ErrCodeCredentialRequiredUnavailable = "memoryhub_credential_required_unavailable"
	ErrCodeDocketRequiredUnavailable     = "memoryhub_docket_required_unavailable"
	ErrCodeAttachmentRequiredUnavailable = "memoryhub_attachment_required_unavailable"
	ErrCodeOptionalDegraded              = "memoryhub_optional_degraded"
	ErrCodeControlPlaneSyncFailed        = "memoryhub_control_plane_sync_failed"
)

// MemoryPolicy mirrors the execution snapshot memory policy.
type MemoryPolicy string

const (
	PolicyRequired MemoryPolicy = "required"
	PolicyOptional MemoryPolicy = "optional"
)

// GateFailure is a typed gate failure. It carries the error code and the
// durable evidence ref + next wakeup the caller must persist.
type GateFailure struct {
	Prerequisite MemoryPrerequisite
	ErrorCode    string
	EvidenceRef  string
	NextWakeup   time.Time
	// DegradedReason is set only for the optional-degraded path.
	DegradedReason string
}

func (f *GateFailure) Error() string {
	return "memoryhub gate: " + f.ErrorCode
}

// GateOutcome is the result of evaluating the claim gate.
type GateOutcome struct {
	State       MemoryGateState
	Preparation *protocol.MemoryHubClaimPreparation
	Failure     *GateFailure
}

// ClaimGateInput is the frozen input to the gate evaluation.
type ClaimGateInput struct {
	// MemoryPolicy is the frozen execution-snapshot memory policy.
	MemoryPolicy MemoryPolicy
	// BindingsResolved reports whether the required binding resolved.
	BindingsResolved bool
	// CredentialResolved reports whether the required credential resolved.
	CredentialResolved bool
	// DocketResolved reports whether the Memory Docket resolved.
	DocketResolved bool
	// AttachmentResolved reports whether the memory attachment resolved.
	AttachmentResolved bool
	// ControlPlaneHealthy reports whether the binding control plane is
	// healthy for the dependent stage.
	ControlPlaneHealthy bool
	// ProviderNeedsCredential reports whether the selected provider requires
	// a credential for this task.
	ProviderNeedsCredential bool
	// DaemonSupportsClaimV1 reports whether the installed daemon advertised
	// memoryhub-claim-v1.
	DaemonSupportsClaimV1 bool
}

// EvaluateMemoryHubClaimGate applies the A1.2 frozen outcome matrix.
//
//   - A required MemoryHub task is not dispatched to a daemon lacking
//     memoryhub-claim-v1 (queue stays queued).
//   - required binding failure -> blocked_required (binding class).
//   - required credential failure (provider needs one) -> blocked_required
//     (credential class). Credentials for any non-memory purpose are always
//     required; optional is never inferred from an absent input.
//   - docket/attachment: policy required -> blocked_required; policy optional
//     -> degraded ONLY with an explicit degrade_reason/evidence_ref/wakeup.
//   - required inputs resolve -> ready.
//   - control-plane failure -> blocked_control_plane on the affected
//     dependency; unrelated tasks remain schedulable.
func EvaluateMemoryHubClaimGate(in ClaimGateInput) GateOutcome {
	// Daemon capability gate: a required MemoryHub task cannot reach a daemon
	// that cannot carry the claim material.
	if !in.DaemonSupportsClaimV1 {
		return GateOutcome{
			State: GateBlockedRequired,
			Failure: &GateFailure{
				Prerequisite: PrereqAttachment,
				ErrorCode:    "memoryhub_daemon_capability_required",
			},
		}
	}

	// Binding is always required for a MemoryHub task.
	if !in.BindingsResolved {
		return GateOutcome{
			State: GateBlockedRequired,
			Failure: &GateFailure{
				Prerequisite: PrereqBinding,
				ErrorCode:    ErrCodeBindingRequiredUnavailable,
			},
		}
	}

	// Credential: required when the provider needs one. No inferred optional.
	if in.ProviderNeedsCredential && !in.CredentialResolved {
		return GateOutcome{
			State: GateBlockedRequired,
			Failure: &GateFailure{
				Prerequisite: PrereqCredential,
				ErrorCode:    ErrCodeCredentialRequiredUnavailable,
			},
		}
	}

	// Docket + attachment: policy-dependent.
	if !in.DocketResolved || !in.AttachmentResolved {
		if in.MemoryPolicy == PolicyOptional {
			// Optional degradation is permitted; the caller must fill
			// degrade_reason/evidence_ref/next_wakeup on the preparation.
			// Absence of any field is handled as required by the caller
			// (fail-closed).
			return GateOutcome{
				State: GateDegraded,
				Failure: &GateFailure{
					Prerequisite: PrereqAttachment,
					ErrorCode:    ErrCodeOptionalDegraded,
				},
			}
		}
		// Required docket failure takes precedence over attachment.
		if !in.DocketResolved {
			return GateOutcome{
				State: GateBlockedRequired,
				Failure: &GateFailure{
					Prerequisite: PrereqDocket,
					ErrorCode:    ErrCodeDocketRequiredUnavailable,
				},
			}
		}
		return GateOutcome{
			State: GateBlockedRequired,
			Failure: &GateFailure{
				Prerequisite: PrereqAttachment,
				ErrorCode:    ErrCodeAttachmentRequiredUnavailable,
			},
		}
	}

	// Control-plane sync failure: blocks only the dependent stage.
	if !in.ControlPlaneHealthy {
		return GateOutcome{
			State: GateBlockedControlPlane,
			Failure: &GateFailure{
				Prerequisite: PrereqBinding,
				ErrorCode:    ErrCodeControlPlaneSyncFailed,
			},
		}
	}

	return GateOutcome{State: GateReady}
}
