package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/lark"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

const feishuDocumentsContributionPrefix = "builtin:feishu-documents:"

const feishuDocumentsCallTimeout = 30 * time.Second

var (
	errFeishuDocumentsCapabilityDenied = errors.New("feishu documents capability denied")
	errFeishuDocumentsUnavailable      = errors.New("feishu documents capability unavailable")
)

// FeishuDocumentsConnection builds the fixed, task-scoped built-in MCP connection.
func FeishuDocumentsConnection(publicURL, taskID string, installation lark.Installation) (remotemcp.Connection, error) {
	base, err := url.Parse(strings.TrimRight(publicURL, "/"))
	if err != nil || base.Scheme != "https" || base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return remotemcp.Connection{}, errors.New("feishu documents: public URL must be an absolute HTTPS origin")
	}
	if taskID == "" || !installation.ID.Valid || !installation.UpdatedAt.Valid {
		return remotemcp.Connection{}, errors.New("feishu documents: task and installation revision are required")
	}
	revision := installation.UpdatedAt.Time.UnixNano()
	if revision <= 0 {
		return remotemcp.Connection{}, errors.New("feishu documents: invalid installation revision")
	}
	installationID := util.UUIDToString(installation.ID)
	contributionID := feishuDocumentsContributionPrefix + installationID + ":" + strconv.FormatInt(revision, 10)
	tools, err := feishuDocumentTools()
	if err != nil {
		return remotemcp.Connection{}, err
	}
	toolSetDigest, err := remotemcp.ToolSetDigest(tools)
	if err != nil {
		return remotemcp.Connection{}, err
	}
	endpoint := strings.TrimRight(publicURL, "/") + "/api/daemon/tasks/" + url.PathEscape(taskID) +
		"/feishu-documents/" + url.PathEscape(contributionID) + "/mcp"
	return remotemcp.Connection{
		InstallationID:       installationID,
		ContributionID:       contributionID,
		ContributionKey:      "feishu-documents",
		ConfigID:             installationID,
		ConfigRevision:       revision,
		Endpoint:             endpoint,
		Transport:            "streamable-http",
		ProtocolVersions:     remotemcp.SupportedProtocolVersions(),
		EndpointAllowedHosts: []string{strings.ToLower(base.Hostname())},
		CredentialHeader:     "Authorization",
		ApprovedTools:        tools,
		ToolSchemaDigest:     toolSetDigest,
		FailurePolicy:        "optional",
	}, nil
}

func parseFeishuDocumentsContribution(raw string) (pgtype.UUID, int64, bool) {
	trimmed, ok := strings.CutPrefix(raw, feishuDocumentsContributionPrefix)
	if !ok {
		return pgtype.UUID{}, 0, false
	}
	installationID, revisionRaw, ok := strings.Cut(trimmed, ":")
	if !ok || installationID == "" || revisionRaw == "" || strings.Contains(revisionRaw, ":") {
		return pgtype.UUID{}, 0, false
	}
	var parsedID pgtype.UUID
	if err := parsedID.Scan(installationID); err != nil || !parsedID.Valid {
		return pgtype.UUID{}, 0, false
	}
	revision, err := strconv.ParseInt(revisionRaw, 10, 64)
	if err != nil || revision <= 0 {
		return pgtype.UUID{}, 0, false
	}
	return parsedID, revision, true
}

