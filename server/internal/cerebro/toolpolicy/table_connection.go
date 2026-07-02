package toolpolicy

// table_connection.go is the per-tool half of the admin table for workspace
// connections (TECH-3156).
//
// table.go emits one capability-wide row per connection ("connection:<name>",
// resource_pattern = ''). That row controls the whole connection. But a single
// MCP connection exposes many underlying tools (customer-service alone exposes
// ~23: draft_reply, lookup_order, search_knowledge, …) and admins need to allow
// or deny each one individually.
//
// So — exactly like repos (table_repo.go) — we emit, per connection, one extra
// row per discovered tool carrying a non-empty resource_pattern (the tool name)
// and that (connection, tool) cell's explicit per-layer settings + resolved
// Effective verdict. The connection's display name is the row Category so the UI
// can group a connection's tools under it. The five-level inheritance chain is
// reused unchanged: a per-tool row can only tighten the connection-wide row.
//
// The tool universe is read from workspace_connection.tools, which the "Test
// connection" call persists from the MCP server's tools/list result.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// pgUndefinedTable is the Postgres SQLSTATE for "relation does not exist". The
// workspace_connection table ships with a cerebro migration; if a deployment is
// mid-migration (or connections are simply not provisioned yet) we treat it as
// "no connections" rather than failing the whole permissions table.
const pgUndefinedTable = "42P01"

func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUndefinedTable
}

// connectionToolKeyPrefix mirrors connections.CapabilityKey ("connection:<name>")
// without importing the connections package — toolpolicy stays dependency-free of
// the feature packages whose policy it resolves.
const connectionToolKeyPrefix = "connection:"

// connectionToolSource labels per-tool rows for MCP (mcp_http) connections, and
// connectionEndpointSource labels per-endpoint+method rows for REST (api)
// connections. Both carry a non-empty resource_pattern (the tool name, or
// "<METHOD> <path>"), so the UI separates them from the capability-wide
// connection row and from per-repo rows. Only MCP tools are runtime-enforced via
// --disallowedTools today; API rows are config-only (CRUD control) until an
// API-call enforcement path is designed — see DeniedConnectionTools.
const (
	connectionToolSource     = "connection-tool"
	connectionEndpointSource = "connection-endpoint"
)

// connectionTool is one tool discovered on a connection, persisted on the
// workspace_connection.tools JSON array.
type connectionTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// endpointPermission mirrors connections.EndpointPermission: one REST path and
// the HTTP methods configured on it. Used to synthesize per-endpoint+method rows
// for API connections.
type endpointPermission struct {
	Path    string   `json:"path"`
	Methods []string `json:"methods"`
}

// connectionRow pairs a connection's policy key with the human label its tools
// group under (the connection display name), the connection kind ("mcp_http" or
// "api"), and the per-tool rows (MCP tools, or synthesized "<METHOD> <path>"
// entries for API endpoints).
type connectionRow struct {
	name        string
	displayName string
	kind        string
	tools       []connectionTool
}

// sourceForKind maps a connection type to the row Source the UI groups on.
func sourceForKind(kind string) string {
	if kind == "api" {
		return connectionEndpointSource
	}
	return connectionToolSource
}

