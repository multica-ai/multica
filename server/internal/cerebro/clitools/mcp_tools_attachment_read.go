package clitools

// CEREBRO-PATCH(mcp-attachment-read-tools): expose existing attachment read APIs to gateway MCP tools.

import (
	"context"
	"net/url"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func registerAttachmentReadTools(srv *mcp.Server, client *cli.APIClient) {
	srv.RegisterTool(mcp.Tool{
		Name:        "list_attachments",
		Description: "List file attachments on a Multica issue or chat message, including attachment IDs needed by read_attachment.",
		InputSchema: map[string]any{
			"type": "object",
			"required": []string{"target_type", "target_id"},
			"properties": map[string]any{
				"target_type":     map[string]any{"type": "string", "enum": []string{"issue", "chat_message"}, "description": "Attachment owner type: issue or chat_message."},
				"target_id":       map[string]any{"type": "string", "description": "Issue ID or chat message ID matching target_type."},
				"issue_id":        map[string]any{"type": "string", "description": "Issue ID whose issue-level attachments should be listed."},
				"chat_message_id": map[string]any{"type": "string", "description": "Chat message ID whose attachments should be listed."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		issueID := strings.TrimSpace(optString(args, "issue_id"))
		chatMessageID := strings.TrimSpace(optString(args, "chat_message_id"))
		targetType := strings.TrimSpace(optString(args, "target_type"))
		targetID := strings.TrimSpace(optString(args, "target_id"))
		if targetID != "" {
			switch targetType {
			case "issue":
				issueID = targetID
			case "chat_message":
				chatMessageID = targetID
			default:
				return mcp.ErrorResult("target_type must be issue or chat_message"), nil
			}
		}
		if (issueID == "") == (chatMessageID == "") {
			return mcp.ErrorResult("set exactly one attachment target"), nil
		}

		path := "/api/issues/" + url.PathEscape(issueID) + "/attachments"
		if chatMessageID != "" {
			path = "/api/chat/messages/" + url.PathEscape(chatMessageID) + "/attachments"
		}

		var attachments any
		if err := client.GetJSON(ctx, path, &attachments); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(attachments)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "read_attachment",
		Description: "Read text content from an attachment by ID. Supports text-like files and PDFs when the server can extract preview text.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"attachment_id"},
			"properties": map[string]any{
				"attachment_id": map[string]any{"type": "string", "description": "Attachment ID returned by list_attachments, get_issue, list_comments, or chat attachment metadata."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		attachmentID, err := requireString(args, "attachment_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		attachmentID = strings.TrimSpace(attachmentID)
		if attachmentID == "" {
			return mcp.ErrorResult("attachment_id is required"), nil
		}

		text, _, err := client.GetText(ctx, "/api/attachments/"+url.PathEscape(attachmentID)+"/content?agent_read=1")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return mcp.TextResult(text), nil
	})
}
