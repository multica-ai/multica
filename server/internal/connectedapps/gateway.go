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
	s.serveRPC(w, r, s.dispatch)
}

func (s *Service) serveRPC(w http.ResponseWriter, r *http.Request, dispatch func(context.Context, *run, rpcRequest) (rpcResponse, bool)) {
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
		resp, notify := dispatch(r.Context(), rn, req)
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
	"If an account is not connected, use apps_request_connection and end your turn; you will be resumed after it is connected. " +
	"If you need an app that apps_list does not show, call apps_request_connection with its slug (e.g. googlecalendar) and end your turn: " +
	"the person who started the run may add it to you and connect their account."

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
			"description": "Ask for an app account that shows not_connected: your own account goes to the person who triggered this run, a team account to the workspace admins. Also adds an app you do not have yet. End your turn after calling it.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"app":     str("App slug from apps_list, or the slug of an app you do not have yet, e.g. googlecalendar"),
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
		// Server is set for an app whose tools are called directly on its
		// own MCP server rather than through apps_run_tool.
		Server string `json:"server,omitempty"`
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
		view := appView{
			App: slug, Name: runtimeapps.DisplayNameForToolkitSlug(slug),
			Account: app.AccountScope, Level: level, Status: status,
		}
		if remote, ok := s.RemoteApp(slug); ok {
			view.Name, view.Server = remote.Name, remote.Slug
		}
		out = append(out, view)
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
	if remote, ok := s.RemoteApp(slug); ok {
		return remoteServerHint(remote)
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
	if remote, ok := s.RemoteApp(app.ToolkitSlug); ok {
		return remoteServerHint(remote)
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
	if remote, ok := s.RemoteApp(slug); ok {
		return remoteServerHint(remote)
	}
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
	if refusal := s.refuseCall(ctx, rn, app, &call, policy); refusal != nil {
		return refusal
	}
	// Last: it counts the call, which runs as soon as this returns nil.
	if exceeded := s.overQuota(ctx, &call); exceeded != nil {
		return textResult(exceeded.Message(), true)
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

// refuseCall applies what stands between an allowed policy and executing a
// call: a blocking rule, the run losing access and the account's rate limit.
// Composio calls then check the workspace's monthly app call allowance.
// It records and returns the refusal, or nil when the call may run.
func (s *Service) refuseCall(ctx context.Context, rn *run, app db.AgentApp, call *toolCall, policy string) map[string]any {
	if policy == PolicyBlock {
		call.outcome = "blocked"
		first := s.firstBlock(ctx, *call)
		s.recordCall(ctx, *call)
		if first && rn.triggeredBy.Valid {
			s.notify(ctx, db.AppActionRequest{
				WorkspaceID: rn.workspaceID, Status: "blocked", Kind: KindAction,
				TaskID: rn.task.ID, AgentID: rn.agent.ID, IssueID: rn.task.IssueID, ChatSessionID: rn.task.ChatSessionID,
				ApproverUserID: rn.triggeredBy, AccountScope: app.AccountScope,
				ToolkitSlug: call.toolkit, ToolSlug: call.tool, ActionClass: call.class,
			}, NotifActionResult)
		}
		return textResult(fmt.Sprintf("blocked: %s calls (%s) are not allowed for you on %s. Do not work around this; tell the person what you would have done.", call.class, call.tool, call.toolkit), true)
	}
	// Tool lookup may involve a network call, and one gateway request may
	// contain a batch. Check again immediately before allowing execution.
	if !s.mayRun(ctx, rn.task, rn.agent) {
		return textResult("this run is no longer allowed to use this agent", true)
	}
	if s.overLimit(ctx, rn.workspaceID, call.toolkit, call.acct) {
		call.outcome = "rate_limited"
		s.recordCall(ctx, *call)
		return textResult(fmt.Sprintf("rate limited: this %s account already ran %d calls in the last minute. Wait a minute before the next call, and batch work where the app allows it.", call.toolkit, s.perMinute), true)
	}
	return nil
}

func (s *Service) toolRequestConnection(ctx context.Context, rn *run, slug, summary string) map[string]any {
	slug = NormalizeSlug(slug)
	app, ok := rn.apps[slug]
	if !ok {
		return s.requestNewApp(ctx, rn, slug, summary)
	}
	if _, err := s.resolveAccount(ctx, rn.workspaceID, rn.triggeredBy, slug, app.AccountScope); err == nil {
		return textResult("the account is already connected; use the app directly", false)
	}
	if !rn.triggeredBy.Valid {
		if app.AccountScope == ScopeTeam {
			return textResult("no person started this run, so the request cannot wait on it. A workspace admin connects the team account in Apps › Accounts; say so in your reply and end your turn.", true)
		}
		return textResult("no person triggered this run, so nobody can be asked to connect an account", true)
	}
	req, err := s.createRequest(ctx, rn, KindConnect, app, "", "", map[string]any{}, summary)
	if err != nil {
		return textResult("could not send the connection request", true)
	}
	who := "the person who triggered this run is"
	if app.AccountScope == ScopeTeam {
		who = "the workspace admins are"
	}
	return textResult(fmt.Sprintf("requested: %s asked to connect %s (request %s). End your turn now; you will be resumed after it is connected.", who, slug, util.UUIDToString(req.ID)), false)
}

// requestNewApp asks the person who triggered the run to add an app the agent
// does not have, through their personal account. Team accounts act as the
// whole workspace, so only an admin attaches one, in the agent's settings.
func (s *Service) requestNewApp(ctx context.Context, rn *run, slug, summary string) map[string]any {
	if slug == "" {
		return textResult("app is required", true)
	}
	if !rn.catalog.Allows(slug) {
		return textResult(fmt.Sprintf("app %q is not available in this workspace", slug), true)
	}
	if !rn.triggeredBy.Valid {
		return textResult("no person triggered this run, so nobody can be asked to add an app", true)
	}
	known, err := s.knownToolkit(ctx, slug)
	if err != nil {
		return textResult("could not look up this app right now; try again later", true)
	}
	if !known {
		return textResult(fmt.Sprintf("there is no app with the slug %q", slug), true)
	}
	if s.resumer == nil || !s.resumer.CanManageAgent(ctx, rn.workspaceID, rn.agent.ID, rn.triggeredBy) {
		return textResult(fmt.Sprintf("%s is not on your app list, and the person who started this run cannot add apps to you. "+
			"Do not retry; say so in your reply and end your turn.", appLabel(slug)), true)
	}
	app := db.AgentApp{ToolkitSlug: slug, AccountScope: ScopePersonal}
	req, err := s.createRequest(ctx, rn, KindConnect, app, "", "", map[string]any{}, summary)
	if err != nil {
		return textResult("could not send the request", true)
	}
	return textResult(fmt.Sprintf("requested: the person who triggered this run is asked to add %s to you and connect their account (request %s). "+
		"End your turn now; you will be resumed after they do.", slug, util.UUIDToString(req.ID)), false)
}

// knownToolkit reports whether slug names a real app: a curated one, or one
// the provider lists tools for.
func (s *Service) knownToolkit(ctx context.Context, slug string) (bool, error) {
	if _, ok := Curated(slug); ok {
		return true, nil
	}
	tools, err := s.composio.ListToolkitTools(ctx, slug)
	if err != nil {
		return false, err
	}
	return len(tools) > 0, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
