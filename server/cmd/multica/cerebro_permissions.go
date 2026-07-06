// CEREBRO-PATCH(cerebro-permissions-cli): FIR-1609 cerebro-only file —
// `multica permissions` CLI over the unified tool-policy engine.
//
// The unified engine (server/internal/cerebro/toolpolicy) already resolves every
// agent action against the Workspace→Runtime→Agent→Group→User/System chain and
// records WHY: the Effective setting plus DecidedBy / CappedBy / Reason. Until now
// that decision was only reachable through the web UI (the tool-policy table) and
// the raw HTTP API. This command exposes the same engine on the CLI so an agent or
// an operator can:
//
//   - `permissions explain` — ask, for an agent (and optionally a member / groups /
//     runtime / system), what every tool resolves to and WHY a given agent+member
//     combination is blocked (`--blocked-only` shows only the Ask/Deny rows).
//   - `permissions set`     — author one Allow / Ask / Deny / Inherit rule at one
//     layer, optionally with a WHEN condition (host allowlist, actions, or CEL).
//   - `permissions clear`   — remove one layer's authored rule for one tool.
//
// All three speak to GET/PUT/DELETE /api/workspaces/{id}/tool-policy, which is the
// same admin/owner-gated surface the UI uses, so the server keeps a single
// authority for both the read model and the write path.
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// --- wire DTOs (mirror server/internal/cerebro/toolpolicy/handler.go) ---------

type permEffective struct {
	Setting   string `json:"setting"`
	DecidedBy string `json:"decided_by"`
	CappedBy  string `json:"capped_by"`
	Reason    string `json:"reason"`
}

type permGroupAttr struct {
	Name  string `json:"name"`
	Owner string `json:"owner"`
}

type permLayers struct {
	Workspace *string `json:"workspace"`
	Runtime   *string `json:"runtime"`
	Agent     *string `json:"agent"`
	Group     *string `json:"group"`
	User      *string `json:"user"`
	System    *string `json:"system"`
}

type permRow struct {
	ToolKey         string          `json:"tool_key"`
	ResourcePattern string          `json:"resource_pattern"`
	Title           string          `json:"title"`
	Category        string          `json:"category"`
	Source          string          `json:"source"`
	Layers          permLayers      `json:"layers"`
	Effective       permEffective   `json:"effective"`
	CappedByGroups  []permGroupAttr `json:"capped_by_groups"`
}

type permTableResponse struct {
	Tools []permRow `json:"tools"`
}

// permConditionRequest mirrors toolpolicy.conditionRequest. Omitted (nil) when the
// rule carries no WHEN clause.
type permConditionRequest struct {
	HostAllowlist []string `json:"host_allowlist,omitempty"`
	Actions       []string `json:"actions,omitempty"`
	Expr          string   `json:"expr,omitempty"`
}

// permSetRequest mirrors toolpolicy.setRequest.
type permSetRequest struct {
	ToolKey         string                `json:"tool_key"`
	Layer           string                `json:"layer"`
	SubjectID       string                `json:"subject_id"`
	ResourcePattern string                `json:"resource_pattern,omitempty"`
	Setting         string                `json:"setting"`
	Condition       *permConditionRequest `json:"condition,omitempty"`
}

// --- validation tables --------------------------------------------------------

var permLayerNames = map[string]bool{
	"workspace": true, "runtime": true, "agent": true,
	"group": true, "user": true, "system": true,
}

// permDecisions are the authorable settings. "inherit" means "no rule at this
// layer" — set writes it (clearing the explicit choice), clear removes the row.
// "disable" (FIR-2351 follow-up, product decision 2026-07-06) is workspace-only:
// a hard, unopenable floor for one permission — see the workspace-layer check
// in runPermSet below, which mirrors toolpolicy.validSetting/Store.Set server-side.
var permDecisions = map[string]bool{
	"allow": true, "ask": true, "deny": true, "inherit": true, "disable": true,
}

// --- commands -----------------------------------------------------------------

