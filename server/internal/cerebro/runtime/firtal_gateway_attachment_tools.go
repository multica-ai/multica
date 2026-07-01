package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type FirtalListAttachmentsTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalListAttachmentsTool) Name() string { return "list_attachments" }
func (t *FirtalListAttachmentsTool) Description() string {
	return "List file attachments on a Multica issue or chat message, including attachment IDs needed by read_attachment."
}
func (t *FirtalListAttachmentsTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"target_type", "target_id"},
		"properties": map[string]any{
			"target_type":     map[string]any{"type": "string", "enum": []string{"issue", "chat_message"}, "description": "Attachment owner type: issue or chat_message."},
			"target_id":       map[string]any{"type": "string", "description": "Issue ID or chat message ID matching target_type."},
			"issue_id":        map[string]any{"type": "string", "description": "Issue ID whose issue-level attachments should be listed."},
			"chat_message_id": map[string]any{"type": "string", "description": "Chat message ID whose attachments should be listed."},
		},
	}
}
func (t *FirtalListAttachmentsTool) Call(ctx context.Context, args map[string]any) (string, error) {
	if t.queries == nil {
		return "", fmt.Errorf("database is not configured")
	}
	issueID := strings.TrimSpace(attachmentStringArg(args, "issue_id"))
	chatMessageID := strings.TrimSpace(attachmentStringArg(args, "chat_message_id"))
	targetType := strings.TrimSpace(attachmentStringArg(args, "target_type"))
	targetID := strings.TrimSpace(attachmentStringArg(args, "target_id"))
	if targetID != "" {
		switch targetType {
		case "issue":
			issueID = targetID
		case "chat_message":
			chatMessageID = targetID
		default:
			return "", fmt.Errorf("target_type must be issue or chat_message")
		}
	}
	if (issueID == "") == (chatMessageID == "") {
		return "", fmt.Errorf("set exactly one attachment target")
	}

	var attachments []db.Attachment
	var err error
	if issueID != "" {
		id, parseErr := util.ParseUUID(issueID)
		if parseErr != nil {
			return "", fmt.Errorf("invalid issue_id: %w", parseErr)
		}
		attachments, err = t.queries.ListAttachmentsByIssue(ctx, db.ListAttachmentsByIssueParams{
			IssueID:     id,
			WorkspaceID: t.tctx.WorkspaceID,
		})
	} else {
		id, parseErr := util.ParseUUID(chatMessageID)
		if parseErr != nil {
			return "", fmt.Errorf("invalid chat_message_id: %w", parseErr)
		}
		attachments, err = t.queries.ListAttachmentsByChatMessage(ctx, db.ListAttachmentsByChatMessageParams{
			ChatMessageID: id,
			WorkspaceID:   t.tctx.WorkspaceID,
		})
	}
	if err != nil {
		return "", err
	}
	rows := make([]map[string]any, 0, len(attachments))
	for _, att := range attachments {
		rows = append(rows, map[string]any{
			"id":           util.UUIDToString(att.ID),
			"filename":     att.Filename,
			"content_type": att.ContentType,
			"size_bytes":   att.SizeBytes,
			"created_at":   att.CreatedAt.Time,
		})
	}
	raw, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

type FirtalReadAttachmentTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalReadAttachmentTool) Name() string { return "read_attachment" }
func (t *FirtalReadAttachmentTool) Description() string {
	return "Read text content from an attachment by ID. Supports text-like files, spreadsheets, documents, and PDFs when the server can extract text."
}
func (t *FirtalReadAttachmentTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"attachment_id"},
		"properties": map[string]any{
			"attachment_id": map[string]any{"type": "string", "description": "Attachment ID returned by list_attachments, get_issue, list_comments, or chat attachment metadata."},
		},
	}
}
func (t *FirtalReadAttachmentTool) Call(ctx context.Context, args map[string]any) (string, error) {
	if t.queries == nil {
		return "", fmt.Errorf("database is not configured")
	}
	if t.tctx.Storage == nil {
		return "", fmt.Errorf("attachment storage is not configured")
	}
	attachmentID := strings.TrimSpace(attachmentStringArg(args, "attachment_id"))
	if attachmentID == "" {
		return "", fmt.Errorf("attachment_id is required")
	}
	id, err := util.ParseUUID(attachmentID)
	if err != nil {
		return "", fmt.Errorf("invalid attachment_id: %w", err)
	}
	att, err := t.queries.GetAttachment(ctx, db.GetAttachmentParams{ID: id, WorkspaceID: t.tctx.WorkspaceID})
	if err != nil {
		return "", err
	}
	blocks, err := gatewayAttachmentBlocks(ctx, t.tctx.Storage, att)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text) != "" {
				parts = append(parts, block.Text)
			}
		case "document":
			if text, ok := extractPDFBlockText(block); ok {
				parts = append(parts, fmt.Sprintf("Attachment %q contents:\n%s", att.Filename, strings.TrimSpace(text)))
			}
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if text == "" {
		return "", fmt.Errorf("attachment %q is not readable as text", att.Filename)
	}
	return text, nil
}

func attachmentStringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

var _ Tool = (*FirtalListAttachmentsTool)(nil)
var _ Tool = (*FirtalReadAttachmentTool)(nil)
