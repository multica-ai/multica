package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var agentRuntimeConfigCmd = &cobra.Command{
	Use:   "runtime-config",
	Short: "Manage your personal runtime configuration for a smart-runtime agent",
}

var agentRuntimeConfigGetCmd = &cobra.Command{
	Use:   "get <agent-id>",
	Short: "Get your personal override, recommendation strategy, and effective runtime config",
	Args:  exactArgs(1),
	RunE:  runAgentRuntimeConfigGet,
}

var agentRuntimeConfigSetCmd = &cobra.Command{
	Use:   "set <agent-id>",
	Short: "Set your personal runtime override without changing the Agent recommendation",
	Args:  exactArgs(1),
	RunE:  runAgentRuntimeConfigSet,
}

var agentRuntimeConfigUseRecommendedCmd = &cobra.Command{
	Use:   "use-recommended <agent-id>",
	Short: "Remove your personal override and use the Agent recommendation",
	Args:  exactArgs(1),
	RunE:  runAgentRuntimeConfigUseRecommended,
}

func init() {
	agentCmd.AddCommand(agentRuntimeConfigCmd)
	agentRuntimeConfigCmd.AddCommand(agentRuntimeConfigGetCmd)
	agentRuntimeConfigCmd.AddCommand(agentRuntimeConfigSetCmd)
	agentRuntimeConfigCmd.AddCommand(agentRuntimeConfigUseRecommendedCmd)

	agentRuntimeConfigGetCmd.Flags().String("output", "json", "Output format: json or table")

	agentRuntimeConfigSetCmd.Flags().String("runtime-id", "", "Personal runtime ID (required)")
	agentRuntimeConfigSetCmd.Flags().String("model", "", "Model override; empty uses the runtime default")
	agentRuntimeConfigSetCmd.Flags().String("thinking-level", "", "Reasoning/effort override; empty uses the runtime default")
	agentRuntimeConfigSetCmd.Flags().String("service-tier", "", "Codex service tier; empty inherits local Codex configuration")
	agentRuntimeConfigSetCmd.Flags().Int32("max-concurrent-tasks", 6, "Personal maximum concurrent runs (1-50)")
	agentRuntimeConfigSetCmd.Flags().String("runtime-config", "", "Runtime config as JSON; omit to preserve masked values when staying on the same runtime")
	agentRuntimeConfigSetCmd.Flags().String("output", "json", "Output format: json or table")

	agentRuntimeConfigUseRecommendedCmd.Flags().String("output", "json", "Output format: json or table")
}

func runAgentRuntimeConfigGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var summary map[string]any
	if err := client.GetJSON(ctx, "/api/agents/"+args[0]+"/runtime-config/current", &summary); err != nil {
		return fmt.Errorf("get personal runtime config: %w", err)
	}
	return printAgentRuntimeConfig(cmd, summary)
}

func runAgentRuntimeConfigSet(cmd *cobra.Command, args []string) error {
	runtimeID, _ := cmd.Flags().GetString("runtime-id")
	if runtimeID == "" {
		return fmt.Errorf("--runtime-id is required")
	}
	maxConcurrent, _ := cmd.Flags().GetInt32("max-concurrent-tasks")
	if err := validateAgentMaxConcurrentTasksFlag(maxConcurrent); err != nil {
		return err
	}
	body := map[string]any{
		"strategy":             "custom",
		"runtime_id":           runtimeID,
		"model":                flagString(cmd, "model"),
		"thinking_level":       flagString(cmd, "thinking-level"),
		"service_tier":         flagString(cmd, "service-tier"),
		"max_concurrent_tasks": maxConcurrent,
	}
	if cmd.Flags().Changed("runtime-config") {
		raw := flagString(cmd, "runtime-config")
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return fmt.Errorf("--runtime-config must be valid JSON: %w", err)
		}
		body["runtime_config"] = value
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var summary map[string]any
	if err := client.PutJSON(ctx, "/api/agents/"+args[0]+"/runtime-config/current", body, &summary); err != nil {
		return fmt.Errorf("set personal runtime config: %w", err)
	}
	return printAgentRuntimeConfig(cmd, summary)
}

func runAgentRuntimeConfigUseRecommended(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var summary map[string]any
	if err := client.PutJSON(ctx, "/api/agents/"+args[0]+"/runtime-config/current", map[string]any{
		"strategy": "recommended",
	}, &summary); err != nil {
		return fmt.Errorf("use recommended runtime config: %w", err)
	}
	return printAgentRuntimeConfig(cmd, summary)
}

func printAgentRuntimeConfig(cmd *cobra.Command, summary map[string]any) error {
	if flagString(cmd, "output") == "json" {
		return cli.PrintJSON(os.Stdout, summary)
	}
	current, _ := summary["current"].(map[string]any)
	cli.PrintTable(os.Stdout,
		[]string{"STRATEGY", "SOURCE", "RUNTIME_ID", "MODEL", "THINKING", "SERVICE_TIER", "MAX_RUNS", "PERSISTED", "STATUS"},
		[][]string{{
			strVal(current, "strategy"), strVal(current, "source"), strVal(current, "runtime_id"),
			strVal(current, "model"), strVal(current, "thinking_level"), strVal(current, "service_tier"),
			strVal(current, "max_concurrent_tasks"), strVal(current, "persisted"), strVal(current, "resolution_status"),
		}},
	)
	return nil
}
