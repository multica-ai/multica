// CEREBRO-PATCH(cerebro-create-file-mcp-tools): MCP CLI wrapper for the
// create_file gateway tool (TECH-3416 Fase 2a). Gateway tool generates files
// server-side from ToolContext; this wrapper does the same via filegen + the
// upload-file API so coding agents on the MCP CLI path get the same surface.
package main

import (
	"context"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/filegen"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func registerCreateFileTools(srv *mcp.Server, client *cli.APIClient) {
	srv.RegisterTool(mcp.Tool{
		Name: "create_file",
		Description: "Create a file from inline content and attach it to an issue or chat reply. " +
			"Supported formats: " + strings.Join(filegen.SupportedFormats(), ", ") + ". " +
			"Provide `content` for text formats (md, txt, html, svg, xml, json) or `rows` for tabular data (csv). " +
			"Pass issue_id to attach to an issue, or chat_message_id to attach to a chat reply.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"filename", "format"},
			"properties": map[string]any{
				"filename": map[string]any{
					"type":        "string",
					"description": "File name shown to the user, e.g. \"rapport\" or \"tal.csv\". Correct extension is appended if missing.",
				},
				"format": map[string]any{
					"type":        "string",
					"enum":        filegen.SupportedFormats(),
					"description": "Output format.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "File body for text formats (md, txt, html, svg, xml, json).",
				},
				"rows": map[string]any{
					"type":        "array",
					"description": "Tabular data for csv: array of rows, each row an array of cell strings. First row may be a header.",
					"items": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
				},
				"issue_id": map[string]any{
					"type":        "string",
					"description": "Issue to attach the file to (omit when attaching to a chat reply).",
				},
				"comment_id": map[string]any{
					"type":        "string",
					"description": "Optional: attach to this comment instead of the issue body.",
				},
				"chat_message_id": map[string]any{
					"type":        "string",
					"description": "Chat reply to attach the file to. Use $MULTICA_CHAT_MESSAGE_ID when running as a chat task.",
				},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		filename, err := requireString(args, "filename")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		format, err := requireString(args, "format")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		issueID := optString(args, "issue_id")
		commentID := optString(args, "comment_id")
		chatMessageID := optString(args, "chat_message_id")
		if issueID == "" && chatMessageID == "" {
			return mcp.ErrorResult("either issue_id or chat_message_id must be set"), nil
		}

		req := filegen.Request{Format: format}
		if c, ok := args["content"].(string); ok {
			req.Content = c
		}
		if rows, ok := args["rows"]; ok {
			if rowSlice, ok := rows.([]any); ok {
				for _, row := range rowSlice {
					if cells, ok := row.([]any); ok {
						var r []string
						for _, c := range cells {
							r = append(r, toString(c))
						}
						req.Rows = append(req.Rows, r)
					}
				}
			}
		}

		res, err := filegen.Build(req)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		if !strings.Contains(filename, ".") {
			filename += "." + res.Ext
		}

		result, err := client.UploadAttachmentTo(ctx, res.Bytes, filename, cli.AttachmentTarget{
			IssueID:       issueID,
			CommentID:     commentID,
			ChatMessageID: chatMessageID,
		})
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
