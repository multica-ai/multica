package runtime

// CEREBRO-PATCH(memory-tools): FIR-1794 — Cognee-backed memory tools for
// agents, wired through the three locked memory gates (29/6 spec):
//
//  1. workspace feature flag `cerebro_memory` (default OFF)
//  2. group capability `create_memory` (default deny)
//  3. per-(user × agent) read/write switches (default off, absence-of-row = off)
//
// The tools proxy to the internal cognee-memory-service over MCP JSON-RPC
// (same transport shape as customer_service_mcp_tools.go). Two properties are
// enforced HERE, in code, and cannot be talked around by the model:
//
//  - IDENTITY IS STAMPED SERVER-SIDE. The input schemas expose no subject_id;
//    Call() derives the memory subject from the task's ToolContext (agent) and
//    the originating user. An agent can never read or write another subject's
//    memory by passing a different id — the argument does not exist.
//  - GATES ARE RE-CHECKED AT CALL TIME. Offering (which tools an agent gets)
//    and dispatch (each call) both resolve the same three gates, fail closed.
//    Granting these tools through any other path (agent_tool_grant, a raw
//    connection) still cannot bypass the switches: the call itself denies.
//
// Scope model (locked spec): private memory is keyed per (user × agent) —
// "knyttet til dig og den agent du tænder det på" — company memory is one
// workspace-shared store, readable by every agent once the workspace flag is
// on, writable only with the write switch.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/mcp"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	defaultCogneeMemoryServiceURL = "http://cognee-memory-service.internal:8080/mcp"
	cogneeMemoryTimeout           = 45 * time.Second
	cerebroMemoryFlagKey          = "cerebro_memory"
	memoryCapability              = "create_memory"

	memoryScopePrivate = "private"
	memoryScopeCompany = "company"
	memoryScopeAll     = "all"
)

type cogneeMemoryConfig struct {
	URL         string
	BearerToken string
}

func loadCogneeMemoryConfig() cogneeMemoryConfig {
	url := strings.TrimSpace(os.Getenv("COGNEE_MEMORY_SERVICE_URL"))
	if url == "" {
		url = defaultCogneeMemoryServiceURL
	}
	return cogneeMemoryConfig{
		URL:         strings.TrimRight(url, "/"),
		BearerToken: strings.TrimSpace(os.Getenv("COGNEE_MEMORY_SERVICE_AUTH_TOKEN")),
	}
}

// memoryGateQuerier is the slice of *cerebrodb.Queries the gate resolution
// needs, kept as an interface so gates are unit-testable without a database.
type memoryGateQuerier interface {
	ListCerebroWorkspaceFeatureFlags(ctx context.Context, workspaceID pgtype.UUID) ([]cerebrodb.ListCerebroWorkspaceFeatureFlagsRow, error)
	HasCerebroCapability(ctx context.Context, arg cerebrodb.HasCerebroCapabilityParams) (bool, error)
	GetUserAgentMemorySettings(ctx context.Context, arg cerebrodb.GetUserAgentMemorySettingsParams) (cerebrodb.GetUserAgentMemorySettingsRow, error)
}

// memoryGates is the resolved verdict of the three memory gates for one
// (workspace, agent, user) triple. Zero value = everything denied.
type memoryGates struct {
	// FlagOn: workspace flag cerebro_memory. Master switch — nothing below it
	// matters while this is false. Company-memory READ needs only this.
	FlagOn bool
	// HasCapability: originating user belongs to a group granted create_memory.
	HasCapability bool
	// CanRead / CanWrite: the per-(user × agent) switches. Both false when the
	// task has no originating user (e.g. an autopilot without a human origin) —
	// private memory always needs a human to be scoped to.
	CanRead  bool
	CanWrite bool
}

// resolveCerebroMemoryGates resolves the three gates, failing closed on every
// error path: a lookup failure, a missing row, or a missing user all read as
// "off". Mirrors the HTTP layer's semantics (agent_memory_settings_cerebro.go)
// with one deliberate tightening: workspace admins are NOT auto-granted the
// capability here — a tool call is machine-path, so it takes the explicit
// group grant like everyone else.
func resolveCerebroMemoryGates(ctx context.Context, q memoryGateQuerier, workspaceID, agentID, userID pgtype.UUID) memoryGates {
	var g memoryGates
	if q == nil || !workspaceID.Valid {
		return g
	}
	flags, err := q.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		return g
	}
	for _, row := range flags {
		if row.FlagKey == cerebroMemoryFlagKey {
			g.FlagOn = row.Enabled
			break
		}
	}
	if !g.FlagOn || !userID.Valid || !agentID.Valid {
		return g
	}
	has, err := q.HasCerebroCapability(ctx, cerebrodb.HasCerebroCapabilityParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Capability:  memoryCapability,
	})
	if err != nil || !has {
		return g
	}
	g.HasCapability = true
	s, err := q.GetUserAgentMemorySettings(ctx, cerebrodb.GetUserAgentMemorySettingsParams{
		UserID:  userID,
		AgentID: agentID,
	})
	if err != nil {
		// Absence-of-row IS the default-off state, not an error.
		if !errors.Is(err, pgx.ErrNoRows) {
			return g
		}
		return g
	}
	g.CanRead, g.CanWrite = s.CanReadMemory, s.CanWriteMemory
	return g
}

