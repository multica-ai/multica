// CEREBRO-PATCH(cerebro-agent-tool-grant-write): FIR-1496 cerebro-only file —
// `multica agent tool-grant add|remove` CLI. Write path for the runtime tool
// grant layer that `agent tool-access` diagnoses.
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// toolGrantResult mirrors agentToolGrantResult in
// server/internal/handler/agent_tool_grant_cerebro.go.
type toolGrantResult struct {
	AgentID    string   `json:"agent_id"`
	RuntimeID  string   `json:"runtime_id"`
	Action     string   `json:"action"`
	TargetType string   `json:"target_type"`
	TargetID   string   `json:"target_id"`
	Tools      []string `json:"tools"`
	Count      int      `json:"count"`
}

type toolGrantRequest struct {
	Tool     string `json:"tool,omitempty"`
	AllTools bool   `json:"all_tools,omitempty"`
	GroupID  string `json:"group_id,omitempty"`
	User     string `json:"user,omitempty"`
}

func init() {
	parent := &cobra.Command{
		Use:   "tool-grant",
		Short: "Grant or revoke an agent's runtime tools for a group or user",
		Long: "Writes the runtime tool grant layer — the cascade that `agent tool-access` " +
			"diagnoses. Grant a group (or a single user) access to one tool or all tools " +
			"on the agent's runtime. Admin-only. Use `agent tool-access` afterwards to " +
			"verify the effect.",
	}
	parent.AddCommand(newToolGrantWriteCmd("add", "grant"))
	parent.AddCommand(newToolGrantWriteCmd("remove", "revoke"))
	agentCmd.AddCommand(parent)
}

func newToolGrantWriteCmd(use, action string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use + " <agent-id>",
		Short: fmt.Sprintf("%s tool access for a group or user", action),
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runToolGrantWrite(cmd, args, action)
		},
	}
	cmd.Flags().String("group", "", "Group ID to grant/revoke (mutually exclusive with --user)")
	cmd.Flags().String("user", "", "User UUID or email to grant/revoke (mutually exclusive with --group)")
	cmd.Flags().String("tool", "", "Tool name to grant/revoke")
	cmd.Flags().Bool("all-tools", false, "Apply to every tool on the agent's runtime")
	cmd.Flags().String("output", "table", "Output format: table or json")
	return cmd
}

func runToolGrantWrite(cmd *cobra.Command, args []string, action string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	group, _ := cmd.Flags().GetString("group")
	user, _ := cmd.Flags().GetString("user")
	tool, _ := cmd.Flags().GetString("tool")
	allTools, _ := cmd.Flags().GetBool("all-tools")

	if (group == "") == (user == "") {
		return fmt.Errorf("specify exactly one of --group or --user")
	}
	if allTools == (tool != "") {
		return fmt.Errorf("specify exactly one of --tool or --all-tools")
	}

	body := toolGrantRequest{Tool: tool, AllTools: allTools, GroupID: group, User: user}
	path := fmt.Sprintf("/api/agents/%s/tool-grants", url.PathEscape(args[0]))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, _ := cmd.Flags().GetString("output")

	// grant returns the resolved tool list (the DELETE client helper does not
	// decode a body, so revoke confirms from the request inputs instead).
	if action == "grant" {
		var res toolGrantResult
		if err := client.PostJSON(ctx, path, body, &res); err != nil {
			return fmt.Errorf("grant tool: %w", err)
		}
		if output == "json" {
			return cli.PrintJSON(os.Stdout, res)
		}
		fmt.Fprintf(os.Stdout, "Granted %s access on runtime %s (%d tool(s)):\n",
			res.TargetType, res.RuntimeID, res.Count)
		rows := make([][]string, 0, len(res.Tools))
		for _, t := range res.Tools {
			rows = append(rows, []string{t})
		}
		cli.PrintTable(os.Stdout, []string{"TOOL"}, rows)
		return nil
	}

	if err := client.DeleteJSONWithBody(ctx, path, body); err != nil {
		return fmt.Errorf("revoke tool: %w", err)
	}
	target := "group " + group
	if user != "" {
		target = "user " + user
	}
	scope := "tool " + tool
	if allTools {
		scope = "all tools"
	}
	fmt.Fprintf(os.Stdout, "Revoked %s for %s on agent %s.\n", scope, target, args[0])
	return nil
}
