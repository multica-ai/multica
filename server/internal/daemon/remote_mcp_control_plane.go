package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

type ManagedMCPPolicyData struct {
	Capability string                     `json:"capability"`
	Revision   int64                      `json:"revision"`
	Rules      []ManagedMCPPolicyRuleData `json:"rules"`
}

type ManagedMCPPolicyRuleData struct {
	TransportKind string `json:"transport_kind"`
	ServerKey     string `json:"server_key"`
	ToolName      string `json:"tool_name"`
	SchemaDigest  string `json:"schema_digest"`
	Effect        string `json:"effect"`
}

type taskManagedMCPControlPlane struct {
	client         *Client
	taskID         string
	daemonToken    string
	providerFamily string
	policy         ManagedMCPPolicyData
}

func newTaskManagedMCPInvocationGate(client *Client, task Task, providerFamily string) remoteMCPInvocationGate {
	if client == nil || task.ManagedMCPPolicy == nil {
		return nil
	}
	control := &taskManagedMCPControlPlane{
		client: client, taskID: task.ID, daemonToken: task.RemoteMCPDaemonToken,
		providerFamily: providerFamily, policy: *task.ManagedMCPPolicy,
	}
	return newManagedMCPInvocationGate(control)
}

func (control *taskManagedMCPControlPlane) SupportsCapability(providerFamily, transportKind string) bool {
	return control != nil && control.client != nil && control.taskID != "" && control.daemonToken != "" &&
		transportKind == managedMCPTransportKind && providerFamily == control.providerFamily &&
		control.policy.Capability == managedMCPPreTransportCapability(providerFamily)
}

func (control *taskManagedMCPControlPlane) LookupRule(_ context.Context, identity managedMCPToolIdentity) (managedMCPPolicyRule, error) {
	if identity.TaskID != control.taskID || identity.ProviderFamily != control.providerFamily || control.policy.Revision < 1 {
		return managedMCPPolicyRule{}, errRemoteMCPPolicyDenied
	}
	for _, candidate := range control.policy.Rules {
		if candidate.TransportKind == identity.TransportKind && candidate.ServerKey == identity.ServerKey && candidate.ToolName == identity.ToolName {
			return managedMCPPolicyRule{
				SchemaDigest: candidate.SchemaDigest, Effect: candidate.Effect,
				PolicyRevision: control.policy.Revision,
			}, nil
		}
	}
	return managedMCPPolicyRule{}, errRemoteMCPPolicyDenied
}

type managedMCPInvocationResponse struct {
	InvocationID      string `json:"invocation_id"`
	ApprovalRequestID string `json:"approval_request_id,omitempty"`
	Status            string `json:"status"`
}

func (control *taskManagedMCPControlPlane) createInvocation(ctx context.Context, invocation remoteMCPInvocation, rule managedMCPPolicyRule) (managedMCPInvocationResponse, error) {
	var response managedMCPInvocationResponse
	body := map[string]any{
		"idempotency_key": invocation.IdempotencyKey,
		"transport_kind":  invocation.TransportKind,
		"server_key":      invocation.ServerKey,
		"tool_name":       invocation.ToolName,
		"schema_digest":   invocation.SchemaDigest,
		"policy_revision": rule.PolicyRevision,
		"argument_bytes":  invocation.ArgumentBytes,
	}
	path := fmt.Sprintf("/api/daemon/tasks/%s/tool-invocations", url.PathEscape(control.taskID))
	if err := control.client.postJSONWithToken(ctx, path, control.daemonToken, body, &response); err != nil {
		return managedMCPInvocationResponse{}, err
	}
	if response.InvocationID == "" || response.Status == "" {
		return managedMCPInvocationResponse{}, errRemoteMCPAuditFailure
	}
	return response, nil
}

func (control *taskManagedMCPControlPlane) LookupApproval(ctx context.Context, invocation remoteMCPInvocation, rule managedMCPPolicyRule) (managedMCPApproval, error) {
	response, err := control.createInvocation(ctx, invocation, rule)
	if err != nil {
		return managedMCPApproval{}, err
	}
	return managedMCPApproval{
		Status: response.Status,
		Grant: remoteMCPInvocationGrant{
			InvocationID: response.InvocationID, ApprovalRequestID: response.ApprovalRequestID,
		},
	}, nil
}

