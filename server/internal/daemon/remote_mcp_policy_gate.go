package daemon

import (
	"context"
	"errors"
)

const managedMCPTransportKind = "managed_mcp"

var errRemoteMCPAuditFailure = errors.New("Remote MCP audit commit failed")

type managedMCPToolIdentity struct {
	TaskID         string
	ProviderFamily string
	TransportKind  string
	ServerKey      string
	ToolName       string
}

type managedMCPPolicyRule struct {
	SchemaDigest   string
	Effect         string
	PolicyRevision int64
}

type managedMCPApproval struct {
	Status string
	Grant  remoteMCPInvocationGrant
}

type managedMCPControlPlane interface {
	SupportsCapability(providerFamily, transportKind string) bool
	LookupRule(context.Context, managedMCPToolIdentity) (managedMCPPolicyRule, error)
	LookupApproval(context.Context, remoteMCPInvocation, managedMCPPolicyRule) (managedMCPApproval, error)
	ConsumeApproval(context.Context, remoteMCPInvocation, managedMCPPolicyRule, managedMCPApproval) error
	CommitStarted(context.Context, remoteMCPInvocation, managedMCPPolicyRule, remoteMCPInvocationGrant) (remoteMCPInvocationGrant, error)
	CommitTerminalAndTaskMessage(context.Context, remoteMCPInvocationGrant, remoteMCPInvocationResult) error
}

type managedMCPInvocationGate struct {
	control managedMCPControlPlane
}

func newManagedMCPInvocationGate(control managedMCPControlPlane) *managedMCPInvocationGate {
	return &managedMCPInvocationGate{control: control}
}

func (gate *managedMCPInvocationGate) CheckCapability(providerFamily, transportKind string) error {
	if gate == nil || gate.control == nil || !gate.control.SupportsCapability(providerFamily, transportKind) {
		return errRemoteMCPCapabilityUnsupported
	}
	return nil
}

func (gate *managedMCPInvocationGate) Begin(ctx context.Context, invocation remoteMCPInvocation) (remoteMCPInvocationGrant, error) {
	identity := managedMCPToolIdentity{
		TaskID: invocation.TaskID, ProviderFamily: invocation.ProviderFamily,
		TransportKind: invocation.TransportKind, ServerKey: invocation.ServerKey,
		ToolName: invocation.ToolName,
	}
	rule, err := gate.control.LookupRule(ctx, identity)
	if err != nil {
		return remoteMCPInvocationGrant{}, errRemoteMCPPolicyDenied
	}
	if rule.SchemaDigest != invocation.SchemaDigest {
		return remoteMCPInvocationGrant{}, errRemoteMCPSchemaDrift
	}

	var grant remoteMCPInvocationGrant
	switch rule.Effect {
	case "allow":
	case "require_approval":
		approval, approvalErr := gate.control.LookupApproval(ctx, invocation, rule)
		if approvalErr != nil {
			return remoteMCPInvocationGrant{}, errRemoteMCPAuditFailure
		}
		switch approval.Status {
		case "approved":
		case "pending":
			return remoteMCPInvocationGrant{}, errRemoteMCPApprovalPending
		case "denied":
			return remoteMCPInvocationGrant{}, errRemoteMCPApprovalDenied
		case "expired":
			return remoteMCPInvocationGrant{}, errRemoteMCPApprovalExpired
		case "cancelled":
			return remoteMCPInvocationGrant{}, errRemoteMCPApprovalCancelled
		case "consumed":
			return remoteMCPInvocationGrant{}, errRemoteMCPApprovalConsumed
		default:
			return remoteMCPInvocationGrant{}, errRemoteMCPAuditFailure
		}
		grant = approval.Grant
		if grant.InvocationID == "" || grant.ApprovalRequestID == "" {
			return remoteMCPInvocationGrant{}, errRemoteMCPAuditFailure
		}
		if err := gate.control.ConsumeApproval(ctx, invocation, rule, approval); err != nil {
			if errors.Is(err, errRemoteMCPApprovalConsumed) {
				return remoteMCPInvocationGrant{}, errRemoteMCPApprovalConsumed
			}
			return remoteMCPInvocationGrant{}, errRemoteMCPAuditFailure
		}
	default:
		return remoteMCPInvocationGrant{}, errRemoteMCPPolicyDenied
	}

	grant, err = gate.control.CommitStarted(ctx, invocation, rule, grant)
	if err != nil || grant.InvocationID == "" {
		return remoteMCPInvocationGrant{}, errRemoteMCPAuditFailure
	}
	grant.PolicyRevision = rule.PolicyRevision
	grant.Invocation = invocation
	return grant, nil
}

func (gate *managedMCPInvocationGate) Finish(ctx context.Context, grant remoteMCPInvocationGrant, result remoteMCPInvocationResult) error {
	if gate == nil || gate.control == nil || grant.InvocationID == "" {
		return errRemoteMCPAuditFailure
	}
	if err := gate.control.CommitTerminalAndTaskMessage(ctx, grant, result); err != nil {
		return errRemoteMCPAuditFailure
	}
	return nil
}

func managedMCPPreTransportCapability(providerFamily string) string {
	if providerFamily == "" {
		return ""
	}
	return managedMCPTransportKind + ":" + providerFamily + ":pretransport_v1"
}
