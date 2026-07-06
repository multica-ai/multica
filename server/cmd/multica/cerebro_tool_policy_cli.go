// CEREBRO-PATCH(cerebro-tool-policy-cli): FIR-1609 cerebro-only file —
// `multica tool-policy` CLI. Understand and manage the unified per-tool
// permission engine (the cerebro_tool_policy table) from the command line:
//
//	explain  why an agent (optionally scoped to a member/user, runtime, or
//	         groups) is allowed / asked / denied a tool — the resolved verdict
//	         plus which layer decided or capped it and the full layer stack.
//	list     the same, as a one-row-per-tool table.
//	set      one layer's choice for one tool (allow|ask|deny|inherit|disable).
//	         disable is workspace-layer only. Admin/owner.
//	unset    clear one layer's choice for one tool. Admin/owner.
//
// Backs onto the same routes the Permissions screen uses:
//
//	GET    /api/workspaces/{ws}/tool-policy  (Handler.Table, any member)
//	PUT    /api/workspaces/{ws}/tool-policy  (Handler.Set,   admin/owner)
//	DELETE /api/workspaces/{ws}/tool-policy  (Handler.Clear, admin/owner)
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

// tpEffective mirrors effectiveResponse in
// server/internal/cerebro/toolpolicy/handler.go.
type tpEffective struct {
	Setting   string `json:"setting"`
	DecidedBy string `json:"decided_by"`
	CappedBy  string `json:"capped_by"`
	Reason    string `json:"reason"`
}

// tpLayers mirrors layerSettings — one explicit choice per layer, nil (absent)
// when that layer carries no rule (Inherit).
type tpLayers struct {
	Workspace *string `json:"workspace"`
	Runtime   *string `json:"runtime"`
	Agent     *string `json:"agent"`
	Group     *string `json:"group"`
	User      *string `json:"user"`
	System    *string `json:"system"`
}

type tpGroupAttribution struct {
	Name  string `json:"name"`
	Owner string `json:"owner"`
}

// tpRow mirrors one row of the Table response.
type tpRow struct {
	ToolKey         string               `json:"tool_key"`
	ResourcePattern string               `json:"resource_pattern"`
	Title           string               `json:"title"`
	Category        string               `json:"category"`
	Source          string               `json:"source"`
	Layers          tpLayers             `json:"layers"`
	Effective       tpEffective          `json:"effective"`
	CappedByGroups  []tpGroupAttribution `json:"capped_by_groups"`
}

type tpTableResponse struct {
	Tools []tpRow `json:"tools"`
}

var tpValidLayers = map[string]bool{
	"workspace": true, "runtime": true, "agent": true,
	"group": true, "user": true, "system": true,
}

// "disable" (FIR-2351 follow-up, product decision 2026-07-06) is workspace-only
// — a hard, unopenable floor for one permission. See the workspace-layer check
// in runToolPolicySet, which mirrors toolpolicy.validSetting/Store.Set server-side.
var tpValidSettings = map[string]bool{
	"allow": true, "ask": true, "deny": true, "inherit": true, "disable": true,
}

func init() {
	parent := &cobra.Command{
		Use:     "tool-policy",
		Aliases: []string{"toolpolicy", "permissions", "perms"},
		Short:   "Understand and manage the unified per-tool permission policy",
		Long: "Inspect and change the unified permission engine that decides every " +
			"agent tool call across the layers workspace, runtime, agent, group, " +
			"user and system.\n\n" +
			"  explain  why an agent/member combination is allowed, asked or denied a tool\n" +
			"  list     the resolved verdict for every tool, one row each\n" +
			"  set      one layer's choice for one tool (allow|ask|deny|inherit|disable) — admin/owner\n" +
			"  unset    clear one layer's choice for one tool — admin/owner",
	}
	parent.GroupID = groupCore
	parent.AddCommand(newToolPolicyExplainCmd())
	parent.AddCommand(newToolPolicyListCmd())
	parent.AddCommand(newToolPolicySetCmd())
	parent.AddCommand(newToolPolicyUnsetCmd())
	rootCmd.AddCommand(parent)
}

// tpBasePath builds /api/workspaces/{ws}/tool-policy.
func tpBasePath(client *cli.APIClient) (string, error) {
	if client.WorkspaceID == "" {
		return "", fmt.Errorf("workspace_id is required (set --workspace-id or MULTICA_WORKSPACE_ID)")
	}
	return "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/tool-policy", nil
}

