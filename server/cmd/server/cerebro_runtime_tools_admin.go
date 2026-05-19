package main

// CEREBRO-PATCH(router-runtime-tools-admin): JEH-1710 wiring adapter for the
// unified runtime tool inventory + access-control admin API.
//
// Bridges runtimetools.Service to handler.RuntimeToolsAdminService so the
// handler package never imports cerebrodb generated types.

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/runtimetools"
	"github.com/multica-ai/multica/server/internal/handler"
)

// runtimeToolsAdminAdapter implements handler.RuntimeToolsAdminService by
// translating between handler view types and runtimetools domain types.
type runtimeToolsAdminAdapter struct {
	svc *runtimetools.Service
}

func newRuntimeToolsAdminAdapter(svc *runtimetools.Service) *runtimeToolsAdminAdapter {
	return &runtimeToolsAdminAdapter{svc: svc}
}

func (a *runtimeToolsAdminAdapter) ListTools(ctx context.Context, runtimeID pgtype.UUID) ([]handler.RuntimeToolView, error) {
	tools, err := a.svc.ListTools(ctx, runtimeID)
	if err != nil {
		return nil, err
	}
	out := make([]handler.RuntimeToolView, 0, len(tools))
	for _, t := range tools {
		out = append(out, toolToView(t))
	}
	return out, nil
}

func (a *runtimeToolsAdminAdapter) SetEnabled(ctx context.Context, runtimeID pgtype.UUID, toolName string, enabled bool) (handler.RuntimeToolView, error) {
	tool, err := a.svc.SetEnabled(ctx, runtimeID, toolName, enabled)
	if err != nil {
		return handler.RuntimeToolView{}, err
	}
	return toolToView(tool), nil
}

func (a *runtimeToolsAdminAdapter) ListGroupGrants(ctx context.Context, runtimeID pgtype.UUID) ([]handler.RuntimeToolGroupGrantView, error) {
	grants, err := a.svc.ListGroupGrants(ctx, runtimeID)
	if err != nil {
		return nil, err
	}
	out := make([]handler.RuntimeToolGroupGrantView, 0, len(grants))
	for _, g := range grants {
		out = append(out, handler.RuntimeToolGroupGrantView{
			RuntimeID: uuidString(g.RuntimeID),
			ToolName:  g.ToolName,
			GroupID:   uuidString(g.GroupID),
			GroupName: g.GroupName,
			GrantedAt: timestampString(g.GrantedAt),
		})
	}
	return out, nil
}

func (a *runtimeToolsAdminAdapter) AddGroupGrant(ctx context.Context, runtimeID pgtype.UUID, toolName string, groupID, grantedBy pgtype.UUID) error {
	return a.svc.AddGroupGrant(ctx, runtimeID, toolName, groupID, grantedBy)
}

func (a *runtimeToolsAdminAdapter) RemoveGroupGrant(ctx context.Context, runtimeID pgtype.UUID, toolName string, groupID pgtype.UUID) error {
	return a.svc.RemoveGroupGrant(ctx, runtimeID, toolName, groupID)
}

func (a *runtimeToolsAdminAdapter) ListUserGrants(ctx context.Context, runtimeID pgtype.UUID) ([]handler.RuntimeToolUserGrantView, error) {
	grants, err := a.svc.ListUserGrants(ctx, runtimeID)
	if err != nil {
		return nil, err
	}
	out := make([]handler.RuntimeToolUserGrantView, 0, len(grants))
	for _, g := range grants {
		out = append(out, handler.RuntimeToolUserGrantView{
			RuntimeID:     uuidString(g.RuntimeID),
			ToolName:      g.ToolName,
			UserID:        uuidString(g.UserID),
			UserName:      g.UserName,
			UserEmail:     g.UserEmail,
			UserAvatarURL: g.UserAvatarURL,
			GrantedAt:     timestampString(g.GrantedAt),
		})
	}
	return out, nil
}

func (a *runtimeToolsAdminAdapter) AddUserGrant(ctx context.Context, runtimeID pgtype.UUID, toolName string, userID, grantedBy pgtype.UUID) error {
	return a.svc.AddUserGrant(ctx, runtimeID, toolName, userID, grantedBy)
}

func (a *runtimeToolsAdminAdapter) RemoveUserGrant(ctx context.Context, runtimeID pgtype.UUID, toolName string, userID pgtype.UUID) error {
	return a.svc.RemoveUserGrant(ctx, runtimeID, toolName, userID)
}

func (a *runtimeToolsAdminAdapter) ListAgentOverrides(ctx context.Context, agentID pgtype.UUID) ([]handler.AgentToolOverrideView, error) {
	overrides, err := a.svc.ListAgentOverrides(ctx, agentID)
	if err != nil {
		return nil, err
	}
	out := make([]handler.AgentToolOverrideView, 0, len(overrides))
	for _, o := range overrides {
		out = append(out, handler.AgentToolOverrideView{
			AgentID:   uuidString(o.AgentID),
			ToolName:  o.ToolName,
			Enabled:   o.Enabled,
			UpdatedAt: timestampString(o.UpdatedAt),
		})
	}
	return out, nil
}

func (a *runtimeToolsAdminAdapter) UpsertAgentOverride(ctx context.Context, agentID pgtype.UUID, toolName string, enabled bool, updatedBy pgtype.UUID) (handler.AgentToolOverrideView, error) {
	o, err := a.svc.UpsertAgentOverride(ctx, agentID, toolName, enabled, updatedBy)
	if err != nil {
		return handler.AgentToolOverrideView{}, err
	}
	return handler.AgentToolOverrideView{
		AgentID:   uuidString(o.AgentID),
		ToolName:  o.ToolName,
		Enabled:   o.Enabled,
		UpdatedAt: timestampString(o.UpdatedAt),
	}, nil
}

func (a *runtimeToolsAdminAdapter) DeleteAgentOverride(ctx context.Context, agentID pgtype.UUID, toolName string) error {
	return a.svc.DeleteAgentOverride(ctx, agentID, toolName)
}

func toolToView(t runtimetools.Tool) handler.RuntimeToolView {
	v := handler.RuntimeToolView{
		ID:            uuidString(t.ID),
		RuntimeID:     uuidString(t.RuntimeID),
		Name:          t.Name,
		Source:        t.Source,
		MCPServerName: t.MCPServerName,
		Description:   t.Description,
		Enabled:       t.Enabled,
	}
	if t.LastScannedAt.Valid {
		s := t.LastScannedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		v.LastScannedAt = &s
	}
	return v
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	// pgtype.UUID.Bytes is [16]byte. Format as canonical UUID.
	const hex = "0123456789abcdef"
	out := make([]byte, 36)
	const dashes = "00000000-0000-0000-0000-000000000000"
	copy(out, dashes)
	idx := 0
	for i, b := range u.Bytes {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			idx++
		}
		out[idx] = hex[b>>4]
		idx++
		out[idx] = hex[b&0x0f]
		idx++
	}
	return string(out)
}

func timestampString(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format("2006-01-02T15:04:05.000Z")
}