var permissionsCmd = &cobra.Command{
	Use:   "permissions",
	Short: "Inspect and control the unified tool-policy permission engine",
	Long: "Read and write the one permission engine that decides every agent action.\n\n" +
		"`explain` shows, per tool, the effective Allow/Ask/Deny and which layer decided " +
		"(or capped) it — so you can see exactly why an agent+member combination is blocked. " +
		"`set` / `clear` author or remove one Allow/Ask/Deny rule at one layer. Admin/owner only.",
}

func init() {
	explain := &cobra.Command{
		Use:   "explain",
		Short: "Show what every tool resolves to for an agent/member, and why",
		Long: "Resolves the unified tool-policy chain for the given context and prints, per tool, " +
			"the effective decision (Allow/Ask/Deny), which layer decided it, which layer (if any) " +
			"capped it, and a human reason. Pass any of --agent / --user / --group / --runtime / " +
			"--system to scope the context; --blocked-only shows only the tools that are Denied or " +
			"gated behind Ask. The --user flag accepts a user UUID or an email.",
		Args: cobra.NoArgs,
		RunE: runPermExplainCmd,
	}
	explain.Flags().String("agent", "", "Agent UUID to resolve for")
	explain.Flags().String("runtime", "", "Runtime UUID to resolve for")
	explain.Flags().String("user", "", "Member to resolve for (user UUID or email)")
	explain.Flags().StringSlice("group", nil, "Group UUID to include (repeatable)")
	explain.Flags().String("system", "", "System/autopilot UUID to scope the System layer to")
	explain.Flags().String("tool", "", "Only show this tool key (substring match)")
	explain.Flags().Bool("blocked-only", false, "Only show tools that resolve to Deny or Ask")
	explain.Flags().String("output", "table", "Output format: table or json")
	permissionsCmd.AddCommand(explain)

	set := &cobra.Command{
		Use:   "set",
		Short: "Author one Allow/Ask/Deny/Inherit rule at one layer",
		Long: "Writes a single rule to the unified engine: at --layer for --subject, set --tool to " +
			"--decision. Optionally scope it to a --resource pattern and attach a WHEN condition " +
			"(--when-host / --when-action / --when-expr). A System-layer rule that would exceed its " +
			"owner's ceiling is rejected by the server.",
		Args: cobra.NoArgs,
		RunE: runPermSetCmd,
	}
	set.Flags().String("tool", "", "Tool key, e.g. Bash or firtal_registry (required)")
	set.Flags().String("layer", "", "Layer: workspace|runtime|agent|group|user|system (required)")
	set.Flags().String("subject", "", "Subject UUID the rule applies to at that layer (required)")
	set.Flags().String("decision", "", "Decision: allow|ask|deny|inherit|disable (required; disable is workspace-layer only)")
	set.Flags().String("resource", "", "Resource pattern the rule is scoped to (optional)")
	set.Flags().StringSlice("when-host", nil, "WHEN condition: host allowlist (repeatable)")
	set.Flags().StringSlice("when-action", nil, "WHEN condition: allowed actions (repeatable)")
	set.Flags().String("when-expr", "", "WHEN condition: a CEL expression")
	_ = set.MarkFlagRequired("tool")
	_ = set.MarkFlagRequired("layer")
	_ = set.MarkFlagRequired("subject")
	_ = set.MarkFlagRequired("decision")
	permissionsCmd.AddCommand(set)

	clear := &cobra.Command{
		Use:   "clear",
		Short: "Remove one layer's authored rule for one tool",
		Long: "Deletes the explicit rule at --layer for --subject on --tool (optionally scoped to " +
			"--resource). The tool then falls back to whatever the chain resolves without that rule.",
		Args: cobra.NoArgs,
		RunE: runPermClearCmd,
	}
	clear.Flags().String("tool", "", "Tool key (required)")
	clear.Flags().String("layer", "", "Layer: workspace|runtime|agent|group|user|system (required)")
	clear.Flags().String("subject", "", "Subject UUID the rule applies to at that layer (required)")
	clear.Flags().String("resource", "", "Resource pattern the rule was scoped to (optional)")
	_ = clear.MarkFlagRequired("tool")
	_ = clear.MarkFlagRequired("layer")
	_ = clear.MarkFlagRequired("subject")
	permissionsCmd.AddCommand(clear)
}