// tpScopeQuery turns the scope flags into the Table query string. agent_id is
// the most common; user_id adds the member dimension; runtime_id and repeated
// group_id complete the layer context.
func tpScopeQuery(cmd *cobra.Command) string {
	v := url.Values{}
	if s, _ := cmd.Flags().GetString("agent"); s != "" {
		v.Set("agent_id", s)
	}
	if s, _ := cmd.Flags().GetString("user"); s != "" {
		v.Set("user_id", s)
	}
	if s, _ := cmd.Flags().GetString("runtime"); s != "" {
		v.Set("runtime_id", s)
	}
	groups, _ := cmd.Flags().GetStringArray("group")
	for _, g := range groups {
		if g != "" {
			v.Add("group_id", g)
		}
	}
	if q := v.Encode(); q != "" {
		return "?" + q
	}
	return ""
}

func tpAddScopeFlags(cmd *cobra.Command) {
	cmd.Flags().String("agent", "", "Agent ID to resolve the policy for")
	cmd.Flags().String("user", "", "Member/user ID — adds the user layer (the member side of the combination)")
	cmd.Flags().String("runtime", "", "Runtime ID — adds the runtime layer")
	cmd.Flags().StringArray("group", nil, "Group ID to include (repeatable) — adds the group layer")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

// verdict renders a setting as a human verdict.
func tpVerdict(setting string) string {
	switch setting {
	case "allow":
		return "ALLOWED"
	case "ask":
		return "ASK (approval required)"
	case "deny":
		return "BLOCKED"
	case "inherit", "":
		return "inherit (no explicit rule)"
	default:
		return strings.ToUpper(setting)
	}
}

// tpLayerStack renders the non-inherited layers as "agent=deny, workspace=allow".
func tpLayerStack(l tpLayers) string {
	parts := []string{}
	add := func(name string, v *string) {
		if v != nil && *v != "" {
			parts = append(parts, name+"="+*v)
		}
	}
	add("workspace", l.Workspace)
	add("runtime", l.Runtime)
	add("agent", l.Agent)
	add("group", l.Group)
	add("user", l.User)
	add("system", l.System)
	if len(parts) == 0 {
		return "(all inherit / default)"
	}
	return strings.Join(parts, ", ")
}

func newToolPolicyExplainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Explain why an agent/member combination is allowed, asked or denied a tool",
		Long: "Resolves the unified policy for the given scope and prints, per tool, the " +
			"verdict (ALLOWED / ASK / BLOCKED), which layer decided or capped it, the " +
			"reason, and the full layer stack. Use --tool to focus on one tool. The " +
			"--user flag adds the member side of the combination.",
		RunE: runToolPolicyExplain,
	}
	tpAddScopeFlags(cmd)
	cmd.Flags().String("tool", "", "Limit to one tool key (e.g. tools:Bash, connection:customer-service-mcp)")
	return cmd
}

func runToolPolicyExplain(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	base, err := tpBasePath(client)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var res tpTableResponse
	if err := client.GetJSON(ctx, base+tpScopeQuery(cmd), &res); err != nil {
		return fmt.Errorf("read tool policy: %w", err)
	}

	only, _ := cmd.Flags().GetString("tool")
	rows := res.Tools
	if only != "" {
		filtered := rows[:0:0]
		for _, r := range rows {
			if r.ToolKey == only {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
		if len(rows) == 0 {
			return fmt.Errorf("tool %q not found in policy for this scope", only)
		}
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"tools": rows})
	}

	for i, r := range rows {
		if i > 0 {
			fmt.Fprintln(os.Stdout)
		}
		who := r.Effective.DecidedBy
		if r.Effective.CappedBy != "" {
			who = r.Effective.CappedBy + " (cap)"
		}
		fmt.Fprintf(os.Stdout, "%s: %s\n", r.ToolKey, tpVerdict(r.Effective.Setting))
		if who != "" {
			fmt.Fprintf(os.Stdout, "  decided by : %s layer\n", who)
		}
		if r.Effective.Reason != "" {
			fmt.Fprintf(os.Stdout, "  reason     : %s\n", r.Effective.Reason)
		}
		fmt.Fprintf(os.Stdout, "  layer stack: %s\n", tpLayerStack(r.Layers))
		if len(r.CappedByGroups) > 0 {
			names := make([]string, 0, len(r.CappedByGroups))
			for _, g := range r.CappedByGroups {
				names = append(names, g.Name)
			}
			fmt.Fprintf(os.Stdout, "  capped by groups: %s\n", strings.Join(names, ", "))
		}
	}
	return nil
}

func newToolPolicyListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every tool with its resolved verdict for a scope",
		RunE:  runToolPolicyList,
	}
	tpAddScopeFlags(cmd)
	return cmd
}