func feishuDocumentTools() ([]remotemcp.Tool, error) {
	definitions := []struct {
		name        string
		description string
		properties  map[string]any
		required    []string
		risk        string
	}{
		{
			name:        "feishu_docs_fetch",
			description: "Read a bounded Feishu document scope as the Agent's connected bot. The bot must already have document access; use fresh block IDs before structural edits.",
			properties: map[string]any{
				"document_url":   documentURLSchema(),
				"scope":          map[string]any{"type": "string", "enum": []string{"full", "outline", "section", "keyword", "range"}},
				"keyword":        map[string]any{"type": "string", "maxLength": lark.DocumentMaxPatternBytes},
				"start_block_id": blockIDSchema(),
				"end_block_id":   blockIDSchema(),
			},
			required: []string{"document_url"}, risk: "read",
		},
		{
			name:        "feishu_docs_append",
			description: "Append bounded text with the connected Feishu bot after a fresh read, then verify. Large edits require preview and confirmation; uncertain writes must not be retried blindly.",
			properties:  map[string]any{"document_url": documentURLSchema(), "content": contentSchema(), "confirmed": booleanSchema()},
			required:    []string{"document_url", "content"}, risk: "write",
		},
		{
			name:        "feishu_docs_replace_text",
			description: "Replace exactly one short text match with the connected Feishu bot after a fresh read. Zero or multiple matches fail; verify after writing and never blindly retry an uncertain result.",
			properties:  map[string]any{"document_url": documentURLSchema(), "pattern": patternSchema(), "content": contentSchema(), "confirmed": booleanSchema()},
			required:    []string{"document_url", "pattern", "content"}, risk: "write",
		},
		{
			name:        "feishu_docs_insert_after",
			description: "Insert bounded ordinary content after a freshly-read block using the connected Feishu bot. Complex unsupported resources are rejected; verify after writing.",
			properties:  map[string]any{"document_url": documentURLSchema(), "block_id": blockIDSchema(), "content": contentSchema(), "confirmed": booleanSchema()},
			required:    []string{"document_url", "block_id", "content"}, risk: "write",
		},
		{
			name:        "feishu_docs_replace_block",
			description: "Replace one block or a small continuous range with the connected Feishu bot after a fresh read. Range and large edits require preview and confirmation; unsupported resources are rejected.",
			properties:  map[string]any{"document_url": documentURLSchema(), "block_id": blockIDSchema(), "start_block_id": blockIDSchema(), "end_block_id": blockIDSchema(), "content": contentSchema(), "confirmed": booleanSchema()},
			required:    []string{"document_url", "content"}, risk: "write",
		},
		{
			name:        "feishu_docs_delete_block",
			description: "Delete one block or a small continuous range with the connected Feishu bot only after preview and explicit confirmation. Read fresh IDs first and verify after writing.",
			properties:  map[string]any{"document_url": documentURLSchema(), "block_id": blockIDSchema(), "start_block_id": blockIDSchema(), "end_block_id": blockIDSchema(), "confirmed": booleanSchema()},
			required:    []string{"document_url", "confirmed"}, risk: "write",
		},
	}
	tools := make([]remotemcp.Tool, 0, len(definitions))
	for _, definition := range definitions {
		schema, err := json.Marshal(map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           definition.properties,
			"required":             definition.required,
		})
		if err != nil {
			return nil, err
		}
		tools = append(tools, remotemcp.Tool{
			Name: definition.name, Description: definition.description,
			InputSchema: schema, SchemaDigest: remotemcp.DigestBytes(schema), Risk: definition.risk,
		})
	}
	return tools, nil
}

func documentURLSchema() map[string]any {
	return map[string]any{"type": "string", "format": "uri", "description": "Explicit Feishu /docx/ or /wiki/ HTTPS URL."}
}

func blockIDSchema() map[string]any {
	return map[string]any{"type": "string", "maxLength": 128}
}

func patternSchema() map[string]any {
	return map[string]any{"type": "string", "maxLength": lark.DocumentMaxPatternBytes}
}

func contentSchema() map[string]any {
	return map[string]any{"type": "string", "maxLength": lark.DocumentMaxContentBytes}
}

func booleanSchema() map[string]any {
	return map[string]any{"type": "boolean"}
}

type feishuDocumentExecutor interface {
	Execute(context.Context, lark.DocumentTaskScope, lark.DocumentOperation, lark.DocumentToolInput) (lark.DocumentToolResult, *lark.DocumentToolError)
}

type feishuDocumentsScopeLoader func(context.Context, *http.Request, string, string) (feishuDocumentsScope, error)

type feishuDocumentsScope struct {
	Task         db.AgentTaskQueue
	Runtime      db.AgentRuntime
	Agent        db.Agent
	Installation lark.Installation
	WorkspaceID  pgtype.UUID
	DaemonID     string
}

func (scope feishuDocumentsScope) documentTaskScope() lark.DocumentTaskScope {
	return lark.DocumentTaskScope{
		TaskID:               scope.Task.ID,
		WorkspaceID:          scope.WorkspaceID,
		AgentID:              scope.Agent.ID,
		RuntimeID:            scope.Runtime.ID,
		DaemonID:             scope.DaemonID,
		InstallationID:       scope.Installation.ID,
		InstallationRevision: scope.Installation.UpdatedAt.Time.UnixNano(),
	}
}