// permBasePath returns the workspace-scoped tool-policy endpoint, or an error when
// no workspace is selected (the engine is always workspace-scoped).
func permBasePath(client *cli.APIClient) (string, error) {
	if client.WorkspaceID == "" {
		return "", fmt.Errorf("no workspace selected: set one with `multica config set workspace_id <id>` or MULTICA_WORKSPACE_ID")
	}
	return "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/tool-policy", nil
}

// --- explain ------------------------------------------------------------------

type permExplainOpts struct {
	agentID, runtimeID, userID, systemID string
	groupIDs                             []string
	toolFilter                           string
	blockedOnly                          bool
	output                               string
}

func runPermExplainCmd(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	user, _ := cmd.Flags().GetString("user")
	userID, err := permResolveUser(ctx, client, user)
	if err != nil {
		return err
	}
	agent, _ := cmd.Flags().GetString("agent")
	runtime, _ := cmd.Flags().GetString("runtime")
	system, _ := cmd.Flags().GetString("system")
	groups, _ := cmd.Flags().GetStringSlice("group")
	tool, _ := cmd.Flags().GetString("tool")
	blocked, _ := cmd.Flags().GetBool("blocked-only")
	output, _ := cmd.Flags().GetString("output")

	return runPermExplain(ctx, client, permExplainOpts{
		agentID:     agent,
		runtimeID:   runtime,
		userID:      userID,
		systemID:    system,
		groupIDs:    groups,
		toolFilter:  tool,
		blockedOnly: blocked,
		output:      output,
	})
}

func runPermExplain(ctx context.Context, client *cli.APIClient, opts permExplainOpts) error {
	base, err := permBasePath(client)
	if err != nil {
		return err
	}
	q := url.Values{}
	if opts.agentID != "" {
		q.Set("agent_id", opts.agentID)
	}
	if opts.runtimeID != "" {
		q.Set("runtime_id", opts.runtimeID)
	}
	if opts.userID != "" {
		q.Set("user_id", opts.userID)
	}
	if opts.systemID != "" {
		q.Set("system_id", opts.systemID)
	}
	for _, g := range opts.groupIDs {
		if g != "" {
			q.Add("group_id", g)
		}
	}
	path := base
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}

	var resp permTableResponse
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return fmt.Errorf("explain permissions: %w", err)
	}

	rows := filterPermRows(resp.Tools, opts.toolFilter, opts.blockedOnly)

	if opts.output == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}

	headers := []string{"TOOL", "RESOURCE", "EFFECTIVE", "DECIDED BY", "CAPPED BY", "WHY"}
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, []string{
			r.ToolKey,
			permDash(r.ResourcePattern),
			strings.ToUpper(permDash(r.Effective.Setting)),
			permDash(r.Effective.DecidedBy),
			permDash(r.Effective.CappedBy),
			permWhy(r),
		})
	}
	cli.PrintTable(os.Stdout, headers, out)
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "no matching tools")
	}
	return nil
}

