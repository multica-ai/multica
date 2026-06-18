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

// effectiveTool mirrors toolaccess.EffectiveTool — the full read-only 5-layer
// preview returned by GET /api/runtimes/{id}/tools/effective. Only the fields
// the CLI renders are kept.
type effectiveTool struct {
	Descriptor struct {
		ToolKey string `json:"tool_key"`
	} `json:"descriptor"`
	Inventory struct {
		Enabled bool `json:"enabled"`
	} `json:"inventory"`
	Policy struct {
		Effective string `json:"effective"`
		Reason    string `json:"reason"`
	} `json:"policy"`
	RuntimeGrant struct {
		Effective string `json:"effective"`
		Reason    string `json:"reason"`
	} `json:"runtime_grant"`
	Protocol struct {
		Effective string `json:"effective"`
	} `json:"protocol"`
	Credential struct {
		Effective string `json:"effective"`
	} `json:"credential"`
	ExposureEffective struct {
		Effective bool   `json:"effective"`
		Reason    string `json:"reason"`
	} `json:"exposure_effective"`
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
	cmd.Flags().Bool("full", false, "Show the full 5-layer effective decision per tool (inventory · policy · grant · protocol · credential)")
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

	// --full upgrades from the grant-cascade diagnostic to the complete 5-layer
	// read-only preview, so an operator sees policy / protocol / credential
	// blocks too — not only the runtime grant layer.
	if full, _ := cmd.Flags().GetBool("full"); full {
		return runAgentToolAccessFull(ctx, client, res, output)
	}

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

// runAgentToolAccessFull renders the complete 5-layer effective preview by
// calling GET /api/runtimes/{id}/tools/effective with the agent + user context
// resolved by the diagnostic endpoint.
func runAgentToolAccessFull(ctx context.Context, client *cli.APIClient, base toolAccessResult, output string) error {
	if base.RuntimeID == "" {
		return fmt.Errorf("agent has no runtime; cannot compute the full 5-layer view")
	}
	path := fmt.Sprintf("/api/runtimes/%s/tools/effective?agent_id=%s&user_id=%s",
		url.PathEscape(base.RuntimeID), url.QueryEscape(base.AgentID), url.QueryEscape(base.UserID))

	var tools []effectiveTool
	if err := client.GetJSON(ctx, path, &tools); err != nil {
		return fmt.Errorf("get effective tool access: %w", err)
	}

	if output == "json" {
		return cli.PrintJSON(os.Stdout, tools)
	}

	headers := []string{"TOOL", "EXPOSED", "INVENTORY", "POLICY", "GRANT", "PROTOCOL", "CREDENTIAL", "WHY"}
	rows := make([][]string, 0, len(tools))
	for _, t := range tools {
		exposed := "yes"
		if !t.ExposureEffective.Effective {
			exposed = "NO"
		}
		inv := "on"
		if !t.Inventory.Enabled {
			inv = "off"
		}
		rows = append(rows, []string{
			t.Descriptor.ToolKey,
			exposed,
			inv,
			dash(t.Policy.Effective),
			dash(t.RuntimeGrant.Effective),
			dash(t.Protocol.Effective),
			dash(t.Credential.Effective),
			t.ExposureEffective.Reason,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