// appendConnectionToolRows discovers the workspace's enabled MCP connections and
// appends, per connection, one row per discovered tool carrying that (connection,
// tool) cell's explicit per-layer settings and resolved Effective verdict.
// groupIDs is the already-resolved group set for the query's context, reused so
// this path resolves the Group layer against the same groups as the
// capability-wide rows.
//
// Like repo rows, connection-tool rows are emitted on every view (including a
// runtime-scoped one): connection capabilities are not runtime-reported, so the
// runtime filter in table.go would otherwise hide them — but connection access is
// authored at all five layers, so the admin must see them everywhere.
func (s *Store) appendConnectionToolRows(ctx context.Context, in TableQuery, groupIDs []pgtype.UUID, out []TableRow) ([]TableRow, error) {
	conns, err := s.discoverConnectionTools(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if len(conns) == 0 {
		return out, nil
	}

	settings, err := s.loadConnectionPolicySettings(ctx, in, groupIDs)
	if err != nil {
		return nil, err
	}

	// The connection-wide row (source 'connection', resource_pattern '') is the
	// control for the WHOLE connection — its Deny/Ask must cascade to every tool
	// on that connection so "deny the connection for this runtime/agent" actually
	// blocks the tools, not just the (display-only) wide row (TECH-3180). The
	// connection-wide rows were already resolved into `out` by Table(); reuse each
	// one's Effective setting as the BASE for its per-tool resolution so a per-tool
	// choice can only tighten the connection-wide one, never loosen it.
	connWideBase := connectionWideBases(out)

	// Fallback when no capability-wide row is in `out`. Table() only emits a
	// source='connection' wide row when a cerebro_capability row exists for the
	// connection (MCP connections register one; an API connection authored only in
	// workspace_connection has none). Without that row the connection-wide
	// Deny/Ask authored on `connection:<name>` (resource_pattern '') would never
	// reach the per-endpoint rows, so a workspace/agent-level "deny this whole
	// connection" would silently fail to cap its endpoints (FIR-2166). Resolve the
	// authored wide settings straight from cerebro_tool_policy and use them as the
	// base wherever the capability-derived base is absent.
	authoredWideBase, err := s.connectionWideAuthoredBases(ctx, in, groupIDs)
	if err != nil {
		return nil, err
	}

	for _, conn := range conns {
		toolKey := connectionToolKeyPrefix + conn.name
		source := sourceForKind(conn.kind)
		base := in.Base
		if w, ok := authoredWideBase[toolKey]; ok && rank(w) >= 0 {
			base = w
		}
		if w, ok := connWideBase[toolKey]; ok && rank(w) >= 0 {
			base = w
		}
		for _, t := range conn.tools {
			row := TableRow{
				ToolKey:         toolKey,
				ResourcePattern: t.Name,
				Title:           t.Name,
				Category:        conn.displayName,
				Source:          source,
				Layers:          map[Layer]Setting{},
				Conditions:      map[Layer]*Condition{},
			}
			if cell, ok := settings[repoPolicyKey{toolKey, t.Name}]; ok {
				for l, set := range cell.layers {
					row.Layers[l] = set
				}
				for l, cond := range cell.conditions {
					row.Conditions[l] = cond
				}
				if len(cell.groups) > 0 {
					row.Layers[LayerGroup] = CombineGroups(cell.groups...)
				}
			}
			row.Effective = Resolve(Input{Settings: row.Layers, Base: base})
			out = append(out, row)
		}
	}
	return out, nil
}

// connectionWideBases maps each connection's policy key to the resolved Effective
// setting of its connection-wide row (source 'connection', empty resource
// pattern) already present in out. It is the per-connection floor every per-tool
// row inherits from: the connection-wide chain (Workspace › Runtime › Agent ›
// Group › User) decides the connection default, and per-tool rows tighten under
// it. A connection with no capability-wide row contributes nothing (callers fall
// back to the workspace Base).
func connectionWideBases(out []TableRow) map[string]Setting {
	bases := map[string]Setting{}
	for _, r := range out {
		if r.Source == "connection" && r.ResourcePattern == "" {
			bases[r.ToolKey] = r.Effective.Setting
		}
	}
	return bases
}

// connectionWideAuthoredBases resolves, per connection policy key, the effective
// connection-wide setting from the authored rows in cerebro_tool_policy
// (tool_key 'connection:%', resource_pattern ”) folded through the same
// Workspace › Runtime › Agent › Group › User chain as every other layer. It is
// the fallback for connectionWideBases: it does NOT depend on a cerebro_capability
// row existing, so a connection authored only in workspace_connection (no
// capability row — the common case for API connections) still propagates its
// connection-wide cap to its per-endpoint rows. The subject predicates mirror
// loadConnectionPolicySettings so an absent subject never matches and that layer
// stays Inherit. Keys with no authored wide row are simply absent (callers keep
// the query Base).
func (s *Store) connectionWideAuthoredBases(ctx context.Context, in TableQuery, groupIDs []pgtype.UUID) (map[string]Setting, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.tool_key, p.layer, p.setting
		FROM cerebro_tool_policy p
		WHERE p.workspace_id = $1
		  AND p.resource_pattern = ''
		  AND p.tool_key LIKE 'connection:%'
		  AND (
		    (p.layer = 'workspace' AND p.subject_id = $1) OR
		    (p.layer = 'runtime'   AND p.subject_id = $2) OR
		    (p.layer = 'agent'     AND p.subject_id = $3) OR
		    (p.layer = 'user'      AND p.subject_id = $4) OR
		    (p.layer = 'group'     AND p.subject_id = ANY($5::uuid[])) OR
		    (p.layer = 'on_behalf_of' AND p.subject_id = $6)
		  )
	`, in.WorkspaceID, in.RuntimeID, in.AgentID, in.UserID, groupIDs, in.OnBehalfOfID)
	if err != nil {
		if isUndefinedTable(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("toolpolicy: load connection-wide settings: %w", err)
	}
	defer rows.Close()

	type wideAcc struct {
		layers map[Layer]Setting
		groups []Setting
	}
	byKey := map[string]*wideAcc{}
	for rows.Next() {
		var toolKey, layer, setting string
		if err := rows.Scan(&toolKey, &layer, &setting); err != nil {
			return nil, fmt.Errorf("toolpolicy: scan connection-wide setting: %w", err)
		}
		a, ok := byKey[toolKey]
		if !ok {
			a = &wideAcc{layers: map[Layer]Setting{}}
			byKey[toolKey] = a
		}
		l := Layer(layer)
		set := Setting(setting)
		if l == LayerGroup {
			a.groups = append(a.groups, set)
		} else {
			a.layers[l] = set
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolpolicy: iterate connection-wide settings: %w", err)
	}

	out := map[string]Setting{}
	for key, a := range byKey {
		if len(a.groups) > 0 {
			a.layers[LayerGroup] = CombineGroups(a.groups...)
		}
		out[key] = Resolve(Input{Settings: a.layers, Base: in.Base}).Setting
	}
	return out, nil
}

// discoverConnectionTools returns each enabled connection in the workspace with
// its per-tool rows, ordered by created_at so the UI order is stable. For MCP
// (mcp_http) connections the rows are the persisted tools/list. For REST (api)
// connections the rows are synthesized per endpoint+method ("<METHOD> <path>")
// from endpoint_permissions, so the same Configure sheet drives CRUD control.
func (s *Store) discoverConnectionTools(ctx context.Context, workspaceID pgtype.UUID) ([]connectionRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT name, display_name, type, tools, endpoint_permissions
		FROM workspace_connection
		WHERE workspace_id = $1 AND enabled = true AND type IN ('mcp_http', 'api')
		ORDER BY created_at ASC
	`, workspaceID)
	if err != nil {
		if isUndefinedTable(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("toolpolicy: load connection tools: %w", err)
	}
	defer rows.Close()

	var out []connectionRow
	for rows.Next() {
		var name, displayName, kind string
		var toolsRaw, endpointsRaw []byte
		if err := rows.Scan(&name, &displayName, &kind, &toolsRaw, &endpointsRaw); err != nil {
			return nil, fmt.Errorf("toolpolicy: scan connection: %w", err)
		}
		var tools []connectionTool
		if kind == "api" {
			tools = endpointMethodTools(endpointsRaw)
		} else {
			_ = json.Unmarshal(toolsRaw, &tools)
		}
		if len(tools) == 0 {
			continue
		}
		out = append(out, connectionRow{name: name, displayName: displayName, kind: kind, tools: tools})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolpolicy: iterate connections: %w", err)
	}
	return out, nil
}

// endpointMethodTools expands an api connection's endpoint_permissions JSON into
// one synthetic tool per (method, path) — the unit the CRUD-control sheet gates.
// The resource_pattern is "<METHOD> <path>" (e.g. "POST /orders").
func endpointMethodTools(raw []byte) []connectionTool {
	var eps []endpointPermission
	if json.Unmarshal(raw, &eps) != nil {
		return nil
	}
	var tools []connectionTool
	for _, ep := range eps {
		if ep.Path == "" {
			continue
		}
		for _, m := range ep.Methods {
			if m == "" {
				continue
			}
			tools = append(tools, connectionTool{Name: m + " " + ep.Path})
		}
	}
	return tools
}

// ConnectionToolDenied reports whether a tool named toolName is denied as a
// connection tool for the given chain context — i.e. any "connection:<name>"
// capability has an effective Deny on resource_pattern == toolName, resolved
// through Workspace › Runtime › Agent › Group › User. It is the firtal-gateway
// half of TECH-3156: that runtime dispatches connection tools server-side (e.g.
// the customer-service MCP tools), bypassing the daemon's --disallowedTools, so
// the gateway must consult this directly before dispatch.
//
// A connection name is intentionally NOT required: the gateway only has the bare
// tool name (e.g. "draft_reply"), so we match the deny by resource_pattern. A
// missing workspace_connection table or no rows means "not denied" (allow).
func (s *Store) ConnectionToolDenied(ctx context.Context, workspaceID, runtimeID, agentID, userID pgtype.UUID, toolName string) (bool, error) {
	if toolName == "" {
		return false, nil
	}
	// Reuse the canonical resolver so the gateway path is identical to the daemon
	// path: DeniedConnectionTools already folds the connection-wide row (TECH-3180)
	// AND per-tool rows through the full Workspace › Runtime › Agent › Group › User
	// chain, returning "mcp__<connection>__<tool>" tokens for every effective Deny.
	tokens, err := s.DeniedConnectionTools(ctx, TableQuery{
		WorkspaceID: workspaceID,
		RuntimeID:   runtimeID,
		AgentID:     agentID,
		UserID:      userID,
	})
	if err != nil {
		return false, err
	}
	for _, tok := range tokens {
		// Token shape is mcp__<connection>__<tool>; the tool is the segment after
		// the connection (split on the first "__" after the prefix, as Claude Code
		// and the persona hook do).
		rest := strings.TrimPrefix(tok, "mcp__")
		_, tool, ok := strings.Cut(rest, "__")
		if ok && tool == toolName {
			return true, nil
		}
	}
	return false, nil
}

// ConnectionToolEffective resolves the effective Allow/Ask/Deny verdict for a
// single workspace-connection MCP tool through the full Workspace › Runtime ›
// Agent › Group › User chain. It is the Ask-capable generalisation of
// ConnectionToolDenied (TECH-3498): the Deny-only resolver could never surface
// an "Ask" row, so connection-tool Ask was stored and shown but never enforced.
//
// Both engines route their connection-tool decision through this one resolver so
// the gateway gate and the daemon resolve endpoint can never drift from the
// admin screen: a tool the screen shows as Ask is exactly a tool this returns
// SettingAsk for.
//
// Matching mirrors ConnectionToolDenied: the dispatched/queried tool carries a
// bare tool name (no connection prefix), so we match per-tool rows by their
// resource_pattern (the tool name). If more than one connection exposes a tool
// of the same name, the MOST restrictive verdict wins (Deny > Ask > Allow) so a
// name collision can never loosen a tighter rule. A tool with no matching
// connection row resolves to SettingAllow (ungated): connection permissions only
// ever tighten from an allow baseline.
func (s *Store) ConnectionToolEffective(ctx context.Context, workspaceID, runtimeID, agentID, userID pgtype.UUID, toolName string) (Setting, string, error) {
	if toolName == "" {
		return SettingAllow, "", nil
	}
	rows, err := s.Table(ctx, TableQuery{
		WorkspaceID: workspaceID,
		RuntimeID:   runtimeID,
		AgentID:     agentID,
		UserID:      userID,
	})
	if err != nil {
		return SettingAllow, "", err
	}
	result := SettingAllow
	connName := ""
	for _, r := range rows {
		if r.Source != connectionToolSource || r.ResourcePattern != toolName {
			continue
		}
		// Fold colliding rows to the tighter verdict (Deny > Ask > Allow) so a
		// same-named tool on a looser connection can never override a tighter rule.
		// Track the connection name of the deciding row whenever the verdict
		// tightens past the allow baseline, so callers can surface "which
		// integration" in the approval context (TECH-3498) and target a
		// permission-level grant at the right connection:<name> key. A tool left
		// on the allow baseline is ungated and reports no connection name.
		tightened := MoreRestrictive(result, r.Effective.Setting)
		if tightened != result && tightened != SettingAllow {
			connName = strings.TrimPrefix(r.ToolKey, connectionToolKeyPrefix)
		}
		result = tightened
	}
	return result, connName, nil
}

// ConnectionEndpointEffective resolves the effective verdict for a single
// API-connection endpoint call (`<METHOD> <path>`, e.g. "GET /secrets") for one
// actor, over the connection-wide row (`connection:<name>`, empty
// resource_pattern) AND the matching per-endpoint row (source
// connection-endpoint, resource_pattern "<METHOD> <path>"), at EVERY actor layer
// (Workspace › Runtime › Agent › Group › User), plus the tighten-only on_behalf_of
// layer (the delegated task initiator, keyed by onBehalfOfID — Deny-only, see the
// SECURITY note in the loop below). It is the engine half the API-call gate uses,
// so the gateway gate and the admin screen can never drift.
//
// API-connection endpoints resolve against a PER-CONNECTION default — FIR-2166
// "C" v2. Each connection carries a default_access mode (allow/ask/deny) chosen
// when it is created/edited (connections.DefaultAccess*); per-actor tool-policy
// rows override that default. This is NOT the tighten-only Resolve, for the same
// reason tools:test-as-user uses ResolveOptIn (FIR-1771): the tighten chain only
// ever tightens below its Base and refuses to loosen, so a Deny base could never
// be lifted by an Allow grant. The rule, over all of the above rows/layers:
//
//   - an explicit Deny at any layer wins (revokes) — fail-safe;
//   - else an explicit Allow at any layer grants the call;
//   - else an explicit Ask at any layer gates the call (human approval);
//   - else (no per-actor row) fall back to the connection's default_access.
//
// A server-side-dispatched API connection can reach internal `.internal` URLs
// with a stored credential the agent never sees (e.g. infisical-admin fronts the
// secrets box), so such a connection is set to default 'deny': "allow only Sara
// & Mia" is then an agent-layer Allow on connection:<name> per agent, and every
// other — and every NEW — agent is denied. A harmless API can be set to 'allow'
// (open, deny to restrict) or 'ask' (approval-gated). This is independent of MCP
// connections (ConnectionToolEffective / ConnectionToolDenied), which keep their
// Allow-baseline shape and are unaffected. Returns the resolved setting and, on a
// non-Allow, the connection name so callers can surface "which integration".
func (s *Store) ConnectionEndpointEffective(ctx context.Context, workspaceID, runtimeID, agentID, userID, onBehalfOfID pgtype.UUID, connName, method, path string) (Setting, string, error) {
	if connName == "" || method == "" || path == "" {
		// An unidentifiable endpoint cannot be matched to a grant; fail closed.
		return SettingDeny, connName, nil
	}
	rows, err := s.Table(ctx, TableQuery{
		WorkspaceID:  workspaceID,
		RuntimeID:    runtimeID,
		AgentID:      agentID,
		UserID:       userID,
		OnBehalfOfID: onBehalfOfID,
	})
	if err != nil {
		// Return the error so the caller's fail-open(list)/fail-closed(call) policy
		// in apiEndpointSetting decides; the Deny is a safe default for any caller
		// that ignores the error.
		return SettingDeny, connName, err
	}
	wantKey := connectionToolKeyPrefix + connName
	wantPattern := method + " " + path
	var hasAllow, hasAsk bool
	for _, r := range rows {
		if r.ToolKey != wantKey {
			continue
		}
		// The connection-wide row (empty pattern) and the matching per-endpoint
		// row both gate this call; everything else for the connection is ignored.
		isWide := r.Source == "connection" && r.ResourcePattern == ""
		isEndpoint := r.Source == connectionEndpointSource && r.ResourcePattern == wantPattern
		if !isWide && !isEndpoint {
			continue
		}
		// Inspect the raw per-layer settings (NOT r.Effective, whose tighten-only
		// verdict carries an Allow base). Deny wins immediately; otherwise an
		// explicit Allow grants and an explicit Ask gates, overriding the default.
		//
		// SECURITY (FIR-2441): the on_behalf_of layer (the delegated task initiator,
		// keyed by OnBehalfOfID above) is TIGHTEN-ONLY on this grant path. This path
		// GRANTS on any layer's explicit Allow, and it is the gate for the default-deny
		// secrets connections (infisical-admin). So a member (on_behalf_of) rule may only
		// ever REVOKE — an on_behalf_of Deny blocks the call, but an on_behalf_of
		// Allow/Ask is ignored (never grants, never gates). That keeps a member from
		// opening the secrets box to whoever drives the agent, while still letting a
		// member-level Deny hold across every agent that member delegates work to.
		for layer, set := range r.Layers {
			if layer == LayerOnBehalfOf {
				// Deny-only: honour a revoke, ignore any Allow/Ask so the initiator can
				// never grant or gate a call the owner's own layers did not.
				if set == SettingDeny {
					return SettingDeny, connName, nil
				}
				continue
			}
			switch set {
			case SettingDeny:
				return SettingDeny, connName, nil
			case SettingAllow:
				hasAllow = true
			case SettingAsk:
				hasAsk = true
			}
		}
	}
	// Per-actor precedence: Deny (returned above) > Allow > Ask. An explicit
	// per-actor row always overrides the connection default.
	if hasAllow {
		return SettingAllow, "", nil
	}
	if hasAsk {
		return SettingAsk, connName, nil
	}
	// No explicit per-actor row — fall back to the connection's configured default
	// access (allow/ask/deny), chosen when the connection was created/edited. A
	// missing row or column (mid-migration) resolves to Deny (fail closed).
	switch s.connectionDefaultAccess(ctx, workspaceID, connName) {
	case SettingAllow:
		return SettingAllow, "", nil
	case SettingAsk:
		return SettingAsk, connName, nil
	default:
		return SettingDeny, connName, nil
	}
}

// connectionDefaultAccess reads a connection's configured default access mode
// (FIR-2166 "C" v2) and maps it to a Setting. It queries workspace_connection
// directly (toolpolicy stays import-free of the connections package, exactly like
// discoverConnectionTools). Any error — missing connection, undefined column
// during a migration window, missing table — resolves to Deny (fail closed): a
// gate that fronts the secrets box must never open on an unresolved default.
func (s *Store) connectionDefaultAccess(ctx context.Context, workspaceID pgtype.UUID, connName string) Setting {
	var mode string
	if err := s.pool.QueryRow(ctx, `
		SELECT default_access FROM workspace_connection
		WHERE workspace_id = $1 AND name = $2
	`, workspaceID, connName).Scan(&mode); err != nil {
		return SettingDeny
	}
	switch mode {
	case "allow":
		return SettingAllow
	case "ask":
		return SettingAsk
	default:
		return SettingDeny
	}
}

// DeniedConnectionTools resolves, for the given chain context, every MCP
// connection tool whose effective verdict is Deny, and returns them as Claude
// Code --disallowedTools tokens ("mcp__<connection>__<tool>"). The daemon passes
// these at spawn so a denied tool is never callable — the runtime half of the
// per-tool permission feature (TECH-3156).
//
// It reuses the same Table resolution the admin UI reads from, so the runtime
// enforcement and the screen can never drift: a tool the screen shows as Deny is
// exactly a tool that ends up on this list.
func (s *Store) DeniedConnectionTools(ctx context.Context, in TableQuery) ([]string, error) {
	rows, err := s.Table(ctx, in)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		if r.Source != connectionToolSource {
			continue
		}
		if r.Effective.Setting != SettingDeny {
			continue
		}
		connName := strings.TrimPrefix(r.ToolKey, connectionToolKeyPrefix)
		if connName == "" || r.ResourcePattern == "" {
			continue
		}
		out = append(out, mcpToolToken(connName, r.ResourcePattern))
	}
	return out, nil
}

