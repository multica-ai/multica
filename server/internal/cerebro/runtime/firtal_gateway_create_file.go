package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	googleuuid "github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/filegen"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// FirtalCreateFileTool lets a chat/gateway agent produce a file from inline
// content and attach it to the current chat or issue (TECH-3416 Fase 2a).
//
// Unlike the coding-agent add_attachment tool (which reads a path off disk),
// gateway agents have no filesystem, so this tool starts from content the
// model emits and renders the bytes server-side via packages-free filegen,
// then stores + records the attachment. Every file it creates carries its
// origin metadata: uploader = the calling agent, plus the chat-message or
// issue it belongs to (CreateAttachmentParams below). Whether the tool is
// offered at all is decided by the runtime tool cascade / toolpolicy chain —
// this handler does no permission decision of its own.
type FirtalCreateFileTool struct {
	queries *db.Queries
	tctx    ToolContext
}

func (t *FirtalCreateFileTool) Name() string { return "create_file" }

func (t *FirtalCreateFileTool) Description() string {
	return "Create a file from inline content and attach it to the current chat or issue. " +
		"Supported formats: " + strings.Join(filegen.SupportedFormats(), ", ") + ". " +
		"Provide `content` for text formats (md, txt, html, svg, xml, json) or `rows` for tabular data (csv). " +
		"The file is attached to the current conversation by default; pass issue_id to target a specific issue."
}

func (t *FirtalCreateFileTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"filename", "format"},
		"properties": map[string]any{
			"filename": map[string]any{
				"type":        "string",
				"description": "File name to show the user, e.g. \"rapport\" or \"tal.csv\". The correct extension is appended automatically if missing.",
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
				"description": "Tabular data for csv: an array of rows, each row an array of cell strings. First row may be a header.",
				"items": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			},
			"issue_id": map[string]any{
				"type":        "string",
				"description": "Optional: attach to this issue (UUID or identifier like JEH-123) instead of the current conversation.",
			},
		},
	}
}

func (t *FirtalCreateFileTool) Call(ctx context.Context, args map[string]any) (string, error) {
	if !t.tctx.AgentID.Valid {
		return "", fmt.Errorf("create_file: missing agent context")
	}
	if t.tctx.Storage == nil {
		return "", fmt.Errorf("create_file: file storage is not configured on this runtime")
	}

	filename, _ := args["filename"].(string)
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return "", fmt.Errorf("create_file: filename is required")
	}
	format, _ := args["format"].(string)

	req := filegen.Request{Format: format}
	if c, ok := args["content"].(string); ok {
		req.Content = c
	}
	if rows, ok := args["rows"]; ok {
		parsed, err := coerceRows(rows)
		if err != nil {
			return "", fmt.Errorf("create_file: %w", err)
		}
		req.Rows = parsed
	}

	res, err := filegen.Build(req)
	if err != nil {
		return "", err
	}

	filename = ensureExt(filename, res.Ext)

	target, err := t.resolveTarget(ctx, args)
	if err != nil {
		return "", err
	}

	// UUIDv7 doubles as the attachment ID and the storage key, matching the
	// upload-file handler so storage layout stays uniform.
	id, err := googleuuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("create_file: generate id: %w", err)
	}
	wsID := util.UUIDToString(t.tctx.WorkspaceID)
	key := "workspaces/" + wsID + "/" + id.String() + "." + res.Ext

	link, err := t.tctx.Storage.Upload(ctx, key, res.Bytes, res.ContentType, filename)
	if err != nil {
		return "", fmt.Errorf("create_file: upload: %w", err)
	}

	params := db.CreateAttachmentParams{
		ID:           pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID:  t.tctx.WorkspaceID,
		UploaderType: "agent",
		UploaderID:   t.tctx.AgentID,
		Filename:     filename,
		Url:          link,
		ContentType:  res.ContentType,
		SizeBytes:    int64(len(res.Bytes)),
	}
	target.apply(&params)

	att, err := t.queries.CreateAttachment(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create_file: record attachment: %w", err)
	}

	out, err := json.Marshal(map[string]any{
		"attachment_id": util.UUIDToString(att.ID),
		"filename":      att.Filename,
		"content_type":  att.ContentType,
		"size_bytes":    att.SizeBytes,
		"attached_to":   target.label,
	})
	if err != nil {
		return "", fmt.Errorf("create_file: marshal result: %w", err)
	}
	return string(out), nil
}

// attachTarget is the resolved destination an attachment is linked to.
type attachTarget struct {
	issueID       pgtype.UUID
	chatMessageID pgtype.UUID
	label         string
}

func (a attachTarget) apply(p *db.CreateAttachmentParams) {
	p.IssueID = a.issueID
	p.ChatMessageID = a.chatMessageID
}

// resolveTarget decides where the file attaches. An explicit issue_id arg wins;
// otherwise it falls back to the surface the task runs on (chat message or
// issue) pinned in the ToolContext. At least one target must resolve.
func (t *FirtalCreateFileTool) resolveTarget(ctx context.Context, args map[string]any) (attachTarget, error) {
	if ref, ok := args["issue_id"].(string); ok && strings.TrimSpace(ref) != "" {
		issue, err := resolveIssue(ctx, t.queries, t.tctx.WorkspaceID, ref)
		if err != nil {
			return attachTarget{}, err
		}
		return attachTarget{issueID: issue.ID, label: "issue:" + util.UUIDToString(issue.ID)}, nil
	}
	if t.tctx.ChatMessageID.Valid {
		return attachTarget{chatMessageID: t.tctx.ChatMessageID, label: "chat"}, nil
	}
	if t.tctx.IssueID.Valid {
		return attachTarget{issueID: t.tctx.IssueID, label: "issue:" + util.UUIDToString(t.tctx.IssueID)}, nil
	}
	return attachTarget{}, fmt.Errorf("create_file: no attachment target — pass issue_id or run inside a chat/issue")
}

// ensureExt appends ".<ext>" when filename has no extension or a different one,
// so a model that says "rapport" or "tal.txt" for a csv still gets "rapport.csv".
func ensureExt(filename, ext string) string {
	if ext == "" {
		return filename
	}
	if strings.EqualFold(strings.TrimPrefix(path.Ext(filename), "."), ext) {
		return filename
	}
	return filename + "." + ext
}

// coerceRows converts the JSON tool argument (array of arrays) into [][]string,
// rendering numbers/bools as their string form so a model can pass mixed cells.
func coerceRows(raw any) ([][]string, error) {
	outer, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("rows must be an array of rows")
	}
	rows := make([][]string, 0, len(outer))
	for i, r := range outer {
		inner, ok := r.([]any)
		if !ok {
			return nil, fmt.Errorf("row %d must be an array of cells", i)
		}
		cells := make([]string, 0, len(inner))
		for _, c := range inner {
			cells = append(cells, cellToString(c))
		}
		rows = append(rows, cells)
	}
	return rows, nil
}

func cellToString(c any) string {
	switch v := c.(type) {
	case string:
		return v
	case nil:
		return ""
	case float64:
		// JSON numbers decode to float64; print without a trailing ".0" for ints.
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}
