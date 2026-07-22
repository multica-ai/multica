package clitools

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/issuepropertytools"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

type mcpIssuePropertyDefinition struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Config   map[string]any `json:"config"`
	Archived bool           `json:"archived"`
}

type mcpIssuePropertyContext struct {
	IssueID    string                       `json:"issue_id"`
	Properties []mcpIssuePropertyDefinition `json:"properties"`
	Values     map[string]any               `json:"values"`
}

func loadMCPIssuePropertyContext(ctx context.Context, client *cli.APIClient, issueRef string) (mcpIssuePropertyContext, error) {
	var issue struct {
		ID         string         `json:"id"`
		Properties map[string]any `json:"properties"`
	}
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueRef), &issue); err != nil {
		return mcpIssuePropertyContext{}, err
	}
	var catalog struct {
		Properties []mcpIssuePropertyDefinition `json:"properties"`
	}
	if err := client.GetJSON(ctx, "/api/properties?include_archived=true", &catalog); err != nil {
		return mcpIssuePropertyContext{}, err
	}
	if issue.Properties == nil {
		issue.Properties = map[string]any{}
	}
	return mcpIssuePropertyContext{IssueID: issue.ID, Properties: catalog.Properties, Values: issue.Properties}, nil
}

func resolveMCPIssueProperty(properties []mcpIssuePropertyDefinition, ref string) (mcpIssuePropertyDefinition, error) {
	for _, property := range properties {
		if property.ID == ref || strings.EqualFold(property.Name, strings.TrimSpace(ref)) {
			return property, nil
		}
	}
	return mcpIssuePropertyDefinition{}, fmt.Errorf("property %q not found; call %s to list available properties", ref, issuepropertytools.ListName)
}

func registerIssuePropertyTools(srv *mcp.Server, client *cli.APIClient) {
	srv.RegisterTool(mcp.Tool{Name: issuepropertytools.ListName, Description: issuepropertytools.ListDescription(), InputSchema: issuepropertytools.ListSchema()}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		issueID, err := requireString(args, "issue_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		out, err := loadMCPIssuePropertyContext(ctx, client, issueID)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	srv.RegisterTool(mcp.Tool{Name: issuepropertytools.SetName, Description: issuepropertytools.SetDescription(), InputSchema: issuepropertytools.SetSchema()}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		issueID, err := requireString(args, "issue_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		propertyRef, err := requireString(args, "property")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		value, ok := args["value"]
		if !ok {
			return mcp.ErrorResult("value is required"), nil
		}
		current, err := loadMCPIssuePropertyContext(ctx, client, issueID)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		property, err := resolveMCPIssueProperty(current.Properties, propertyRef)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var out any
		path := "/api/issues/" + url.PathEscape(current.IssueID) + "/properties/" + url.PathEscape(property.ID)
		if err := client.PutJSON(ctx, path, map[string]any{"value": value}, &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	srv.RegisterTool(mcp.Tool{Name: issuepropertytools.UnsetName, Description: issuepropertytools.UnsetDescription(), InputSchema: issuepropertytools.UnsetSchema()}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		issueID, err := requireString(args, "issue_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		propertyRef, err := requireString(args, "property")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		current, err := loadMCPIssuePropertyContext(ctx, client, issueID)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		property, err := resolveMCPIssueProperty(current.Properties, propertyRef)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		path := "/api/issues/" + url.PathEscape(current.IssueID) + "/properties/" + url.PathEscape(property.ID)
		if err := client.DeleteJSON(ctx, path); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(map[string]any{"issue_id": current.IssueID, "property_id": property.ID, "unset": true})
	})
}