// DisallowedMCPTools is the claim-time adapter that satisfies the handler's
// ConnectionToolDenyResolver seam. It resolves the agent owner (the user
// ceiling) so user/group-layer denies are honoured, then returns the denied
// connection tools as --disallowedTools tokens.
//
// It FAILS CLOSED: any resolve error is returned, not swallowed, so the caller
// withholds the connections this claim rather than silently letting a denied
// tool through. A genuinely absent owner (agent with no owner_id) is not an
// error — the user layer is simply dropped.
func (s *Store) DisallowedMCPTools(ctx context.Context, workspaceID, runtimeID, agentID pgtype.UUID) ([]string, error) {
	var ownerID pgtype.UUID
	if agentID.Valid {
		err := s.pool.QueryRow(ctx,
			`SELECT owner_id FROM agent WHERE id = $1`, agentID).Scan(&ownerID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("toolpolicy: load agent owner: %w", err)
		}
	}
	return s.DeniedConnectionTools(ctx, TableQuery{
		WorkspaceID: workspaceID,
		RuntimeID:   runtimeID,
		AgentID:     agentID,
		UserID:      ownerID,
	})
}

// mcpToolToken builds Claude Code's namespaced MCP tool identifier. Claude Code
// exposes an MCP server's tools as "mcp__<server>__<tool>", and the connection
// name is the server key the daemon injects into --mcp-config (connections.Store
// BuildMCPConfig), so the two match by construction.
func mcpToolToken(connectionName, tool string) string {
	return "mcp__" + connectionName + "__" + tool
}

