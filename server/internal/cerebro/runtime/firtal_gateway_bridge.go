package runtime

// firtal_gateway_bridge.go — FIR-1449 spike.
//
// Goal of the spike: prove that the *entire* Multica CLI/MCP tool surface can be
// hooked into the in-app runtime (Kristian/Saga/Knud) at ONE place — a generic
// bridge — instead of hand-porting ~60 DB-direct tools one at a time.
//
// The mechanism has three pieces, all in this file:
//
//  1. loopbackTransport — an http.RoundTripper that dispatches a request against
//     the server's own router IN-PROCESS (no network hop). This is what makes
//     "reuse the CLI handlers" cheap: the CLI MCP tools already speak to the REST
//     API through a *cli.APIClient; we just give that client a transport that
//     loops straight back into the same process.
//
//  2. mcpBridgeTool — an adapter that makes one *mcp.Server tool satisfy the
//     gateway's in-process Tool interface (Name/Description/InputSchema/Call).
//     The gateway Registry then treats a bridged CLI tool exactly like a native
//     DB-direct builtin.
//
//  3. RegisterBridgedMCPTools — enumerate every tool on a populated *mcp.Server,
//     drop the admin denylist, and register the rest into the gateway Registry.
//
// Identity & auth: the calling agent's identity travels in the standard auth
// headers on the APIClient (Bearer token, X-Agent-ID, X-Workspace-ID, X-Task-ID).
// The in-process request still flows through the server's normal middleware, so
// authorization is unchanged — the bridge swaps the TRANSPORT, never the auth.
//
// Admin off at runtime level: the authoritative lever is the existing
// cerebro_runtime_tool cascade (a row with enabled=false hides a tool for a
// runtime, no code). DefaultInAppAdminDenylist is a static belt to that
// suspenders — even if a future upstream tool is added and someone forgets to
// seed the cascade, the bridge will not surface an access-control MUTATION tool
// to an in-app answer-assistant.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// inProcessBaseURL is the sentinel BaseURL for an in-process APIClient. It is
// never dialed — loopbackTransport intercepts every request before the network
// — but cli.APIClient needs a non-empty, well-formed base so request building
// (and the relative-download guard) behaves normally.
const inProcessBaseURL = "http://in-process.cerebro.local"

// loopbackTransport is an http.RoundTripper that serves each request against an
// in-process http.Handler (the server's router) instead of opening a socket.
// It is the transport half of architecture choice C (see the issue plan): the
// CLI MCP tool handlers keep calling client.GetJSON/PostJSON unchanged; those
// calls land back in this same process with zero network latency.
type loopbackTransport struct {
	handler http.Handler
}

func (t loopbackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.handler == nil {
		return nil, fmt.Errorf("loopbackTransport: nil handler")
	}
	// Drain/close the request body per RoundTripper contract.
	if req.Body != nil {
		defer req.Body.Close()
	}
	rec := httptest.NewRecorder()
	t.handler.ServeHTTP(rec, req)
	resp := rec.Result()
	resp.Request = req
	return resp, nil
}

// InProcessAPIClient builds a cli.APIClient that round-trips in-process against
// the given handler (the server's own router) carrying the calling agent's
// identity. The agentID/token/taskID are the same values a remote CLI would
// send; the server authorizes the request identically. workspaceID scopes every
// call to the agent's workspace.
//
// This is the single seam the in-app runtime needs to gain the full CLI tool
// surface: build one of these per task with the task's identity, register the
// CLI MCP tools against it, and bridge them in.
func InProcessAPIClient(handler http.Handler, workspaceID, agentID, token, taskID string) *cli.APIClient {
	c := cli.NewAPIClient(inProcessBaseURL, workspaceID, token)
	c.AgentID = agentID
	c.TaskID = taskID
	c.HTTPClient = &http.Client{Transport: loopbackTransport{handler: handler}}
	return c
}

// mcpBridgeTool adapts a single tool registered on an *mcp.Server so it
// satisfies the gateway's Tool interface. One adapter instance per tool; the
// shared *mcp.Server owns the actual handler and dispatches by name.
type mcpBridgeTool struct {
	srv *mcp.Server
	def mcp.Tool
}

