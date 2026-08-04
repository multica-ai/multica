package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Work with event hooks (automation rules)",
}

var hookListCmd = &cobra.Command{
	Use:   "list",
	Short: "List hooks in the workspace",
	RunE:  runHookList,
}

var hookGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a hook and its active revision",
	Args:  exactArgs(1),
	RunE:  runHookGet,
}

var hookCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a hook from a JSON spec file",
	RunE:  runHookCreate,
}

var hookUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a hook from a JSON spec file (appends a new revision)",
	Args:  exactArgs(1),
	RunE:  runHookUpdate,
}

var hookEnableCmd = &cobra.Command{
	Use:   "enable <id>",
	Short: "Enable a hook",
	Args:  exactArgs(1),
	RunE:  runHookEnable,
}

var hookDisableCmd = &cobra.Command{
	Use:   "disable <id>",
	Short: "Disable a hook",
	Args:  exactArgs(1),
	RunE:  runHookDisable,
}

var hookDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Archive (soft-delete) a hook",
	Args:  exactArgs(1),
	RunE:  runHookDelete,
}

var hookExecutionsCmd = &cobra.Command{
	Use:   "executions <id>",
	Short: "List a hook's recent execution trace",
	Args:  exactArgs(1),
	RunE:  runHookExecutions,
}

var hookDryRunCmd = &cobra.Command{
	Use:   "dry-run",
	Short: "Evaluate a candidate hook spec against a historical event (read-only)",
	RunE:  runHookDryRun,
}

var hookExplainCmd = &cobra.Command{
	Use:   "explain <hook-id>",
	Short: "Explain whether a stored hook would fire for a historical event (read-only)",
	Args:  exactArgs(1),
	RunE:  runHookExplain,
}

func init() {
	hookCmd.AddCommand(hookListCmd)
	hookCmd.AddCommand(hookGetCmd)
	hookCmd.AddCommand(hookCreateCmd)
	hookCmd.AddCommand(hookUpdateCmd)
	hookCmd.AddCommand(hookEnableCmd)
	hookCmd.AddCommand(hookDisableCmd)
	hookCmd.AddCommand(hookDeleteCmd)
	hookCmd.AddCommand(hookExecutionsCmd)
	hookCmd.AddCommand(hookDryRunCmd)
	hookCmd.AddCommand(hookExplainCmd)

	hookListCmd.Flags().String("output", "table", "Output format: table or json")
	hookGetCmd.Flags().String("output", "json", "Output format: table or json")
	hookExecutionsCmd.Flags().String("output", "table", "Output format: table or json")

	hookCreateCmd.Flags().String("file", "", "Path to a JSON hook spec file (required)")
	_ = hookCreateCmd.MarkFlagRequired("file")
	hookUpdateCmd.Flags().String("file", "", "Path to a JSON hook spec file (required)")
	_ = hookUpdateCmd.MarkFlagRequired("file")

	hookDisableCmd.Flags().String("reason", "", "Optional reason recorded on the hook")

	hookDryRunCmd.Flags().String("file", "", "Path to a JSON hook spec file (required)")
	_ = hookDryRunCmd.MarkFlagRequired("file")
	hookDryRunCmd.Flags().String("event", "", "Historical event id to evaluate against (required)")
	_ = hookDryRunCmd.MarkFlagRequired("event")

	hookExplainCmd.Flags().String("event", "", "Historical event id to evaluate against (required)")
	_ = hookExplainCmd.MarkFlagRequired("event")
	hookExplainCmd.Flags().Int("revision", 0, "Explain a specific revision number (default: active revision)")
}

// readHookSpecFile loads and validates that the spec file is well-formed JSON,
// then returns it for the request body. The server performs full typed validation.
func readHookSpecFile(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read spec file: %w", err)
	}
	var body any
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("spec file is not valid JSON: %w", err)
	}
	return body, nil
}

func runHookList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var hooks []map[string]any
	if err := client.GetJSON(ctx, "/api/hooks", &hooks); err != nil {
		return fmt.Errorf("list hooks: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, hooks)
	}
	headers := []string{"ID", "NAME", "EVENT", "FIRE", "ENABLED"}
	rows := make([][]string, 0, len(hooks))
	for _, hook := range hooks {
		rows = append(rows, []string{
			strVal(hook, "id"),
			strVal(hook, "name"),
			hookRevisionField(hook, "event"),
			hookRevisionField(hook, "fire_mode"),
			fmt.Sprintf("%v", hook["enabled"]),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runHookGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var hook map[string]any
	if err := client.GetJSON(ctx, "/api/hooks/"+args[0], &hook); err != nil {
		return fmt.Errorf("get hook: %w", err)
	}
	return cli.PrintJSON(os.Stdout, hook)
}

func runHookCreate(cmd *cobra.Command, _ []string) error {
	path, _ := cmd.Flags().GetString("file")
	body, err := readHookSpecFile(path)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/hooks", body, &result); err != nil {
		return fmt.Errorf("create hook: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runHookUpdate(cmd *cobra.Command, args []string) error {
	path, _ := cmd.Flags().GetString("file")
	body, err := readHookSpecFile(path)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PatchJSON(ctx, "/api/hooks/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update hook: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runHookEnable(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/hooks/"+args[0]+"/enable", map[string]any{}, &result); err != nil {
		return fmt.Errorf("enable hook: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runHookDisable(cmd *cobra.Command, args []string) error {
	reason, _ := cmd.Flags().GetString("reason")
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/hooks/"+args[0]+"/disable", map[string]any{"reason": reason}, &result); err != nil {
		return fmt.Errorf("disable hook: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runHookDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/hooks/"+args[0]); err != nil {
		return fmt.Errorf("delete hook: %w", err)
	}
	fmt.Printf("Hook archived: %s\n", args[0])
	return nil
}

func runHookExecutions(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var execs []map[string]any
	if err := client.GetJSON(ctx, "/api/hooks/"+args[0]+"/executions", &execs); err != nil {
		return fmt.Errorf("list hook executions: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, execs)
	}
	headers := []string{"ID", "STATUS", "SKIP_REASON", "EVENT_ID", "CREATED_AT"}
	rows := make([][]string, 0, len(execs))
	for _, e := range execs {
		rows = append(rows, []string{
			strVal(e, "id"),
			strVal(e, "status"),
			strVal(e, "skip_reason"),
			strVal(e, "event_id"),
			strVal(e, "created_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runHookDryRun(cmd *cobra.Command, _ []string) error {
	path, _ := cmd.Flags().GetString("file")
	event, _ := cmd.Flags().GetString("event")
	spec, err := readHookSpecFile(path)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/hooks/dry-run", map[string]any{"hook": spec, "event_id": event}, &result); err != nil {
		return fmt.Errorf("dry-run hook: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runHookExplain(cmd *cobra.Command, args []string) error {
	event, _ := cmd.Flags().GetString("event")
	revision, _ := cmd.Flags().GetInt("revision")
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"hook_id": args[0], "event_id": event}
	if revision > 0 {
		body["revision"] = revision
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/hooks/explain", body, &result); err != nil {
		return fmt.Errorf("explain hook: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

// hookRevisionField reads a field out of the nested "revision" object for the
// list table view.
func hookRevisionField(hook map[string]any, key string) string {
	rev, ok := hook["revision"].(map[string]any)
	if !ok {
		return ""
	}
	return strVal(rev, key)
}