// MCPToolToken is the exported form of mcpToolToken. The connection tool
// resolver (FIR-2441) derives its --disallowedTools tokens from this single
// source, so it never re-implements the "mcp__<server>__<tool>" format that
// DeniedConnectionTools already emits.
func MCPToolToken(connectionName, tool string) string {
	return mcpToolToken(connectionName, tool)
}

// ConnectionToolVerdict is one mcp_http connection tool paired with its resolved
// effective verdict for a chain context.
type ConnectionToolVerdict struct {
	// Connection is the connection name (the "connection:<name>" key stripped).
	Connection string
	// Tool is the discovered tool name (the row's resource_pattern).
	Tool string
	// Setting is the effective verdict across the whole chain (Allow/Ask/Deny).
	Setting Setting
}

// ConnectionToolVerdicts resolves the effective verdict for EVERY discovered
// mcp_http connection tool in the query context. Where DeniedConnectionTools
// returns only the Deny tokens for --disallowedTools, this returns each MCP tool
// with its Allow/Ask/Deny verdict, so the connection tool resolver (FIR-2441)
// can decide per tool which become raw relay entries and which need a
// policy-aware path. It reads the same Table() rows the admin screen renders and
// DeniedConnectionTools filters, so the resolver, the runtime enforcement, and
// the screen can never drift. API-endpoint rows (connectionEndpointSource) are
// intentionally excluded — those resolve through ConnectionEndpointEffective.
func (s *Store) ConnectionToolVerdicts(ctx context.Context, in TableQuery) ([]ConnectionToolVerdict, error) {
	rows, err := s.Table(ctx, in)
	if err != nil {
		return nil, err
	}
	var out []ConnectionToolVerdict
	for _, r := range rows {
		if r.Source != connectionToolSource {
			continue
		}
		connName := strings.TrimPrefix(r.ToolKey, connectionToolKeyPrefix)
		if connName == "" || r.ResourcePattern == "" {
			continue
		}
		out = append(out, ConnectionToolVerdict{
			Connection: connName,
			Tool:       r.ResourcePattern,
			Setting:    r.Effective.Setting,
		})
	}
	return out, nil
}

