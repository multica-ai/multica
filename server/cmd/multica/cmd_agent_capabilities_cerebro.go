package main

// CEREBRO-PATCH(cli-agent-capabilities): TECH-3642 `multica agent capabilities
// <id>` — the CLI surface of the unified per-agent capabilities card. It is a
// thin client over GET /api/agents/{id}/capabilities: `--output json` prints
// the endpoint payload verbatim so an agent reading via the CLI sees byte-for-
// byte what the MCP tool and the dashboard show. Lives in its own file with its
// own init() so the upstream cmd_agent.go is untouched.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var agentCapabilitiesCmd = &cobra.Command{
	Use:   "capabilities <id>",
	Short: "Show an agent's capabilities (skills, tools, credentials, limits)",
	Args:  exactArgs(1),
	RunE:  runAgentCapabilities,
}

func init() {
	agentCmd.AddCommand(agentCapabilitiesCmd)
	agentCapabilitiesCmd.Flags().String("output", "json", "Output format: table or json")
}

func runAgentCapabilities(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var caps map[string]any
	if err := client.GetJSON(ctx, "/api/agents/"+args[0]+"/capabilities", &caps); err != nil {
		return fmt.Errorf("get agent capabilities: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, caps)
	}

	// Table view: the card is nested, so summarise each section as a count.
	enabledTools := 0
	for _, t := range asSlice(caps["tools"]) {
		if m, ok := t.(map[string]any); ok {
			if enabled, _ := m["enabled"].(bool); enabled {
				enabledTools++
			}
		}
	}
	headers := []string{"ID", "NAME", "MODEL", "SKILLS", "TOOLS_ENABLED", "CREDENTIALS"}
	rows := [][]string{{
		strVal(caps, "agent_id"),
		strVal(caps, "name"),
		strVal(caps, "model"),
		fmt.Sprintf("%d", len(asSlice(caps["skills"]))),
		fmt.Sprintf("%d", enabledTools),
		fmt.Sprintf("%d", len(asSlice(caps["credentials"]))),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// asSlice coerces a decoded JSON value to a slice, returning nil when absent.
func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}