func (control *taskManagedMCPControlPlane) ConsumeApproval(ctx context.Context, invocation remoteMCPInvocation, rule managedMCPPolicyRule, approval managedMCPApproval) error {
	path := fmt.Sprintf("/api/daemon/tasks/%s/tool-approvals/%s/consume",
		url.PathEscape(control.taskID), url.PathEscape(approval.Grant.ApprovalRequestID))
	body := map[string]any{
		"invocation_id":   approval.Grant.InvocationID,
		"transport_kind":  invocation.TransportKind,
		"server_key":      invocation.ServerKey,
		"tool_name":       invocation.ToolName,
		"schema_digest":   invocation.SchemaDigest,
		"policy_revision": rule.PolicyRevision,
	}
	var response struct {
		Authorized bool `json:"authorized"`
	}
	if err := control.client.postJSONWithToken(ctx, path, control.daemonToken, body, &response); err != nil {
		var requestErr *requestError
		if errors.As(err, &requestErr) && (requestErr.StatusCode == http.StatusConflict || requestErr.StatusCode == http.StatusGone) {
			return errRemoteMCPApprovalConsumed
		}
		return err
	}
	if !response.Authorized {
		return errRemoteMCPApprovalConsumed
	}
	return nil
}

func (control *taskManagedMCPControlPlane) CommitStarted(ctx context.Context, invocation remoteMCPInvocation, rule managedMCPPolicyRule, grant remoteMCPInvocationGrant) (remoteMCPInvocationGrant, error) {
	if grant.InvocationID == "" {
		created, err := control.createInvocation(ctx, invocation, rule)
		if err != nil {
			return remoteMCPInvocationGrant{}, err
		}
		if created.Status != "allowed" {
			return remoteMCPInvocationGrant{}, errRemoteMCPAuditFailure
		}
		grant.InvocationID = created.InvocationID
		grant.ApprovalRequestID = created.ApprovalRequestID
	}
	body := map[string]any{
		"event_type":      "started",
		"transport_kind":  invocation.TransportKind,
		"server_key":      invocation.ServerKey,
		"tool_name":       invocation.ToolName,
		"schema_digest":   invocation.SchemaDigest,
		"policy_revision": rule.PolicyRevision,
		"argument_bytes":  invocation.ArgumentBytes,
		"task_message": map[string]any{
			"invocation_id": grant.InvocationID,
			"type":          "tool_use",
			"tool":          invocation.ToolName,
		},
	}
	if err := control.commitEvent(ctx, grant.InvocationID, body); err != nil {
		return remoteMCPInvocationGrant{}, err
	}
	return grant, nil
}

func (control *taskManagedMCPControlPlane) CommitTerminalAndTaskMessage(ctx context.Context, grant remoteMCPInvocationGrant, result remoteMCPInvocationResult) error {
	eventType := "failed"
	if result.OutcomeCode == "succeeded" {
		eventType = "succeeded"
	} else if result.OutcomeCode == "cancelled" {
		eventType = "cancelled"
	}
	invocation := grant.Invocation
	body := map[string]any{
		"event_type":      eventType,
		"transport_kind":  invocation.TransportKind,
		"server_key":      invocation.ServerKey,
		"tool_name":       invocation.ToolName,
		"schema_digest":   invocation.SchemaDigest,
		"policy_revision": grant.PolicyRevision,
		"result_bytes":    result.ResultBytes,
		"duration_ms":     result.DurationMS,
		"outcome_code":    result.OutcomeCode,
		"error_class":     result.ErrorClass,
		"task_message": map[string]any{
			"invocation_id": grant.InvocationID,
			"type":          "tool_result",
			"tool":          invocation.ToolName,
			"outcome_code":  result.OutcomeCode,
			"error_class":   result.ErrorClass,
		},
	}
	return control.commitEvent(ctx, grant.InvocationID, body)
}

func (control *taskManagedMCPControlPlane) commitEvent(ctx context.Context, invocationID string, body map[string]any) error {
	path := fmt.Sprintf("/api/daemon/tasks/%s/tool-invocations/%s/events",
		url.PathEscape(control.taskID), url.PathEscape(invocationID))
	return control.client.postJSONWithToken(ctx, path, control.daemonToken, body, nil)
}
