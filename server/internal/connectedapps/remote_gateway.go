package connectedapps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
)

// ServeRemoteApp is the MCP server a run sees for one remote app, under the
// app's own name. Tools keep the names and schemas the app's server gives
// them; every call still goes through the account, rules, rate limit and
// activity log of the meta gateway before Multica forwards it with the
// account's token.
func (s *Service) ServeRemoteApp(w http.ResponseWriter, r *http.Request, slug string) {
	app, ok := s.RemoteApp(slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.serveRPC(w, r, func(ctx context.Context, rn *run, req rpcRequest) (rpcResponse, bool) {
		return s.dispatchRemote(ctx, rn, app, req)
	})
}

func (s *Service) dispatchRemote(ctx context.Context, rn *run, app RemoteApp, req rpcRequest) (rpcResponse, bool) {
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
			"serverInfo":      map[string]any{"name": app.Slug, "version": "1"},
			"instructions": fmt.Sprintf("%s tools, run through Multica with the account and permissions set there. "+
				"If a call says the account is not connected, call %s on the %s server and end your turn.",
				app.Name, toolRequestConnection, GatewayServerName),
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": s.remoteToolList(ctx, rn, app)}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "invalid params"}
			break
		}
		resp.Result = s.remoteRun(ctx, rn, app, p.Name, p.Arguments)
	default:
		if strings.HasPrefix(req.Method, "notifications/") {
			return resp, true
		}
		resp.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	return resp, notification
}

// remoteToolList is the app's tools this run may call: none when the agent
// lacks the app or the account is not connected, and never a tool whose
// class the rules block.
func (s *Service) remoteToolList(ctx context.Context, rn *run, app RemoteApp) []json.RawMessage {
	out := []json.RawMessage{}
	agentApp, ok := rn.apps[app.Slug]
	if !ok {
		return out
	}
	acct, err := s.resolveAccount(ctx, rn.workspaceID, rn.triggeredBy, app.Slug, agentApp.AccountScope)
	if err != nil {
		return out
	}
	tools, err := s.remoteTools(ctx, app, acct)
	if err != nil {
		if !errors.Is(err, ErrAccountMissing) {
			slog.WarnContext(ctx, "connectedapps: list remote tools failed", "app", app.Slug, "task_id", util.UUIDToString(rn.task.ID))
		}
		return out
	}
	policies := map[string]string{}
	for _, tool := range tools {
		class := s.classifyRemote(ctx, rn.workspaceID, app.Slug, tool)
		policy, ok := policies[class]
		if !ok {
			if policy, _, err = s.policyFor(ctx, rn, agentApp, class); err != nil {
				policy = PolicyBlock
			}
			policies[class] = policy
		}
		if policy != PolicyBlock {
			out = append(out, tool.Raw)
		}
	}
	return out
}

func (s *Service) remoteRun(ctx context.Context, rn *run, app RemoteApp, name string, args map[string]any) any {
	agentApp, ok := rn.apps[app.Slug]
	if !ok {
		return textResult(fmt.Sprintf("%s is not available to you in this run", app.Name), true)
	}
	notConnected := textResult(fmt.Sprintf("the %s account for %s is not connected. Call %s on the %s server to ask for it, then end your turn.",
		agentApp.AccountScope, app.Slug, toolRequestConnection, GatewayServerName), true)
	acct, err := s.resolveAccount(ctx, rn.workspaceID, rn.triggeredBy, app.Slug, agentApp.AccountScope)
	if errors.Is(err, ErrAccountMissing) {
		return notConnected
	}
	if err != nil {
		return textResult("could not resolve the account for this app", true)
	}
	tools, err := s.remoteTools(ctx, app, acct)
	if errors.Is(err, ErrAccountMissing) {
		return notConnected
	}
	if err != nil {
		return textResult(fmt.Sprintf("could not reach %s right now; try again later", app.Name), true)
	}
	var tool *RemoteTool
	for i := range tools {
		if tools[i].Name == name {
			tool = &tools[i]
			break
		}
	}
	if tool == nil {
		return textResult(fmt.Sprintf("%s has no tool named %q", app.Name, name), true)
	}
	class := ClassifyCall(s.classifyRemote(ctx, rn.workspaceID, app.Slug, *tool), tool.Name, args)
	call := toolCall{
		workspaceID: rn.workspaceID, taskID: rn.task.ID, agentID: rn.agent.ID,
		issueID: rn.task.IssueID, chatSessionID: rn.task.ChatSessionID, triggeredBy: rn.triggeredBy,
		toolkit: app.Slug, tool: tool.Name, class: class,
	}
	policy, acct, err := s.policyFor(ctx, rn, agentApp, class)
	call.acct = acct
	if errors.Is(err, ErrAccountMissing) {
		return notConnected
	}
	if err != nil {
		return textResult("could not resolve the account for this app", true)
	}
	if refusal := s.refuseCall(ctx, rn, agentApp, &call, policy); refusal != nil {
		return refusal
	}
	var result json.RawMessage
	err = s.withRemoteToken(ctx, acct, func(token string) error {
		var err error
		result, err = s.remote.upstream.CallTool(ctx, app.Endpoint, token, tool.Name, args)
		return err
	})
	s.touch(ctx, rn.workspaceID, acct)
	if errors.Is(err, ErrAccountMissing) {
		call.outcome, call.err = "error", "sign-in expired"
		s.recordCall(ctx, call)
		return textResult(fmt.Sprintf("the %s sign-in for this account expired. Call %s on the %s server to ask for a new connection, then end your turn.",
			app.Name, toolRequestConnection, GatewayServerName), true)
	}
	if err != nil {
		call.outcome, call.err = "error", truncate(err.Error(), 500)
		logExecutionFailure(ctx, call, "request_failed")
		s.recordCall(ctx, call)
		return textResult("the app call failed: "+truncate(err.Error(), 500), true)
	}
	var outcome struct {
		IsError bool `json:"isError"`
	}
	_ = json.Unmarshal(result, &outcome)
	call.outcome = "ok"
	if outcome.IsError {
		call.outcome, call.err = "error", "the tool returned an error"
		logExecutionFailure(ctx, call, "tool_unsuccessful")
	}
	s.recordCall(ctx, call)
	return result
}

func remoteServerHint(app RemoteApp) map[string]any {
	return textResult(fmt.Sprintf("%s tools are on the MCP server %q. Call them there directly; they are not run through apps_run_tool.", app.Name, app.Slug), true)
}
