package connectedapps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/runtimeapps"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	maxGatewayBody   = 1 << 20
	maxResultBytes   = 60_000
	maxSearchResults = 40
	mcpVersion       = "2025-03-26"
)

// Gateway tool names.
const (
	toolListApps          = "apps_list"
	toolSearchTools       = "apps_search_tools"
	toolGetTool           = "apps_get_tool"
	toolRunTool           = "apps_run_tool"
	toolRequestConnection = "apps_request_connection"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// run is everything the gateway knows about the calling run.
type run struct {
	task        db.AgentTaskQueue
	agent       db.Agent
	workspaceID pgtype.UUID
	triggeredBy pgtype.UUID
	catalog     Catalog
	apps        map[string]db.AgentApp
}

// ServeHTTP is the MCP gateway (streamable HTTP, JSON responses only). A run
// authenticates with the bearer token minted at claim; the token only works
// while its task is dispatched or running.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	taskID, err := VerifyTaskToken(s.secret, token, s.now())
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rn, err := s.loadRun(r.Context(), taskID)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxGatewayBody+1))
	if err != nil || len(body) > maxGatewayBody {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}

	trimmed := strings.TrimSpace(string(body))
	var reqs []rpcRequest
	batch := strings.HasPrefix(trimmed, "[")
	if batch {
		err = json.Unmarshal(body, &reqs)
	} else {
		var one rpcRequest
		err = json.Unmarshal(body, &one)
		reqs = []rpcRequest{one}
	}
	if err != nil {
		writeJSON(w, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "parse error"}})
		return
	}
	var out []rpcResponse
	for _, req := range reqs {
		resp, notify := s.dispatch(r.Context(), rn, req)
		if !notify {
			out = append(out, resp)
		}
	}
	if len(out) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if batch {
		writeJSON(w, out)
		return
	}
	writeJSON(w, out[0])
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Service) loadRun(ctx context.Context, taskID string) (*run, error) {
	id, err := util.ParseUUID(taskID)
	if err != nil {
		return nil, err
	}
	task, err := s.q.GetAgentTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if task.Status != "dispatched" && task.Status != "running" {
		return nil, fmt.Errorf("task %s is %s", taskID, task.Status)
	}
	agent, err := s.q.GetAgent(ctx, task.AgentID)
	if err != nil {
		return nil, err
	}
	// A task can remain running on another member's runtime after its
	// initiator loses access. Recheck on every gateway request, not at enqueue.
	if !s.mayRun(ctx, task, agent) {
		return nil, ErrNoLongerAllowed
	}
	apps, err := s.q.ListAgentApps(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	rn := &run{
		task:        task,
		agent:       agent,
		workspaceID: agent.WorkspaceID,
		triggeredBy: task.OriginatorUserID,
		catalog:     s.Catalog(ctx, agent.WorkspaceID),
		apps:        map[string]db.AgentApp{},
	}
	for _, app := range apps {
		if rn.catalog.Allows(app.ToolkitSlug) {
			rn.apps[NormalizeSlug(app.ToolkitSlug)] = app
		}
	}
	return rn, nil
}

func (s *Service) mayRun(ctx context.Context, task db.AgentTaskQueue, agent db.Agent) bool {
	if agent.ArchivedAt.Valid || s.resumer == nil {
		return false
	}
	if task.OriginatorUserID.Valid {
		return s.mayUseAgent(ctx, agent.WorkspaceID, agent.ID, task.OriginatorUserID)
	}
	// Without a human originator, use the system invocation grant. The
	// accountable member is audit metadata and must never lend authority.
	return s.resumer.CanActFor(ctx, agent.WorkspaceID, agent.ID, pgtype.UUID{})
}

func (s *Service) dispatch(ctx context.Context, rn *run, req rpcRequest) (rpcResponse, bool) {
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	notification := len(req.ID) == 0 || string(req.ID) == "null"
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := p.ProtocolVersion
		if version == "" {
			version = mcpVersion
		}
		resp.Result = map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "multica-apps", "version": "1"},
			"instructions":    gatewayInstructions,
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": gatewayTools()}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "invalid params"}
			break
		}
		resp.Result = s.callTool(ctx, rn, p.Name, p.Arguments)
	default:
		if strings.HasPrefix(req.Method, "notifications/") {
			return resp, true
		}
		resp.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	return resp, notification
}