func validateFeishuDocumentsScope(scope feishuDocumentsScope, installationID pgtype.UUID, revision int64) bool {
	if !scope.Task.ID.Valid || !scope.Task.AgentID.Valid || !scope.Task.RuntimeID.Valid ||
		(scope.Task.Status != "dispatched" && scope.Task.Status != "running") ||
		!scope.WorkspaceID.Valid || scope.DaemonID == "" {
		return false
	}
	if !scope.Runtime.ID.Valid || scope.Runtime.ID != scope.Task.RuntimeID ||
		scope.Runtime.WorkspaceID != scope.WorkspaceID || !scope.Runtime.DaemonID.Valid ||
		scope.Runtime.DaemonID.String != scope.DaemonID {
		return false
	}
	if !scope.Agent.ID.Valid || scope.Agent.ID != scope.Task.AgentID ||
		scope.Agent.WorkspaceID != scope.WorkspaceID || scope.Agent.RuntimeID != scope.Task.RuntimeID ||
		scope.Agent.ArchivedAt.Valid {
		return false
	}
	return installationID.Valid && revision > 0 && scope.Installation.ID == installationID &&
		scope.Installation.WorkspaceID == scope.WorkspaceID && scope.Installation.AgentID == scope.Agent.ID &&
		scope.Installation.Status == "active" && scope.Installation.UpdatedAt.Valid &&
		scope.Installation.UpdatedAt.Time.UnixNano() == revision
}

func (h *Handler) loadFeishuDocumentsScope(ctx context.Context, r *http.Request, taskID, contributionID string) (feishuDocumentsScope, error) {
	if h.Queries == nil || h.TaskService == nil {
		return feishuDocumentsScope{}, errFeishuDocumentsUnavailable
	}
	taskUUID, err := util.ParseUUID(taskID)
	if err != nil {
		return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
	}
	_, _, ok := parseFeishuDocumentsContribution(contributionID)
	if !ok {
		return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
	}
	task, err := h.Queries.GetAgentTask(ctx, taskUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
		}
		return feishuDocumentsScope{}, errFeishuDocumentsUnavailable
	}
	if task.ID != taskUUID || !task.AgentID.Valid || !task.RuntimeID.Valid ||
		(task.Status != "dispatched" && task.Status != "running") {
		return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
	}
	workspaceID, err := util.ParseUUID(h.TaskService.ResolveTaskWorkspaceID(ctx, task))
	if err != nil {
		return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
	}
	daemonID := middleware.DaemonIDFromContext(r.Context())
	if middleware.DaemonWorkspaceIDFromContext(r.Context()) != util.UUIDToString(workspaceID) || daemonID == "" {
		return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
	}
	runtime, err := h.Queries.GetAgentRuntime(ctx, task.RuntimeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
		}
		return feishuDocumentsScope{}, errFeishuDocumentsUnavailable
	}
	if runtime.ID != task.RuntimeID || runtime.WorkspaceID != workspaceID || !runtime.DaemonID.Valid ||
		runtime.DaemonID.String != daemonID {
		return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
	}
	agent, err := h.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
		}
		return feishuDocumentsScope{}, errFeishuDocumentsUnavailable
	}
	if agent.ID != task.AgentID || agent.WorkspaceID != workspaceID || agent.RuntimeID != task.RuntimeID ||
		agent.ArchivedAt.Valid {
		return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
	}
	installation, err := lark.NewChannelStore(h.Queries).GetActiveLarkInstallationForAgent(ctx, workspaceID, task.AgentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
		}
		return feishuDocumentsScope{}, errFeishuDocumentsUnavailable
	}
	return feishuDocumentsScope{
		Task: task, Runtime: runtime, Agent: agent, Installation: installation,
		WorkspaceID: workspaceID, DaemonID: daemonID,
	}, nil
}

func (h *Handler) authorizeFeishuDocumentsRequest(r *http.Request) (feishuDocumentsScope, error) {
	if middleware.DaemonAuthPathFromContext(r.Context()) != middleware.DaemonAuthPathDaemonToken {
		return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
	}
	taskID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
	}
	installationID, revision, ok := parseFeishuDocumentsContribution(chi.URLParam(r, "contributionId"))
	if !ok {
		return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
	}
	loader := h.feishuDocumentsScopeLoader
	if loader == nil {
		loader = h.loadFeishuDocumentsScope
	}
	scope, err := loader(r.Context(), r, chi.URLParam(r, "id"), chi.URLParam(r, "contributionId"))
	if err != nil {
		return feishuDocumentsScope{}, err
	}
	if scope.Task.ID != taskID || middleware.DaemonWorkspaceIDFromContext(r.Context()) != util.UUIDToString(scope.WorkspaceID) ||
		!validateFeishuDocumentsScope(scope, installationID, revision) {
		return feishuDocumentsScope{}, errFeishuDocumentsCapabilityDenied
	}
	return scope, nil
}

