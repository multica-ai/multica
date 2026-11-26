package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/redact"
)

type mcpAuditWrite struct {
	Source    string
	Server    string
	Tool      string
	CallID    string
	Arguments json.RawMessage
	Outcome   string
	Error     string
	Duration  *int32
	Provider  string
}

func auditText(s string) pgtype.Text {
	s = strings.TrimSpace(s)
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func truncateAuditError(s string) string {
	s = strings.TrimSpace(redact.Text(s))
	runes := []rune(s)
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return s
}

func (h *Handler) writeMCPActionAudit(ctx context.Context, task db.AgentTaskQueue, workspaceID string, in mcpAuditWrite) {
	if h == nil || h.Queries == nil || strings.TrimSpace(in.Tool) == "" || workspaceID == "" {
		return
	}
	tool := strings.TrimSpace(in.Tool)
	if len([]rune(tool)) > 256 {
		tool = string([]rune(tool)[:256])
	}
	provider := auditText(in.Provider)
	if !provider.Valid && task.RuntimeID.Valid {
		if rt, err := h.getAgentRuntime(ctx, obsmetrics.RuntimeLookupSourceDaemonAPI, task.RuntimeID); err == nil {
			provider = auditText(rt.Provider)
		}
	}
	duration := pgtype.Int4{}
	if in.Duration != nil && *in.Duration >= 0 {
		duration = pgtype.Int4{Int32: *in.Duration, Valid: true}
	}
	row := db.MCPActionAudit{
		WorkspaceID:      parseUUID(workspaceID),
		TaskID:           task.ID,
		AgentID:          task.AgentID,
		IssueID:          task.IssueID,
		ChatSessionID:    task.ChatSessionID,
		OriginatorUserID: task.OriginatorUserID,
		Provider:         provider,
		Source:           in.Source,
		MCPServer:        auditText(in.Server),
		ToolName:         tool,
		CallID:           auditText(in.CallID),
		Arguments:        redact.AuditJSON(in.Arguments),
		Outcome:          in.Outcome,
		Error:            auditText(truncateAuditError(in.Error)),
		DurationMs:       duration,
	}
	if err := h.Queries.UpsertMCPActionAudit(ctx, row); err != nil {
		slog.Warn("mcp action audit write failed", "task_id", uuidToString(task.ID), "tool", tool, "error", err)
	}
}

func hookAuditOutcome(status string) string {
	switch status {
	case "ok":
		return "ok"
	case "timeout":
		return "timeout"
	case "refused":
		return "denied"
	default:
		return "error"
	}
}

func (h *Handler) auditPluginHookCall(ctx context.Context, task db.AgentTaskQueue, workspaceID string, req invokeAgentHookRequest, result service.HookResult, elapsed time.Duration) {
	var duration *int32
	if result.Latency > 0 && result.Latency < 1<<31 {
		v := int32(result.Latency)
		duration = &v
	} else if ms := elapsed.Milliseconds(); ms > 0 && ms < 1<<31 {
		v := int32(ms)
		duration = &v
	}
	h.writeMCPActionAudit(ctx, task, workspaceID, mcpAuditWrite{
		Source:    "plugin_hook",
		Server:    "multica-plugins",
		Tool:      req.HookKey,
		Arguments: req.Input,
		Outcome:   hookAuditOutcome(result.Status),
		Error:     result.Error,
		Duration:  duration,
	})
}

func transcriptArguments(input map[string]any) json.RawMessage {
	if input == nil {
		return nil
	}
	args, ok := input["arguments"]
	if !ok {
		copied := make(map[string]any, len(input))
		for k, v := range input {
			if k == "server" {
				continue
			}
			copied[k] = v
		}
		args = copied
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil
	}
	return raw
}

func (h *Handler) auditTranscriptMCP(ctx context.Context, task db.AgentTaskQueue, workspaceID string, messages []TaskMessageRequest) {
	provider := ""
	if task.RuntimeID.Valid {
		if rt, err := h.getAgentRuntime(ctx, obsmetrics.RuntimeLookupSourceDaemonAPI, task.RuntimeID); err == nil {
			provider = rt.Provider
		}
	}
	serversByCall := map[string]string{}
	for _, msg := range messages {
		if msg.Type != "tool_use" && msg.Type != "tool_result" {
			continue
		}
		server, _ := msg.Input["server"].(string)
		server = strings.TrimSpace(server)
		if msg.Type == "tool_use" && server != "" && msg.CallID != "" {
			serversByCall[msg.CallID] = server
		}
		if server == "" && msg.Type == "tool_result" {
			server = serversByCall[msg.CallID]
		}
		if server == "" || service.SkipProviderStreamMCPServer(server) {
			continue
		}
		write := mcpAuditWrite{
			Source:   "provider_stream",
			Server:   server,
			Tool:     msg.Tool,
			CallID:   msg.CallID,
			Provider: provider,
		}
		switch msg.Type {
		case "tool_use":
			write.Outcome = "started"
			write.Arguments = transcriptArguments(msg.Input)
		default:
			outcome, duration, errText := service.TranscriptMCPOutcome(msg.Output)
			write.Outcome = outcome
			write.Duration = duration
			write.Error = errText
		}
		h.writeMCPActionAudit(ctx, task, workspaceID, write)
	}
}

type reportMCPActionAuditRequest struct {
	Source     string          `json:"source"`
	MCPServer  string          `json:"mcp_server"`
	ToolName   string          `json:"tool_name"`
	CallID     string          `json:"call_id"`
	Arguments  json.RawMessage `json:"arguments"`
	Outcome    string          `json:"outcome"`
	Error      string          `json:"error"`
	DurationMs *int32          `json:"duration_ms"`
}

// ReportMCPActionAudit — POST /api/daemon/tasks/{id}/mcp-action-audits
func (h *Handler) ReportMCPActionAudit(w http.ResponseWriter, r *http.Request) {
	task, scope, ok := h.requireDaemonTaskAccessWithScope(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req reportMCPActionAuditRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.Source {
	case "remote_broker", "plugin_hook", "provider_stream":
	default:
		writeError(w, http.StatusBadRequest, "invalid source")
		return
	}
	switch req.Outcome {
	case "started", "ok", "error", "denied", "timeout", "unavailable":
	default:
		writeError(w, http.StatusBadRequest, "invalid outcome")
		return
	}
	h.writeMCPActionAudit(r.Context(), task, scope.WorkspaceID, mcpAuditWrite{
		Source:    req.Source,
		Server:    req.MCPServer,
		Tool:      req.ToolName,
		CallID:    req.CallID,
		Arguments: req.Arguments,
		Outcome:   req.Outcome,
		Error:     req.Error,
		Duration:  req.DurationMs,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type mcpActionAuditResponse struct {
	ID               string          `json:"id"`
	WorkspaceID      string          `json:"workspace_id"`
	TaskID           string          `json:"task_id"`
	AgentID          string          `json:"agent_id"`
	IssueID          string          `json:"issue_id,omitempty"`
	ChatSessionID    string          `json:"chat_session_id,omitempty"`
	OriginatorUserID string          `json:"originator_user_id,omitempty"`
	Provider         string          `json:"provider,omitempty"`
	Source           string          `json:"source"`
	MCPServer        string          `json:"mcp_server,omitempty"`
	ToolName         string          `json:"tool_name"`
	CallID           string          `json:"call_id,omitempty"`
	Arguments        json.RawMessage `json:"arguments,omitempty"`
	Outcome          string          `json:"outcome"`
	Error            string          `json:"error,omitempty"`
	DurationMs       *int32          `json:"duration_ms,omitempty"`
	CreatedAt        string          `json:"created_at"`
}

func mcpAuditToResponse(row db.MCPActionAudit) mcpActionAuditResponse {
	resp := mcpActionAuditResponse{
		ID:               uuidToString(row.ID),
		WorkspaceID:      uuidToString(row.WorkspaceID),
		TaskID:           uuidToString(row.TaskID),
		AgentID:          uuidToString(row.AgentID),
		IssueID:          uuidToString(row.IssueID),
		ChatSessionID:    uuidToString(row.ChatSessionID),
		OriginatorUserID: uuidToString(row.OriginatorUserID),
		Provider:         row.Provider.String,
		Source:           row.Source,
		MCPServer:        row.MCPServer.String,
		ToolName:         row.ToolName,
		CallID:           row.CallID.String,
		Arguments:        row.Arguments,
		Outcome:          row.Outcome,
		Error:            row.Error.String,
		CreatedAt:        timestampToString(row.CreatedAt),
	}
	if row.DurationMs.Valid {
		v := row.DurationMs.Int32
		resp.DurationMs = &v
	}
	return resp
}

// ListMCPActionAudits — GET /api/workspaces/{id}/mcp-action-audits
func (h *Handler) ListMCPActionAudits(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	taskID := pgtype.UUID{}
	if raw := strings.TrimSpace(r.URL.Query().Get("task_id")); raw != "" {
		parsed, ok := parseUUIDOrBadRequest(w, raw, "task_id")
		if !ok {
			return
		}
		taskID = parsed
	}
	since := pgtype.Timestamptz{}
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "since must be RFC3339")
			return
		}
		since = pgtype.Timestamptz{Time: parsed, Valid: true}
	}
	limit := int32(100)
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		if n > 500 {
			n = 500
		}
		limit = int32(n)
	}
	rows, err := h.Queries.ListMCPActionAudits(r.Context(), workspaceUUID, taskID, since, limit)
	if err != nil {
		slog.Warn("list mcp action audits failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list mcp action audits")
		return
	}
	resp := make([]mcpActionAuditResponse, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, mcpAuditToResponse(row))
	}
	writeJSON(w, http.StatusOK, resp)
}
