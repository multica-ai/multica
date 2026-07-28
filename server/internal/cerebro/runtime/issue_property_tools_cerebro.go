package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/issuepropertytools"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func gatewayIssuePropertyValues(raw []byte) map[string]any {
	var values map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &values) != nil || values == nil {
		return map[string]any{}
	}
	return values
}

func findGatewayIssueProperty(ctx context.Context, queries *db.Queries, workspaceID db.ListIssuePropertiesParams, ref string) (db.IssueProperty, error) {
	rows, err := queries.ListIssueProperties(ctx, workspaceID)
	if err != nil {
		return db.IssueProperty{}, fmt.Errorf("list issue properties: %w", err)
	}
	ref = strings.TrimSpace(ref)
	for _, row := range rows {
		if util.UUIDToString(row.ID) == ref || strings.EqualFold(row.Name, ref) {
			property, err := queries.GetIssueProperty(ctx, db.GetIssuePropertyParams{ID: row.ID, WorkspaceID: workspaceID.WorkspaceID})
			if err != nil {
				return db.IssueProperty{}, fmt.Errorf("get issue property: %w", err)
			}
			return property, nil
		}
	}
	return db.IssueProperty{}, fmt.Errorf("property %q not found; call %s to list available properties", ref, issuepropertytools.ListName)
}

type FirtalListIssuePropertiesTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalListIssuePropertiesTool) Name() string { return issuepropertytools.ListName }
func (t *FirtalListIssuePropertiesTool) Description() string {
	return issuepropertytools.ListDescription()
}
func (t *FirtalListIssuePropertiesTool) InputSchema() map[string]any {
	return issuepropertytools.ListSchema()
}
func (t *FirtalListIssuePropertiesTool) Call(ctx context.Context, args map[string]any) (string, error) {
	issueRef, err := toolRequireString(args, "issue_id")
	if err != nil {
		return "", err
	}
	issue, err := resolveIssue(ctx, t.queries, t.tctx.WorkspaceID, issueRef)
	if err != nil {
		return "", err
	}
	rows, err := t.queries.ListIssueProperties(ctx, db.ListIssuePropertiesParams{WorkspaceID: t.tctx.WorkspaceID, IncludeArchived: true})
	if err != nil {
		return "", fmt.Errorf("list_issue_properties: %w", err)
	}
	definitions := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		var config map[string]any
		if json.Unmarshal(row.Config, &config) != nil || config == nil {
			config = map[string]any{}
		}
		definitions = append(definitions, map[string]any{
			"id":       util.UUIDToString(row.ID),
			"name":     row.Name,
			"type":     row.Type,
			"config":   config,
			"archived": row.ArchivedAt.Valid,
		})
	}
	result, err := json.Marshal(map[string]any{
		"issue_id":   util.UUIDToString(issue.ID),
		"properties": definitions,
		"values":     gatewayIssuePropertyValues(issue.Properties),
	})
	if err != nil {
		return "", fmt.Errorf("list_issue_properties: marshal result: %w", err)
	}
	return string(result), nil
}

type FirtalSetIssuePropertyTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalSetIssuePropertyTool) Name() string { return issuepropertytools.SetName }
func (t *FirtalSetIssuePropertyTool) Description() string {
	return issuepropertytools.SetDescription()
}
func (t *FirtalSetIssuePropertyTool) InputSchema() map[string]any {
	return issuepropertytools.SetSchema()
}
func (t *FirtalSetIssuePropertyTool) Call(ctx context.Context, args map[string]any) (string, error) {
	issueRef, err := toolRequireString(args, "issue_id")
	if err != nil {
		return "", err
	}
	propertyRef, err := toolRequireString(args, "property")
	if err != nil {
		return "", err
	}
	value, ok := args["value"]
	if !ok {
		return "", fmt.Errorf("value is required")
	}
	issue, err := resolveIssue(ctx, t.queries, t.tctx.WorkspaceID, issueRef)
	if err != nil {
		return "", err
	}
	property, err := findGatewayIssueProperty(ctx, t.queries, db.ListIssuePropertiesParams{WorkspaceID: t.tctx.WorkspaceID, IncludeArchived: true}, propertyRef)
	if err != nil {
		return "", err
	}
	if property.ArchivedAt.Valid {
		return "", fmt.Errorf("property %q is archived and cannot receive new values", property.Name)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal property value: %w", err)
	}
	canonical, err := handler.ValidateIssuePropertyValue(property, raw)
	if err != nil {
		return "", err
	}
	updated, err := t.queries.SetIssuePropertyValue(ctx, db.SetIssuePropertyValueParams{
		ID:          issue.ID,
		WorkspaceID: t.tctx.WorkspaceID,
		Key:         util.UUIDToString(property.ID),
		Value:       canonical,
	})
	if err != nil {
		return "", fmt.Errorf("set_issue_property: %w", err)
	}
	result, err := json.Marshal(map[string]any{"issue_id": util.UUIDToString(updated.ID), "properties": gatewayIssuePropertyValues(updated.Properties)})
	if err != nil {
		return "", fmt.Errorf("set_issue_property: marshal result: %w", err)
	}
	return string(result), nil
}

type FirtalUnsetIssuePropertyTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalUnsetIssuePropertyTool) Name() string { return issuepropertytools.UnsetName }
func (t *FirtalUnsetIssuePropertyTool) Description() string {
	return issuepropertytools.UnsetDescription()
}
func (t *FirtalUnsetIssuePropertyTool) InputSchema() map[string]any {
	return issuepropertytools.UnsetSchema()
}
func (t *FirtalUnsetIssuePropertyTool) Call(ctx context.Context, args map[string]any) (string, error) {
	issueRef, err := toolRequireString(args, "issue_id")
	if err != nil {
		return "", err
	}
	propertyRef, err := toolRequireString(args, "property")
	if err != nil {
		return "", err
	}
	issue, err := resolveIssue(ctx, t.queries, t.tctx.WorkspaceID, issueRef)
	if err != nil {
		return "", err
	}
	property, err := findGatewayIssueProperty(ctx, t.queries, db.ListIssuePropertiesParams{WorkspaceID: t.tctx.WorkspaceID, IncludeArchived: true}, propertyRef)
	if err != nil {
		return "", err
	}
	updated, err := t.queries.DeleteIssuePropertyValue(ctx, db.DeleteIssuePropertyValueParams{
		ID:          issue.ID,
		WorkspaceID: t.tctx.WorkspaceID,
		Key:         util.UUIDToString(property.ID),
	})
	if err != nil {
		return "", fmt.Errorf("unset_issue_property: %w", err)
	}
	result, err := json.Marshal(map[string]any{"issue_id": util.UUIDToString(updated.ID), "properties": gatewayIssuePropertyValues(updated.Properties)})
	if err != nil {
		return "", fmt.Errorf("unset_issue_property: marshal result: %w", err)
	}
	return string(result), nil
}