// cerebroMemoryToolBase carries the per-task identity and service config shared
// by the three tools. originUser is the task's originating human (task
// OriginalUserID / cascade user), NOT tctx.UserID (authorship), because the
// memory switches are per originating user.
type cerebroMemoryToolBase struct {
	cerebro memoryGateQuerier
	tctx    ToolContext
	origin  pgtype.UUID
	cfg     cogneeMemoryConfig
	client  *http.Client
}

func (b *cerebroMemoryToolBase) gates(ctx context.Context) memoryGates {
	return resolveCerebroMemoryGates(ctx, b.cerebro, b.tctx.WorkspaceID, b.tctx.AgentID, b.origin)
}

// privateSubjectID is the stamped subject for private memory: the
// (user × agent) pair, per the locked spec. Errors when the task has no
// originating user — private memory cannot exist without one.
func (b *cerebroMemoryToolBase) privateSubjectID() (string, error) {
	if !b.origin.Valid || !b.tctx.AgentID.Valid {
		return "", fmt.Errorf("private memory needs an originating user; this task has none")
	}
	return "user-" + util.UUIDToString(b.origin) + "-agent-" + util.UUIDToString(b.tctx.AgentID), nil
}

// companySubjectID isolates the shared company store per workspace.
func (b *cerebroMemoryToolBase) companySubjectID() string {
	return "workspace-" + util.UUIDToString(b.tctx.WorkspaceID)
}

