package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dwickyfp/wallts/server/internal/cli"
)

var failoverGroupCmd = &cobra.Command{
	Use:   "failover-group",
	Short: "Manage runtime failover groups",
}

var failoverGroupListCmd = &cobra.Command{
	Use:   "list",
	Short: "List failover groups in the workspace",
	RunE:  runFailoverGroupList,
}

var failoverGroupCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new failover group",
	Args:  exactArgs(1),
	RunE:  runFailoverGroupCreate,
}

var failoverGroupDeleteCmd = &cobra.Command{
	Use:   "delete <group-id>",
	Short: "Delete a failover group",
	Args:  exactArgs(1),
	RunE:  runFailoverGroupDelete,
}

var failoverGroupAddRuntimeCmd = &cobra.Command{
	Use:   "add-runtime <group-id> <runtime-id>",
	Short: "Add a runtime to a failover group",
	Args:  exactArgs(2),
	RunE:  runFailoverGroupAddRuntime,
}

var failoverGroupRemoveRuntimeCmd = &cobra.Command{
	Use:   "remove-runtime <runtime-id>",
	Short: "Remove a runtime from its failover group",
	Args:  exactArgs(1),
	RunE:  runFailoverGroupRemoveRuntime,
}

func init() {
	failoverGroupCmd.AddCommand(failoverGroupListCmd)
	failoverGroupCmd.AddCommand(failoverGroupCreateCmd)
	failoverGroupCmd.AddCommand(failoverGroupDeleteCmd)
	failoverGroupCmd.AddCommand(failoverGroupAddRuntimeCmd)
	failoverGroupCmd.AddCommand(failoverGroupRemoveRuntimeCmd)

	failoverGroupListCmd.Flags().String("output", "table", "Output format: table or json")
	failoverGroupCreateCmd.Flags().String("strategy", "priority", "Failover strategy: priority, round-robin, or least-loaded")
	failoverGroupCreateCmd.Flags().String("output", "json", "Output format: table or json")
	failoverGroupDeleteCmd.Flags().String("output", "json", "Output format: table or json")
	failoverGroupAddRuntimeCmd.Flags().Int("priority", 0, "Priority for the runtime in this group (higher = preferred)")
	failoverGroupAddRuntimeCmd.Flags().String("output", "json", "Output format: table or json")
	failoverGroupRemoveRuntimeCmd.Flags().String("output", "json", "Output format: table or json")
}

func runFailoverGroupList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var groups []map[string]any
	if err := client.GetJSON(ctx, "/api/runtimes/failover-groups", &groups); err != nil {
		return fmt.Errorf("list failover groups: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, groups)
	}
	headers := []string{"ID", "NAME", "STRATEGY", "CREATED_AT"}
	rows := make([][]string, 0, len(groups))
	for _, g := range groups {
		rows = append(rows, []string{
			strVal(g, "id"),
			strVal(g, "name"),
			strVal(g, "strategy"),
			strVal(g, "created_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runFailoverGroupCreate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	strategy, _ := cmd.Flags().GetString("strategy")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	body := map[string]any{
		"name":     args[0],
		"strategy": strategy,
	}
	var group map[string]any
	if err := client.PostJSON(ctx, "/api/runtimes/failover-groups", body, &group); err != nil {
		return fmt.Errorf("create failover group: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, group)
	}
	fmt.Printf("Created failover group: %s (%s)\n", strVal(group, "name"), strVal(group, "id"))
	return nil
}

func runFailoverGroupDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.DeleteJSON(ctx, "/api/runtimes/failover-groups/"+args[0]); err != nil {
		return fmt.Errorf("delete failover group: %w", err)
	}
	fmt.Printf("Deleted failover group: %s\n", args[0])
	return nil
}

func runFailoverGroupAddRuntime(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	priority, _ := cmd.Flags().GetInt("priority")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	body := map[string]any{
		"failover_group_id": args[0],
		"priority":          priority,
	}
	var result map[string]any
	if err := client.PatchJSON(ctx, "/api/runtimes/"+args[1]+"/priority", body, &result); err != nil {
		return fmt.Errorf("add runtime to group: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Added runtime %s to group %s with priority %d\n", args[1], args[0], priority)
	return nil
}

func runFailoverGroupRemoveRuntime(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	body := map[string]any{
		"failover_group_id": "",
	}
	var result map[string]any
	if err := client.PatchJSON(ctx, "/api/runtimes/"+args[0]+"/priority", body, &result); err != nil {
		return fmt.Errorf("remove runtime from group: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Removed runtime %s from its failover group\n", args[0])
	return nil
}