func (t *mcpBridgeTool) Name() string        { return t.def.Name }
func (t *mcpBridgeTool) Description() string { return t.def.Description }
func (t *mcpBridgeTool) InputSchema() map[string]any {
	if t.def.InputSchema != nil {
		return t.def.InputSchema
	}
	// Defensive default: the gateway's Anthropic schema normalizer expects an
	// object schema. A tool that declared no schema still gets a valid one.
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (t *mcpBridgeTool) Call(ctx context.Context, args map[string]any) (string, error) {
	res, err := t.srv.Call(ctx, t.def.Name, args)
	if err != nil {
		return "", err
	}
	text := mcpResultText(res)
	if res.IsError {
		if text == "" {
			text = "tool reported an error"
		}
		return "", fmt.Errorf("%s", text)
	}
	return text, nil
}

// mcpResultText flattens an mcp.CallToolResult into the single string the
// gateway tool-loop expects. Text content blocks are concatenated; a
// structuredContent payload (CEREBRO-PATCH mcp-structured-content) is appended
// as raw JSON so nothing the CLI tool returned is lost.
func mcpResultText(res mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if c.Text != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(c.Text)
		}
	}
	if len(res.StructuredContent) > 0 {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.Write(res.StructuredContent)
	}
	return b.String()
}

// DefaultInAppAdminDenylist is the set of MCP tool names the in-app runtime
// must never expose, regardless of what the bridged server registers. These are
// the access-control / permission MUTATION tools: groups, grants, credential
// policy, skill governance writes, and code-session control. Read access is
// intentionally NOT denied here — the product decision (Jesper, FIR-1449) is
// "read everything, do the normal work, but no write access to who-can-do-what".
//
// This is a STATIC safety net. The per-runtime, authoritative lever is the
// cerebro_runtime_tool cascade seed (enabled=false), which another runtime can
// flip back on later with no code change. The denylist exists so a forgotten
// seed can never silently leak an admin-write tool to an answer assistant.
func DefaultInAppAdminDenylist() map[string]bool {
	names := []string{
		// Groups (write)
		"create_group", "update_group", "delete_group",
		"add_group_member", "remove_group_member",
		"add_group_agent", "remove_group_agent",
		"add_group_runtime", "remove_group_runtime",
		"set_group_capability", "remove_group_capability",
		"add_project_group", "remove_project_group",
		// Persona grants (write)
		"create_grant", "update_grant", "delete_grant", "evaluate_policy",
		// Credential policy (write)
		"credential_policy_set", "credential_policy_get",
		// Skill governance (write)
		"skill_propose_change", "skill_review_change_request", "skill_fork",
		"skill_record_observation", "skill_audit", "skill_impact_analysis",
		// Code sessions (irrelevant for an in-app chat assistant)
		"attach_session", "fork_session", "resume_session",
		"report_activity", "complete_work", "bind_repo",
	}
	deny := make(map[string]bool, len(names))
	for _, n := range names {
		deny[n] = true
	}
	return deny
}

// BridgeMCPTools enumerates every tool registered on srv, skips any whose name
// is in deny, and returns gateway Tool adapters for the rest. deny may be nil.
func BridgeMCPTools(srv *mcp.Server, deny map[string]bool) []Tool {
	if srv == nil {
		return nil
	}
	defs := srv.Tools()
	out := make([]Tool, 0, len(defs))
	for _, def := range defs {
		if deny[def.Name] {
			continue
		}
		out = append(out, &mcpBridgeTool{srv: srv, def: def})
	}
	return out
}

// RegisterBridgedMCPTools bridges every non-denied tool on srv into the gateway
// Registry and returns how many were registered.
//
// When onlyNew is true, a bridged tool is registered only if the Registry does
// not already have a tool of that name — this preserves the existing native
// DB-direct builtins (get_issue, list_issues, …) and lets the bridge fill ONLY
// the gaps (search_issues, skills, artifact CRUD, wakeups). When onlyNew is
// false the bridged tools win, which is the eventual single-layer end-state of
// architecture choice C.
func RegisterBridgedMCPTools(r *Registry, srv *mcp.Server, deny map[string]bool, onlyNew bool) int {
	if r == nil || srv == nil {
		return 0
	}
	n := 0
	for _, t := range BridgeMCPTools(srv, deny) {
		if onlyNew && r.hasTool(t.Name()) {
			continue
		}
		r.Register(t)
		n++
	}
	return n
}