func writeFeishuDocumentsAccessError(w http.ResponseWriter, err error) {
	if errors.Is(err, errFeishuDocumentsUnavailable) {
		writeErrorCode(w, http.StatusServiceUnavailable, "capability_unavailable", "document capability is temporarily unavailable")
		return
	}
	writeErrorCode(w, http.StatusForbidden, "capability_denied", "document capability denied")
}

// ResolveRemoteMCPCredential returns the same short-lived task daemon token
// that authenticated this request. The broker uses it to call the built-in MCP
// endpoint; no Feishu installation credential crosses the daemon boundary.
func (h *Handler) ResolveRemoteMCPCredential(w http.ResponseWriter, r *http.Request) {
	if _, err := h.authorizeFeishuDocumentsRequest(r); err != nil {
		writeFeishuDocumentsAccessError(w, err)
		return
	}
	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer mdt_") || strings.TrimSpace(authorization) != authorization {
		writeFeishuDocumentsAccessError(w, errFeishuDocumentsCapabilityDenied)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"credential_header": "Authorization",
		"credential":        authorization,
	})
}

type feishuDocumentsRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func strictJSONDecode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func strictJSONParams(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	return strictJSONDecode(raw, target)
}

func hasRPCID(id json.RawMessage) bool {
	return len(id) > 0 && string(id) != "null"
}

// ServeFeishuDocumentsMCP exposes only the six schema-pinned document tools to
// the daemon that currently owns this exact running task.
func (h *Handler) ServeFeishuDocumentsMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	scope, err := h.authorizeFeishuDocumentsRequest(r)
	if err != nil {
		writeFeishuDocumentsAccessError(w, err)
		return
	}
	if h.LarkDocuments == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "capability_unavailable", "document capability is temporarily unavailable")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), feishuDocumentsCallTimeout)
	defer cancel()
	r = r.WithContext(ctx)
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, lark.DocumentMaxRequestBytes))
	if err != nil {
		writeFeishuDocumentsRPCError(w, nil, -32600, "request is not valid JSON-RPC")
		return
	}
	var request feishuDocumentsRPCRequest
	if err := strictJSONDecode(raw, &request); err != nil || request.JSONRPC != "2.0" || request.Method == "" {
		writeFeishuDocumentsRPCError(w, request.ID, -32600, "request is not valid JSON-RPC")
		return
	}

	switch request.Method {
	case "initialize":
		h.serveFeishuDocumentsInitialize(w, request)
	case "notifications/initialized":
		if hasRPCID(request.ID) || strictJSONParams(request.Params, &struct{}{}) != nil {
			writeFeishuDocumentsRPCError(w, request.ID, -32602, "invalid initialization notification")
			return
		}
		w.WriteHeader(http.StatusOK)
	case "tools/list":
		if !hasRPCID(request.ID) || strictJSONParams(request.Params, &struct{}{}) != nil {
			writeFeishuDocumentsRPCError(w, request.ID, -32602, "invalid tools list request")
			return
		}
		tools, toolErr := feishuDocumentTools()
		if toolErr != nil {
			writeFeishuDocumentsRPCError(w, request.ID, -32603, "document tools are unavailable")
			return
		}
		descriptors := make([]map[string]any, 0, len(tools))
		for _, tool := range tools {
			descriptors = append(descriptors, map[string]any{
				"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema,
			})
		}
		writeFeishuDocumentsRPCResult(w, request.ID, map[string]any{"tools": descriptors})
	case "tools/call":
		h.serveFeishuDocumentsToolCall(w, r, scope, request)
	default:
		writeFeishuDocumentsRPCError(w, request.ID, -32601, "only Feishu document tools are available")
	}
}