// loadConnectionPolicySettings fetches every explicit per-layer setting authored
// for a connection tool (tool_key 'connection:%', resource_pattern non-empty) in
// the query's context, bucketed by (tool_key, tool). It mirrors the subject
// predicates of table_repo.go's loader so an absent subject id never matches and
// that layer stays Inherit.
func (s *Store) loadConnectionPolicySettings(ctx context.Context, in TableQuery, groupIDs []pgtype.UUID) (map[repoPolicyKey]*repoPolicyLayers, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.tool_key, p.resource_pattern, p.layer, p.setting, p.conditions
		FROM cerebro_tool_policy p
		WHERE p.workspace_id = $1
		  AND p.resource_pattern <> ''
		  AND p.tool_key LIKE 'connection:%'
		  AND (
		    (p.layer = 'workspace' AND p.subject_id = $1) OR
		    (p.layer = 'runtime'   AND p.subject_id = $2) OR
		    (p.layer = 'agent'     AND p.subject_id = $3) OR
		    (p.layer = 'user'      AND p.subject_id = $4) OR
		    (p.layer = 'group'     AND p.subject_id = ANY($5::uuid[])) OR
		    (p.layer = 'on_behalf_of' AND p.subject_id = $6)
		  )
	`, in.WorkspaceID, in.RuntimeID, in.AgentID, in.UserID, groupIDs, in.OnBehalfOfID)
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: load connection policy settings: %w", err)
	}
	defer rows.Close()

	out := map[repoPolicyKey]*repoPolicyLayers{}
	for rows.Next() {
		var toolKey, resourcePattern, layer, setting string
		var conditions []byte
		if err := rows.Scan(&toolKey, &resourcePattern, &layer, &setting, &conditions); err != nil {
			return nil, fmt.Errorf("toolpolicy: scan connection policy setting: %w", err)
		}
		key := repoPolicyKey{toolKey, resourcePattern}
		cell, ok := out[key]
		if !ok {
			cell = &repoPolicyLayers{layers: map[Layer]Setting{}, conditions: map[Layer]*Condition{}}
			out[key] = cell
		}
		l := Layer(layer)
		set := Setting(setting)
		if l == LayerGroup {
			cell.groups = append(cell.groups, set)
		} else {
			cell.layers[l] = set
			// Surface the rule's WHEN layer for single-subject layers only,
			// mirroring table_repo.go and the capability-wide decode (FIR-1708 D).
			cond, err := decodeCondition(conditions)
			if err != nil {
				return nil, fmt.Errorf("toolpolicy: decode connection conditions for %q at %s: %w", toolKey, l, err)
			}
			if cond != nil {
				cell.conditions[l] = cond
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolpolicy: iterate connection policy settings: %w", err)
	}
	return out, nil
}
