package clitools

// CEREBRO-PATCH(mcp-note-tools): FIR-2022 — MCP tools to read a note/document,
// read its comments, and search notes/documents by title, body and comments.
// Mirrors mcp_tools_artifact.go; calls the cerebro note HTTP surface
// (server/internal/cerebro/note): GET /api/notes/{id}, /{id}/comments, /search.

import (
	"context"
	"net/url"
	"strconv"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// registerNoteTools wires the read + search MCP tools for notes/documents.
// A "note" is any document (artifact) the caller may see — every kind, not
// only personal notes. Access is the caller's: an agent token resolves to its
// owner, so a private note never leaks into agent results.
func registerNoteTools(srv *mcp.Server, client *cli.APIClient) {
	// -----------------------------------------------------------------------
	// read_note
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "read_note",
		Description: "Read a note/document by ID — returns its title, full body, owner, visibility and scope. Use search_notes first to find the ID, then read_note for the full content.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Note/document (artifact) ID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var result map[string]any
		if err := client.GetJSON(ctx, "/api/notes/"+url.PathEscape(id), &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// list_note_comments
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "list_note_comments",
		Description: "List every comment on a note/document (thread roots + replies, oldest first), including suggestions and their resolution state. Use to read the discussion attached to a note.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Note/document (artifact) ID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var result []map[string]any
		if err := client.GetJSON(ctx, "/api/notes/"+url.PathEscape(id)+"/comments", &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// search_notes
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "search_notes",
		Description: `Full-text search across notes/documents in the workspace — matches the title, the body AND the comments. Returns each match with a match_source ("title", "body" or "comment") and a short snippet showing why it matched. Only returns notes/documents you may see.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"q"},
			"properties": map[string]any{
				"q":      map[string]any{"type": "string", "description": "Search text — matched against title, body and comments"},
				"kind":   map[string]any{"type": "string", "enum": []string{"report", "plan", "decision", "diagram", "note"}, "description": "Restrict to one document kind"},
				"limit":  map[string]any{"type": "integer", "description": "Max results (default 20, max 50)"},
				"offset": map[string]any{"type": "integer", "description": "Offset for pagination"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		q, err := requireString(args, "q")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		params := url.Values{}
		params.Set("q", q)
		if v := optString(args, "kind"); v != "" {
			params.Set("kind", v)
		}
		if v := optInt(args, "limit", 0); v > 0 {
			params.Set("limit", strconv.Itoa(v))
		}
		if v := optInt(args, "offset", 0); v > 0 {
			params.Set("offset", strconv.Itoa(v))
		}
		var result map[string]any
		if err := client.GetJSON(ctx, "/api/notes/search?"+params.Encode(), &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})
}