// callService proxies one tool call to cognee-memory-service over MCP JSON-RPC.
func (b *cerebroMemoryToolBase) callService(ctx context.Context, tool string, args map[string]any) (string, error) {
	cfg := b.cfg
	if cfg.URL == "" {
		cfg = loadCogneeMemoryConfig()
	}
	if cfg.BearerToken == "" {
		return "", fmt.Errorf("memory service is missing COGNEE_MEMORY_SERVICE_AUTH_TOKEN")
	}
	client := b.client
	if client == nil {
		client = &http.Client{Timeout: cogneeMemoryTimeout}
	}
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": args,
		},
	})
	if err != nil {
		return "", fmt.Errorf("memory service marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("memory service request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("memory service call %s: %w", tool, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", fmt.Errorf("memory service read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("memory service call %s returned HTTP %d: %s", tool, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var envelope struct {
		Result mcp.CallToolResult `json:"result"`
		Error  *mcp.RPCError      `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("memory service decode response: %w", err)
	}
	if envelope.Error != nil {
		return "", fmt.Errorf("memory service call %s failed: %s", tool, envelope.Error.Message)
	}
	return callToolResultText(envelope.Result), nil
}

// ── memory_remember ───────────────────────────────────────────────────────────

type CerebroMemoryRememberTool struct{ cerebroMemoryToolBase }

func (t *CerebroMemoryRememberTool) Name() string { return "memory_remember" }
func (t *CerebroMemoryRememberTool) Description() string {
	return "Save one durable memory. PROTOCOL: at the END of a task, save the stable facts, decisions, and preferences worth keeping — never transient numbers (fetch those live) and never raw transcripts. One claim per memory, distilled, with its source. Default scope is your private memory (you + the person you act for); pass scope='company' for knowledge every agent in the workspace should share. Whose memory it is is stamped server-side from your task identity — you cannot write anyone else's."
}
func (t *CerebroMemoryRememberTool) InputSchema() map[string]any {
	return objectSchema([]string{"text"}, map[string]any{
		"text":  stringProp("The fact to remember, already distilled (not the raw transcript)."),
		"scope": stringProp("private (default) | company. private = you + the requesting person; company = shared with the whole workspace."),
		"pin":   map[string]any{"type": "boolean", "description": "Force never-forget. Optional; default false."},
		"source": map[string]any{
			"type":                 "object",
			"description":          "Optional provenance, e.g. {\"type\":\"issue\",\"id\":\"FIR-123\"}.",
			"additionalProperties": true,
		},
	})
}
func (t *CerebroMemoryRememberTool) Call(ctx context.Context, args map[string]any) (string, error) {
	g := t.gates(ctx)
	if !g.FlagOn {
		return "", fmt.Errorf("memory is not enabled for this workspace")
	}
	if !g.HasCapability || !g.CanWrite {
		return "", fmt.Errorf("memory write is not enabled for this agent and user (write switch is off)")
	}
	scope, subject, err := t.stampScope(args)
	if err != nil {
		return "", err
	}
	text, _ := args["text"].(string)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("text is required")
	}
	out := map[string]any{
		"scope":      scope,
		"subject_id": subject,
		"text":       text,
	}
	if pin, ok := args["pin"].(bool); ok && pin {
		out["pin"] = true
	}
	if source, ok := args["source"].(map[string]any); ok {
		out["source"] = source
	}
	return t.callService(ctx, "memory_remember", out)
}

// ── memory_recall ─────────────────────────────────────────────────────────────

type CerebroMemoryRecallTool struct{ cerebroMemoryToolBase }

func (t *CerebroMemoryRecallTool) Name() string { return "memory_recall" }
func (t *CerebroMemoryRecallTool) Description() string {
	return "Search memory in natural language. PROTOCOL: at the START of a task, recall what is already known about the topic before doing new work. By default this searches every store you may read — your private memory (when your read switch is on) and the shared company memory. Results are labeled by store."
}
func (t *CerebroMemoryRecallTool) InputSchema() map[string]any {
	return objectSchema([]string{"query"}, map[string]any{
		"query": stringProp("What you are trying to recall, in natural language."),
		"scope": stringProp("all (default) | private | company. all = every store you may read."),
		"limit": numberProp("Max results per store (1-50). Optional; default 10."),
	})
}
func (t *CerebroMemoryRecallTool) Call(ctx context.Context, args map[string]any) (string, error) {
	g := t.gates(ctx)
	if !g.FlagOn {
		return "", fmt.Errorf("memory is not enabled for this workspace")
	}
	scope, _ := args["scope"].(string)
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = memoryScopeAll
	}
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query is required")
	}
	base := map[string]any{"query": query}
	if limit, ok := args["limit"].(float64); ok && limit >= 1 {
		base["limit"] = int(limit)
	}

	var searches []map[string]any
	switch scope {
	case memoryScopePrivate:
		if !g.HasCapability || !g.CanRead {
			return "", fmt.Errorf("private memory read is not enabled for this agent and user (read switch is off)")
		}
		subject, err := t.privateSubjectID()
		if err != nil {
			return "", err
		}
		searches = append(searches, mergeMemoryArgs(base, memoryScopePrivate, subject))
	case memoryScopeCompany:
		searches = append(searches, mergeMemoryArgs(base, memoryScopeCompany, t.companySubjectID()))
	case memoryScopeAll:
		if g.HasCapability && g.CanRead {
			if subject, err := t.privateSubjectID(); err == nil {
				searches = append(searches, mergeMemoryArgs(base, memoryScopePrivate, subject))
			}
		}
		searches = append(searches, mergeMemoryArgs(base, memoryScopeCompany, t.companySubjectID()))
	default:
		return "", fmt.Errorf("scope must be one of: all, private, company")
	}

	var parts []string
	for _, s := range searches {
		label, _ := s["scope"].(string)
		res, err := t.callService(ctx, "memory_recall", s)
		if err != nil {
			// Partial recall is better than none: report the failing store
			// inline instead of dropping the stores that answered.
			parts = append(parts, fmt.Sprintf("[%s] recall failed: %v", label, err))
			continue
		}
		parts = append(parts, fmt.Sprintf("[%s]\n%s", label, res))
	}
	return strings.Join(parts, "\n\n"), nil
}

func mergeMemoryArgs(base map[string]any, scope, subject string) map[string]any {
	out := make(map[string]any, len(base)+2)
	for k, v := range base {
		out[k] = v
	}
	out["scope"] = scope
	out["subject_id"] = subject
	return out
}

// ── memory_delete ─────────────────────────────────────────────────────────────

type CerebroMemoryDeleteTool struct{ cerebroMemoryToolBase }

func (t *CerebroMemoryDeleteTool) Name() string { return "memory_delete" }
func (t *CerebroMemoryDeleteTool) Description() string {
	return "Delete one memory by its id — for a fact that turned out wrong, or a right-to-be-forgotten request. You can only delete memories in stores you own (your private store, or the company store when you may write); the ownership check runs server-side."
}
func (t *CerebroMemoryDeleteTool) InputSchema() map[string]any {
	return objectSchema([]string{"memory_id"}, map[string]any{
		"memory_id": stringProp("The memory_id returned by memory_remember or memory_recall."),
		"scope":     stringProp("private (default) | company — which store the memory lives in."),
	})
}
func (t *CerebroMemoryDeleteTool) Call(ctx context.Context, args map[string]any) (string, error) {
	g := t.gates(ctx)
	if !g.FlagOn {
		return "", fmt.Errorf("memory is not enabled for this workspace")
	}
	if !g.HasCapability || !g.CanWrite {
		return "", fmt.Errorf("memory delete is not enabled for this agent and user (write switch is off)")
	}
	memoryID, _ := args["memory_id"].(string)
	if strings.TrimSpace(memoryID) == "" {
		return "", fmt.Errorf("memory_id is required")
	}
	scope, subject, err := t.stampScope(args)
	if err != nil {
		return "", err
	}
	// scope + subject_id let the service verify the memory actually belongs to
	// this caller's store before deleting (ownership check, service-side).
	return t.callService(ctx, "memory_delete", map[string]any{
		"memory_id":  memoryID,
		"scope":      scope,
		"subject_id": subject,
	})
}

// stampScope resolves the caller-chosen scope ("private" default, or
// "company") to the canonical scope plus the SERVER-STAMPED subject id.
func (b *cerebroMemoryToolBase) stampScope(args map[string]any) (string, string, error) {
	scope, _ := args["scope"].(string)
	scope = strings.ToLower(strings.TrimSpace(scope))
	switch scope {
	case "", memoryScopePrivate:
		subject, err := b.privateSubjectID()
		if err != nil {
			return "", "", err
		}
		return memoryScopePrivate, subject, nil
	case memoryScopeCompany:
		return memoryScopeCompany, b.companySubjectID(), nil
	default:
		return "", "", fmt.Errorf("scope must be private or company")
	}
}

// ── offering ──────────────────────────────────────────────────────────────────

// cerebroMemoryToolMeta feeds the admin tools API. Status is EXCLUDED on
// purpose: the only path to these tools is the three memory gates — a manual
// agent_tool_grant row must never be able to enable them (and could not call
// them anyway; every Call re-resolves the gates).
func cerebroMemoryToolMeta() []ToolMeta {
	return []ToolMeta{
		{Name: "memory_recall", Description: (&CerebroMemoryRecallTool{}).Description(), Status: ToolStatusExcluded},
		{Name: "memory_remember", Description: (&CerebroMemoryRememberTool{}).Description(), Status: ToolStatusExcluded},
		{Name: "memory_delete", Description: (&CerebroMemoryDeleteTool{}).Description(), Status: ToolStatusExcluded},
	}
}

// CerebroMemoryToolSchemas exposes the static input schemas for the admin
// tools API. The memory tools are per-task constructions (never in the
// throwaway schema registry), so the router merges these in explicitly.
func CerebroMemoryToolSchemas() map[string]map[string]any {
	return map[string]map[string]any{
		"memory_recall":   (&CerebroMemoryRecallTool{}).InputSchema(),
		"memory_remember": (&CerebroMemoryRememberTool{}).InputSchema(),
		"memory_delete":   (&CerebroMemoryDeleteTool{}).InputSchema(),
	}
}

// CerebroMemoryToolsForTask resolves the three memory gates for this task's
// (workspace, agent, originating user) and returns the memory tools the task
// may use right now. Empty on any gate failure (fail closed):
//
//   - memory_recall: workspace flag on (company memory is readable by every
//     agent once the feature is on; the private arm inside the tool still
//     requires the read switch).
//   - memory_remember, memory_delete: flag + capability + write switch.
//
// Call-time re-checks inside each tool make this offering advisory rather than
// load-bearing — but keeping the list honest means the model is never shown a
// tool every call of which would fail.
func CerebroMemoryToolsForTask(ctx context.Context, cerebro memoryGateQuerier, tctx ToolContext, originUser pgtype.UUID) []Tool {
	g := resolveCerebroMemoryGates(ctx, cerebro, tctx.WorkspaceID, tctx.AgentID, originUser)
	if !g.FlagOn {
		return nil
	}
	base := cerebroMemoryToolBase{cerebro: cerebro, tctx: tctx, origin: originUser}
	tools := []Tool{&CerebroMemoryRecallTool{base}}
	if g.HasCapability && g.CanWrite {
		tools = append(tools,
			&CerebroMemoryRememberTool{base},
			&CerebroMemoryDeleteTool{base},
		)
	}
	return tools
}