const gatewayInstructions = "Multica apps. Use apps_list to see your apps, apps_search_tools to find a tool, apps_get_tool for its input schema, then apps_run_tool. " +
	"Allowed calls run immediately and return their result. Never work around a blocked call. " +
	"If an account is not connected, use apps_request_connection and end your turn; you will be resumed after it is connected."

func gatewayTools() []map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	return []map[string]any{
		{
			"name":        toolListApps,
			"description": "List the external apps you can use in this run, whose account each acts as, your permission level, and whether the account is connected.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        toolSearchTools,
			"description": "Search the tools of one app. Returns tool slugs, what they do, and whether a call is allowed or blocked.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"app":   str("App slug from apps_list, e.g. gmail"),
					"query": str("Words describing what you want to do, e.g. 'send email'. Optional."),
				},
				"required": []string{"app"},
			},
		},
		{
			"name":        toolGetTool,
			"description": "Get one tool's input schema. Call before apps_run_tool.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"tool": str("Tool slug, e.g. GMAIL_SEND_EMAIL")},
				"required":   []string{"tool"},
			},
		},
		{
			"name":        toolRunTool,
			"description": "Run an app tool immediately and return its result. Blocked calls return an error; do not try to work around them.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tool":      str("Tool slug, e.g. GMAIL_SEND_EMAIL"),
					"arguments": map[string]any{"type": "object", "description": "Arguments matching the tool's input schema"},
					"summary":   str("Optional summary of what this call does and why."),
				},
				"required": []string{"tool", "arguments"},
			},
		},
		{
			"name":        toolRequestConnection,
			"description": "Ask the person who triggered this run to connect their account for an app that shows not_connected. End your turn after calling it.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"app":     str("App slug from apps_list"),
					"summary": str("One sentence: why you need this app."),
				},
				"required": []string{"app"},
			},
		},
	}
}

func textResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

func jsonResult(v any) map[string]any {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return textResult("failed to encode result", true)
	}
	text := string(raw)
	if len(text) > maxResultBytes {
		text = text[:maxResultBytes] + "\n…(truncated)"
	}
	return textResult(text, false)
}