func runToolPolicyList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	base, err := tpBasePath(client)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var res tpTableResponse
	if err := client.GetJSON(ctx, base+tpScopeQuery(cmd), &res); err != nil {
		return fmt.Errorf("read tool policy: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, res)
	}

	headers := []string{"TOOL", "VERDICT", "DECIDED/CAPPED BY", "REASON"}
	rows := make([][]string, 0, len(res.Tools))
	for _, r := range res.Tools {
		who := r.Effective.DecidedBy
		if r.Effective.CappedBy != "" {
			who = r.Effective.CappedBy + " (cap)"
		}
		rows = append(rows, []string{r.ToolKey, tpVerdict(r.Effective.Setting), dash(who), r.Effective.Reason})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func newToolPolicySetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set one layer's choice for one tool (allow|ask|deny|inherit|disable) — admin/owner",
		Long: "Writes one rule into the unified policy: the chosen setting for one tool " +
			"at one layer for one subject. The subject id matches the layer — the " +
			"workspace id for the workspace layer, an agent id for the agent layer, a " +
			"group id for the group layer, a user id for the user layer, a runtime id " +
			"for the runtime layer, an autopilot id for the system layer. Admin/owner only.",
		RunE: runToolPolicySet,
	}
	cmd.Flags().String("tool", "", "Tool key (required), e.g. tools:Bash")
	cmd.Flags().String("layer", "", "Layer: workspace|runtime|agent|group|user|system (required)")
	cmd.Flags().String("subject", "", "Subject id for the layer (required)")
	cmd.Flags().String("setting", "", "Setting: allow|ask|deny|inherit|disable (required; disable is workspace-layer only)")
	cmd.Flags().String("resource", "", "Resource pattern (optional; empty = capability-wide)")
	cmd.Flags().String("output", "text", "Output format: text or json")
	_ = cmd.MarkFlagRequired("tool")
	_ = cmd.MarkFlagRequired("layer")
	_ = cmd.MarkFlagRequired("subject")
	_ = cmd.MarkFlagRequired("setting")
	return cmd
}

func runToolPolicySet(cmd *cobra.Command, _ []string) error {
	tool, _ := cmd.Flags().GetString("tool")
	layer, _ := cmd.Flags().GetString("layer")
	subject, _ := cmd.Flags().GetString("subject")
	setting, _ := cmd.Flags().GetString("setting")
	resource, _ := cmd.Flags().GetString("resource")

	layer = strings.ToLower(layer)
	setting = strings.ToLower(setting)
	if !tpValidLayers[layer] {
		return fmt.Errorf("invalid layer %q (want workspace|runtime|agent|group|user|system)", layer)
	}
	if !tpValidSettings[setting] {
		return fmt.Errorf("invalid setting %q (want allow|ask|deny|inherit|disable)", setting)
	}
	if setting == "disable" && layer != "workspace" {
		return fmt.Errorf("disable is only valid at the workspace layer (got layer %q)", layer)
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	base, err := tpBasePath(client)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{
		"tool_key":         tool,
		"layer":            layer,
		"subject_id":       subject,
		"resource_pattern": resource,
		"setting":          setting,
	}
	if err := client.PutJSON(ctx, base, body, nil); err != nil {
		return fmt.Errorf("set tool policy: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, body)
	}
	fmt.Fprintf(os.Stdout, "Set %s at %s layer (subject %s) → %s\n", tool, layer, subject, setting)
	return nil
}

func newToolPolicyUnsetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "unset",
		Aliases: []string{"clear"},
		Short:   "Clear one layer's choice for one tool (back to inherit) — admin/owner",
		RunE:    runToolPolicyUnset,
	}
	cmd.Flags().String("tool", "", "Tool key (required)")
	cmd.Flags().String("layer", "", "Layer: workspace|runtime|agent|group|user|system (required)")
	cmd.Flags().String("subject", "", "Subject id for the layer (required)")
	cmd.Flags().String("resource", "", "Resource pattern (optional; must match the rule)")
	cmd.Flags().String("output", "text", "Output format: text or json")
	_ = cmd.MarkFlagRequired("tool")
	_ = cmd.MarkFlagRequired("layer")
	_ = cmd.MarkFlagRequired("subject")
	return cmd
}

func runToolPolicyUnset(cmd *cobra.Command, _ []string) error {
	tool, _ := cmd.Flags().GetString("tool")
	layer, _ := cmd.Flags().GetString("layer")
	subject, _ := cmd.Flags().GetString("subject")
	resource, _ := cmd.Flags().GetString("resource")

	layer = strings.ToLower(layer)
	if !tpValidLayers[layer] {
		return fmt.Errorf("invalid layer %q (want workspace|runtime|agent|group|user|system)", layer)
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	base, err := tpBasePath(client)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	v := url.Values{}
	v.Set("tool_key", tool)
	v.Set("layer", layer)
	v.Set("subject_id", subject)
	if resource != "" {
		v.Set("resource_pattern", resource)
	}
	if err := client.DeleteJSON(ctx, base+"?"+v.Encode()); err != nil {
		return fmt.Errorf("clear tool policy: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"tool_key": tool, "layer": layer, "subject_id": subject, "cleared": true})
	}
	fmt.Fprintf(os.Stdout, "Cleared %s at %s layer (subject %s) → back to inherit\n", tool, layer, subject)
	return nil
}