func (h *Handler) serveFeishuDocumentsInitialize(w http.ResponseWriter, request feishuDocumentsRPCRequest) {
	if !hasRPCID(request.ID) {
		writeFeishuDocumentsRPCError(w, request.ID, -32602, "invalid initialize request")
		return
	}
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    any    `json:"capabilities"`
		ClientInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}
	if strictJSONParams(request.Params, &params) != nil || !containsFeishuDocumentsString(remotemcp.SupportedProtocolVersions(), params.ProtocolVersion) {
		writeFeishuDocumentsRPCError(w, request.ID, -32602, "unsupported MCP protocol version")
		return
	}
	writeFeishuDocumentsRPCResult(w, request.ID, map[string]any{
		"protocolVersion": params.ProtocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]string{"name": "feishu-documents", "version": "1"},
	})
}

func containsFeishuDocumentsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (h *Handler) serveFeishuDocumentsToolCall(w http.ResponseWriter, r *http.Request, scope feishuDocumentsScope, request feishuDocumentsRPCRequest) {
	if !hasRPCID(request.ID) {
		writeFeishuDocumentsRPCError(w, request.ID, -32602, "invalid tool call parameters")
		return
	}
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if strictJSONParams(request.Params, &params) != nil {
		writeFeishuDocumentsRPCError(w, request.ID, -32602, "invalid tool call parameters")
		return
	}
	operation := lark.DocumentOperation(params.Name)
	input, err := strictFeishuDocumentToolInput(operation, params.Arguments)
	if err != nil {
		writeFeishuDocumentsRPCError(w, request.ID, -32602, "invalid document tool arguments")
		return
	}
	validated, validationErr := lark.ValidateDocumentToolInput(operation, input)
	if validationErr != nil {
		writeFeishuDocumentsToolResult(w, request.ID, operation, lark.DocumentToolResult{}, validationErr)
		return
	}
	result, toolErr := h.LarkDocuments.Execute(r.Context(), scope.documentTaskScope(), operation, validated.Input)
	writeFeishuDocumentsToolResult(w, request.ID, operation, result, toolErr)
}

func strictFeishuDocumentToolInput(operation lark.DocumentOperation, raw json.RawMessage) (lark.DocumentToolInput, error) {
	allowed := map[lark.DocumentOperation]map[string]bool{
		lark.DocumentOperationFetch:        {"document_url": true, "scope": true, "keyword": true, "start_block_id": true, "end_block_id": true},
		lark.DocumentOperationAppend:       {"document_url": true, "content": true, "confirmed": true},
		lark.DocumentOperationReplaceText:  {"document_url": true, "pattern": true, "content": true, "confirmed": true},
		lark.DocumentOperationInsertAfter:  {"document_url": true, "block_id": true, "content": true, "confirmed": true},
		lark.DocumentOperationReplaceBlock: {"document_url": true, "block_id": true, "start_block_id": true, "end_block_id": true, "content": true, "confirmed": true},
		lark.DocumentOperationDeleteBlock:  {"document_url": true, "block_id": true, "start_block_id": true, "end_block_id": true, "confirmed": true},
	}[operation]
	if allowed == nil || len(raw) == 0 {
		return lark.DocumentToolInput{}, errors.New("unsupported document tool")
	}
	var fields map[string]json.RawMessage
	if err := strictJSONDecode(raw, &fields); err != nil || fields == nil {
		return lark.DocumentToolInput{}, errors.New("invalid document tool arguments")
	}
	for field := range fields {
		if !allowed[field] {
			return lark.DocumentToolInput{}, errors.New("document tool field is not allowed")
		}
	}
	var input lark.DocumentToolInput
	if err := strictJSONDecode(raw, &input); err != nil {
		return lark.DocumentToolInput{}, err
	}
	return input, nil
}

func writeFeishuDocumentsToolResult(w http.ResponseWriter, id json.RawMessage, operation lark.DocumentOperation, result lark.DocumentToolResult, toolErr *lark.DocumentToolError) {
	payload := map[string]any{"ok": toolErr == nil, "operation": operation}
	rpcResult := map[string]any{}
	if toolErr != nil {
		payload["error"] = toolErr
		rpcResult["isError"] = true
	} else {
		payload["result"] = result
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		writeFeishuDocumentsRPCError(w, id, -32603, "document result is unavailable")
		return
	}
	rpcResult["content"] = []map[string]string{{"type": "text", "text": string(encoded)}}
	writeFeishuDocumentsRPCResult(w, id, rpcResult)
}

func writeFeishuDocumentsRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeFeishuDocumentsRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message},
	})
}