// filterPermRows applies the optional tool substring filter and blocked-only
// (deny/ask) filter.
func filterPermRows(rows []permRow, toolFilter string, blockedOnly bool) []permRow {
	out := make([]permRow, 0, len(rows))
	for _, r := range rows {
		if toolFilter != "" && !strings.Contains(strings.ToLower(r.ToolKey), strings.ToLower(toolFilter)) {
			continue
		}
		if blockedOnly {
			s := strings.ToLower(r.Effective.Setting)
			if s != "deny" && s != "ask" {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// permWhy renders the human reason plus any group attribution behind a cap, so an
// operator sees "Capped by group <name> (owner: <person>)" inline.
func permWhy(r permRow) string {
	why := r.Effective.Reason
	if len(r.CappedByGroups) > 0 {
		names := make([]string, 0, len(r.CappedByGroups))
		for _, g := range r.CappedByGroups {
			if g.Owner != "" {
				names = append(names, fmt.Sprintf("%s (owner: %s)", g.Name, g.Owner))
			} else {
				names = append(names, g.Name)
			}
		}
		group := "group " + strings.Join(names, ", ")
		if why == "" {
			why = group
		} else {
			why = why + " — " + group
		}
	}
	return permDash(why)
}

// --- set ----------------------------------------------------------------------

func runPermSetCmd(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tool, _ := cmd.Flags().GetString("tool")
	layer, _ := cmd.Flags().GetString("layer")
	subject, _ := cmd.Flags().GetString("subject")
	decision, _ := cmd.Flags().GetString("decision")
	resource, _ := cmd.Flags().GetString("resource")
	whenHost, _ := cmd.Flags().GetStringSlice("when-host")
	whenAction, _ := cmd.Flags().GetStringSlice("when-action")
	whenExpr, _ := cmd.Flags().GetString("when-expr")

	req := permSetRequest{
		ToolKey:         tool,
		Layer:           strings.ToLower(layer),
		SubjectID:       subject,
		ResourcePattern: resource,
		Setting:         strings.ToLower(decision),
	}
	if len(whenHost) > 0 || len(whenAction) > 0 || whenExpr != "" {
		req.Condition = &permConditionRequest{
			HostAllowlist: whenHost,
			Actions:       whenAction,
			Expr:          whenExpr,
		}
	}
	return runPermSet(ctx, client, req)
}

func runPermSet(ctx context.Context, client *cli.APIClient, req permSetRequest) error {
	if !permLayerNames[req.Layer] {
		return fmt.Errorf("invalid layer %q: want one of workspace|runtime|agent|group|user|system", req.Layer)
	}
	if !permDecisions[req.Setting] {
		return fmt.Errorf("invalid decision %q: want one of allow|ask|deny|inherit|disable", req.Setting)
	}
	if req.Setting == "disable" && req.Layer != "workspace" {
		return fmt.Errorf("disable is only valid at the workspace layer (got layer %q)", req.Layer)
	}
	base, err := permBasePath(client)
	if err != nil {
		return err
	}
	if err := client.PutJSON(ctx, base, req, nil); err != nil {
		return fmt.Errorf("set permission: %w", err)
	}
	scope := req.ToolKey
	if req.ResourcePattern != "" {
		scope += " / " + req.ResourcePattern
	}
	fmt.Fprintf(os.Stdout, "set %s = %s at %s layer (subject %s)\n",
		scope, strings.ToUpper(req.Setting), req.Layer, req.SubjectID)
	return nil
}

// --- clear --------------------------------------------------------------------

func runPermClearCmd(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tool, _ := cmd.Flags().GetString("tool")
	layer, _ := cmd.Flags().GetString("layer")
	subject, _ := cmd.Flags().GetString("subject")
	resource, _ := cmd.Flags().GetString("resource")
	return runPermClear(ctx, client, tool, strings.ToLower(layer), subject, resource)
}

func runPermClear(ctx context.Context, client *cli.APIClient, tool, layer, subject, resource string) error {
	if !permLayerNames[layer] {
		return fmt.Errorf("invalid layer %q: want one of workspace|runtime|agent|group|user|system", layer)
	}
	base, err := permBasePath(client)
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("tool_key", tool)
	q.Set("layer", layer)
	q.Set("subject_id", subject)
	if resource != "" {
		q.Set("resource_pattern", resource)
	}
	if err := client.DeleteJSON(ctx, base+"?"+q.Encode()); err != nil {
		return fmt.Errorf("clear permission: %w", err)
	}
	scope := tool
	if resource != "" {
		scope += " / " + resource
	}
	fmt.Fprintf(os.Stdout, "cleared %s rule at %s layer (subject %s)\n", scope, layer, subject)
	return nil
}

// --- helpers ------------------------------------------------------------------

// permResolveUser maps an email to a user UUID via the workspace members list, or
// returns the input unchanged when it is empty or already a UUID (no "@").
func permResolveUser(ctx context.Context, client *cli.APIClient, user string) (string, error) {
	if user == "" || !strings.Contains(user, "@") {
		return user, nil
	}
	if client.WorkspaceID == "" {
		return "", fmt.Errorf("no workspace selected: cannot resolve member email %q", user)
	}
	var members []map[string]any
	path := "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/members"
	if err := client.GetJSON(ctx, path, &members); err != nil {
		return "", fmt.Errorf("resolve member email: %w", err)
	}
	for _, m := range members {
		if email, _ := m["email"].(string); strings.EqualFold(email, user) {
			if id, _ := m["user_id"].(string); id != "" {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("no member with email %q in this workspace", user)
}

func permDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