func argString(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func (s *Service) callTool(ctx context.Context, rn *run, name string, args map[string]any) map[string]any {
	switch name {
	case toolListApps:
		return s.toolListApps(ctx, rn)
	case toolSearchTools:
		return s.toolSearch(ctx, rn, argString(args, "app"), argString(args, "query"))
	case toolGetTool:
		return s.toolGet(ctx, rn, argString(args, "tool"))
	case toolRunTool:
		toolArgs, _ := args["arguments"].(map[string]any)
		if toolArgs == nil {
			toolArgs = map[string]any{}
		}
		return s.toolRun(ctx, rn, argString(args, "tool"), toolArgs, argString(args, "summary"))
	case toolRequestConnection:
		return s.toolRequestConnection(ctx, rn, argString(args, "app"), argString(args, "summary"))
	}
	return textResult("unknown tool "+name, true)
}

func (s *Service) toolListApps(ctx context.Context, rn *run) map[string]any {
	type appView struct {
		App     string `json:"app"`
		Name    string `json:"name"`
		Account string `json:"account"`
		Level   string `json:"level"`
		Status  string `json:"status"`
	}
	out := []appView{}
	for slug, app := range rn.apps {
		level := app.Level
		if level == LevelConfirmWrites {
			level = LevelWrite
		}
		status := "connected"
		if _, err := s.resolveAccount(ctx, rn.workspaceID, rn.triggeredBy, slug, app.AccountScope); err != nil {
			status = "not_connected"
		}
		out = append(out, appView{
			App: slug, Name: runtimeapps.DisplayNameForToolkitSlug(slug),
			Account: app.AccountScope, Level: level, Status: status,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].App < out[j].App })
	return jsonResult(map[string]any{"apps": out})
}

// appForTool finds the agent app a tool slug belongs to by its prefix.
func (rn *run) appForTool(tool string) (db.AgentApp, bool) {
	upper := strings.ToUpper(tool)
	best := ""
	for slug := range rn.apps {
		prefix := strings.ToUpper(slug) + "_"
		if strings.HasPrefix(upper, prefix) && len(slug) > len(best) {
			best = slug
		}
	}
	app, ok := rn.apps[best]
	return app, ok && best != ""
}

// policyFor returns the policy a call of class would get right now.
func (s *Service) policyFor(ctx context.Context, rn *run, app db.AgentApp, class string) (string, account, error) {
	slug := NormalizeSlug(app.ToolkitSlug)
	acct, err := s.resolveAccount(ctx, rn.workspaceID, rn.triggeredBy, slug, app.AccountScope)
	if err != nil {
		return "", account{scope: app.AccountScope}, err
	}
	rule, err := s.accountRule(ctx, rn.workspaceID, slug, acct, class)
	if err != nil {
		return "", acct, err
	}
	return Decide(slug, app.Level, class, rule), acct, nil
}

func (s *Service) toolSearch(ctx context.Context, rn *run, slug, query string) map[string]any {
	slug = NormalizeSlug(slug)
	app, ok := rn.apps[slug]
	if !ok {
		return textResult(fmt.Sprintf("app %q is not available to you in this run; call apps_list", slug), true)
	}
	tools, err := s.composio.ListToolkitTools(ctx, slug)
	if err != nil {
		slog.Warn("connectedapps: list tools failed", "toolkit", slug, "error", err)
		return textResult("could not load this app's tools right now; try again later", true)
	}
	terms := strings.Fields(strings.ToLower(query))
	type hit struct {
		Tool        string `json:"tool"`
		Description string `json:"description"`
		Class       string `json:"class"`
		Policy      string `json:"policy"`
		score       int
	}
	overrides := s.ClassOverrides(ctx, rn.workspaceID, slug)
	policies := map[string]string{}
	var hits []hit
	for _, t := range tools {
		hay := strings.ToLower(t.Slug + " " + t.Name + " " + t.Description)
		score := 0
		for _, term := range terms {
			if strings.Contains(hay, term) {
				score++
			}
		}
		if len(terms) > 0 && score == 0 {
			continue
		}
		class, ok := overrides[strings.ToUpper(t.Slug)]
		if !ok {
			class = Classify(slug, t.Slug, t.Tags)
		}
		policy, ok := policies[class]
		if !ok {
			policy, _, err = s.policyFor(ctx, rn, app, class)
			if err != nil {
				policy = "account_not_connected"
			}
			policies[class] = policy
		}
		desc := t.Description
		if len(desc) > 220 {
			desc = desc[:220] + "…"
		}
		hits = append(hits, hit{Tool: t.Slug, Description: desc, Class: class, Policy: policy, score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > maxSearchResults {
		hits = hits[:maxSearchResults]
	}
	return jsonResult(map[string]any{"app": slug, "tools": hits})
}

func (s *Service) toolGet(ctx context.Context, rn *run, toolSlug string) map[string]any {
	app, ok := rn.appForTool(toolSlug)
	if !ok {
		return textResult("this tool does not belong to an app you can use in this run", true)
	}
	tool, err := s.composio.GetTool(ctx, app.ToolkitSlug, toolSlug)
	if err != nil {
		return textResult("tool not found", true)
	}
	class := s.classify(ctx, rn.workspaceID, app.ToolkitSlug, tool)
	policy, _, err := s.policyFor(ctx, rn, app, class)
	if err != nil {
		policy = "account_not_connected"
	}
	return jsonResult(map[string]any{
		"tool":         tool.Slug,
		"description":  tool.Description,
		"class":        class,
		"policy":       policy,
		"input_schema": tool.InputParameters,
	})
}

func (s *Service) toolRun(ctx context.Context, rn *run, toolSlug string, args map[string]any, summary string) map[string]any {
	app, ok := rn.appForTool(toolSlug)
	if !ok {
		return textResult("this tool does not belong to an app you can use in this run", true)
	}
	slug := NormalizeSlug(app.ToolkitSlug)
	tool, err := s.composio.GetTool(ctx, slug, toolSlug)
	if err != nil {
		return textResult("tool not found", true)
	}
	class := ClassifyCall(s.classify(ctx, rn.workspaceID, slug, tool), tool.Slug, args)
	call := toolCall{
		workspaceID: rn.workspaceID, taskID: rn.task.ID, agentID: rn.agent.ID,
		issueID: rn.task.IssueID, chatSessionID: rn.task.ChatSessionID, triggeredBy: rn.triggeredBy,
		toolkit: slug, tool: tool.Slug, class: class,
	}
	policy, acct, err := s.policyFor(ctx, rn, app, class)
	call.acct = acct
	if err != nil {
		if err == ErrAccountMissing {
			return textResult(fmt.Sprintf("the %s account for %s is not connected. Call %s to ask for it, then end your turn.", app.AccountScope, slug, toolRequestConnection), true)
		}
		return textResult("could not resolve the account for this app", true)
	}
	switch policy {
	case PolicyBlock:
		call.outcome = "blocked"
		first := s.firstBlock(ctx, call)
		s.recordCall(ctx, call)
		if first && rn.triggeredBy.Valid {
			s.notify(ctx, db.AppActionRequest{
				WorkspaceID: rn.workspaceID, Status: "blocked", Kind: KindAction,
				TaskID: rn.task.ID, AgentID: rn.agent.ID, IssueID: rn.task.IssueID, ChatSessionID: rn.task.ChatSessionID,
				ApproverUserID: rn.triggeredBy, AccountScope: app.AccountScope,
				ToolkitSlug: slug, ToolSlug: tool.Slug, ActionClass: class,
			}, NotifActionResult)
		}
		return textResult(fmt.Sprintf("blocked: %s calls (%s) are not allowed for you on %s. Do not work around this; tell the person what you would have done.", class, tool.Slug, slug), true)
	}
	// Tool lookup may involve a network call, and one gateway request may
	// contain a batch. Check again immediately before allowing execution.
	if !s.mayRun(ctx, rn.task, rn.agent) {
		return textResult("this run is no longer allowed to use this agent", true)
	}
	if s.overLimit(ctx, rn.workspaceID, slug, acct) {
		call.outcome = "rate_limited"
		s.recordCall(ctx, call)
		return textResult(fmt.Sprintf("rate limited: this %s account already ran %d calls in the last minute. Wait a minute before the next call, and batch work where the app allows it.", slug, s.perMinute), true)
	}
	resp, err := s.composio.ExecuteTool(ctx, acct.connectedAccountID, acct.composioUserID, *tool, args)
	s.touch(ctx, rn.workspaceID, acct)
	if err != nil {
		call.outcome, call.err = "error", truncate(err.Error(), 500)
		logExecutionFailure(ctx, call, "request_failed")
		s.recordCall(ctx, call)
		return textResult("the app call failed: "+truncate(err.Error(), 500), true)
	}
	if !resp.Successful {
		call.outcome, call.err = "error", truncate(resp.Error, 500)
		logExecutionFailure(ctx, call, "tool_unsuccessful")
		s.recordCall(ctx, call)
		return textResult("the app returned an error: "+truncate(resp.Error, 2000), true)
	}
	call.outcome = "ok"
	s.recordCall(ctx, call)
	return jsonResult(resp.Data)
}

func (s *Service) toolRequestConnection(ctx context.Context, rn *run, slug, summary string) map[string]any {
	slug = NormalizeSlug(slug)
	app, ok := rn.apps[slug]
	if !ok {
		return textResult(fmt.Sprintf("app %q is not available to you in this run", slug), true)
	}
	if _, err := s.resolveAccount(ctx, rn.workspaceID, rn.triggeredBy, slug, app.AccountScope); err == nil {
		return textResult("the account is already connected; use the app directly", false)
	}
	if app.AccountScope == ScopeTeam {
		return textResult("this app uses the team account, which a workspace admin connects in Settings › Apps. Tell the person in your reply and end your turn.", true)
	}
	if !rn.triggeredBy.Valid {
		return textResult("no person triggered this run, so nobody can be asked to connect an account", true)
	}
	req, err := s.createRequest(ctx, rn, "connect", app, "", "", map[string]any{}, summary)
	if err != nil {
		return textResult("could not send the connection request", true)
	}
	return textResult(fmt.Sprintf("requested: %s is asked to connect %s (request %s). End your turn now; you will be resumed after they connect.", "the person who triggered this run", slug, util.UUIDToString(req.ID)), false)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
