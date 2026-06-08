package main

// CEREBRO-PATCH(runtime-agnostic-tool-access): TECH-3071 adapter for the
// read-only effective runtime tool access preview.

import (
	"context"

	"github.com/multica-ai/multica/server/internal/cerebro/toolaccess"
	"github.com/multica-ai/multica/server/internal/handler"
)

type runtimeToolAccessAdapter struct {
	svc *toolaccess.Service
}

func newRuntimeToolAccessAdapter(svc *toolaccess.Service) *runtimeToolAccessAdapter {
	return &runtimeToolAccessAdapter{svc: svc}
}

func (a *runtimeToolAccessAdapter) ListEffectiveTools(ctx context.Context, q handler.RuntimeToolAccessQuery) ([]handler.RuntimeToolEffectiveAccessView, error) {
	rows, err := a.svc.ListEffectiveTools(ctx, toolaccess.Query{
		WorkspaceID:         q.WorkspaceID,
		RuntimeID:           q.RuntimeID,
		RuntimeMode:         q.RuntimeMode,
		RuntimeProvider:     q.RuntimeProvider,
		RuntimeCapabilities: q.RuntimeCapabilities,
		AgentID:             q.AgentID,
		UserID:              q.UserID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]handler.RuntimeToolEffectiveAccessView, 0, len(rows))
	for _, row := range rows {
		out = append(out, handler.RuntimeToolEffectiveAccessView{
			Descriptor: handler.RuntimeToolDescriptorView{
				ToolKey:                  row.Descriptor.ToolKey,
				DisplayName:              row.Descriptor.DisplayName,
				Description:              row.Descriptor.Description,
				Source:                   row.Descriptor.Source,
				RiskClass:                row.Descriptor.RiskClass,
				Protocols:                row.Descriptor.Protocols,
				RecommendedDefaultPolicy: row.Descriptor.RecommendedDefaultPolicy,
			},
			Inventory: handler.RuntimeToolInventoryStateView{
				RuntimeID:     row.Inventory.RuntimeID,
				ToolName:      row.Inventory.ToolName,
				Source:        row.Inventory.Source,
				MCPServerName: row.Inventory.MCPServerName,
				Enabled:       row.Inventory.Enabled,
			},
			Policy: handler.RuntimeToolPolicyStateView{
				Effective: row.Policy.Effective,
				Reason:    row.Policy.Reason,
				DecidedBy: row.Policy.DecidedBy,
				CappedBy:  row.Policy.CappedBy,
			},
			RuntimeGrant: handler.RuntimeToolGrantStateView{
				Effective: row.RuntimeGrant.Effective,
				Reason:    row.RuntimeGrant.Reason,
			},
			Protocol: handler.RuntimeToolProtocolStateView{
				Effective:          row.Protocol.Effective,
				RequiredProtocols:  row.Protocol.RequiredProtocols,
				RuntimeProtocols:   row.Protocol.RuntimeProtocols,
				SelectedProtocol:   row.Protocol.SelectedProtocol,
				SupportsAsk:        row.Protocol.SupportsAsk,
				UnsupportedMessage: row.Protocol.UnsupportedMessage,
			},
			Credential: handler.RuntimeToolCredentialStateView{
				Effective: row.Credential.Effective,
				Reason:    row.Credential.Reason,
			},
			ExposureEffective: handler.RuntimeToolExposureEffectiveView{
				Effective: row.ExposureEffective.Effective,
				Reason:    row.ExposureEffective.Reason,
			},
			Layers: row.Layers,
		})
	}
	return out, nil
}
