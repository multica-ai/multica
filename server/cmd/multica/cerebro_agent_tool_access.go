// CEREBRO-PATCH(cerebro-agent-tool-access-diagnostic): FIR-1480 cerebro-only file — `multica agent tool-access` CLI.
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

// toolAccessEntry / toolAccessResult mirror the GET /api/agents/{id}/tool-access
// response (server/internal/handler/agent_tool_access_cerebro.go).
type toolAccessEntry struct {
	Tool        string `json:"tool"`
	Source      string `json:"source"`
	McpServer   string `json:"mcp_server"`
	Callable    bool   `json:"callable"`
	Reason      string `json:"reason"`
	GroupGrants string `json:"group_grants"`
}

type toolAccessResult struct {
	AgentID   string            `json:"agent_id"`
	RuntimeID string            `json:"runtime_id"`
	User      string            `json:"user"`
	UserID    string            `json:"user_id"`
	Tools     []toolAccessEntry `json:"tools"`
}

func init() {
	cmd := &cobra.Command{
		Use:   "tool-access <agent-id>",
		Short: "Show which tools a user can use through an agent, and why",
		Long: "Resolves the effective tool access for a given user on a given agent's " +
			"runtime and prints, per tool, whether it is callable and the reason " +
			"(admin bypass / direct grant / group grant / not granted / disabled). " +
			"Admin-only. The --user flag accepts a user UUID or email.",
		Args: exactArgs(1),
		RunE: runAgentToolAccess,
	}
	cmd.Flags().String("user", "", "User to check access for (UUID or email)")
	cmd.Flags().String("output", "table", "Output format: table or json")
	_ = cmd.MarkFlagRequired("user")
	agentCmd.AddCommand(cmd)
}

func runAgentToolAccess(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	user, _ := cmd.Flags().GetString("user")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := fmt.Sprintf("/api/agents/%s/tool-access?user=%s",
		url.PathEscape(args[0]), url.QueryEscape(user))

	var res toolAccessResult
	if err := client.GetJSON(ctx, path, &res); err != nil {
		return fmt.Errorf("get tool access: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, res)
	}

	headers := []string{"TOOL", "CALLABLE", "REASON", "SOURCE"}
	rows := make([][]string, 0, len(res.Tools))
	for _, t := range res.Tools {
		mark := "yes"
		if !t.Callable {
			mark = "NO"
		}
		src := t.Source
		if t.McpServer != "" {
			src = t.Source + ":" + t.McpServer
		}
		rows = append(rows, []string{t.Tool, mark, t.Reason, src})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}
