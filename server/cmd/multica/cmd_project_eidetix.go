package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var projectEidetixCmd = &cobra.Command{
	Use:   "eidetix",
	Short: "Manage a project's Eidetix shared-context binding (workspace owner/admin only)",
}

var projectEidetixSetCmd = &cobra.Command{
	Use:   "set <project-id>",
	Short: "Bind a project to an Eidetix graph by Bearer token (upsert)",
	Args:  exactArgs(1),
	RunE:  runProjectEidetixSet,
}

var projectEidetixShowCmd = &cobra.Command{
	Use:   "show <project-id>",
	Short: "Show a project's Eidetix binding status (never prints the token)",
	Args:  exactArgs(1),
	RunE:  runProjectEidetixShow,
}

var projectEidetixClearCmd = &cobra.Command{
	Use:   "clear <project-id>",
	Short: "Remove a project's Eidetix binding",
	Args:  exactArgs(1),
	RunE:  runProjectEidetixClear,
}

var projectEidetixEnableCmd = &cobra.Command{
	Use:   "enable <project-id>",
	Short: "Enable Eidetix for a project (keeps the stored token)",
	Args:  exactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runProjectEidetixToggle(cmd, args, true) },
}

var projectEidetixDisableCmd = &cobra.Command{
	Use:   "disable <project-id>",
	Short: "Disable Eidetix for a project (keeps the stored token)",
	Args:  exactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runProjectEidetixToggle(cmd, args, false) },
}

func init() {
	projectCmd.AddCommand(projectEidetixCmd)
	projectEidetixCmd.AddCommand(projectEidetixSetCmd)
	projectEidetixCmd.AddCommand(projectEidetixShowCmd)
	projectEidetixCmd.AddCommand(projectEidetixClearCmd)
	projectEidetixCmd.AddCommand(projectEidetixEnableCmd)
	projectEidetixCmd.AddCommand(projectEidetixDisableCmd)

	registerEidetixSetFlags(projectEidetixSetCmd)
	projectEidetixShowCmd.Flags().String("output", "table", "Output format: table or json")
	projectEidetixEnableCmd.Flags().String("output", "table", "Output format: table or json")
	projectEidetixDisableCmd.Flags().String("output", "table", "Output format: table or json")
}

// registerEidetixSetFlags is shared with the test constructor so the flag set
// stays in one place.
func registerEidetixSetFlags(cmd *cobra.Command) {
	cmd.Flags().String("token", "", "Eidetix Bearer token (prefer --token-stdin so it never lands in shell history)")
	cmd.Flags().Bool("token-stdin", false, "Read the token from stdin")
	cmd.Flags().String("token-file", "", "Read the token from a file path")
	cmd.Flags().String("endpoint", "", "Override the Eidetix endpoint URL (defaults to the partner SSE URL)")
	cmd.Flags().String("label", "", "Human label for the graph (e.g. Marketing). Never the token.")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

// resolveEidetixToken reads the token from exactly one of --token,
// --token-stdin, or --token-file. Mirrors resolveMcpConfig's secure-input
// pattern so the secret never has to appear on the command line.
func resolveEidetixToken(cmd *cobra.Command) (string, error) {
	inline := cmd.Flags().Changed("token")
	fromStdin, _ := cmd.Flags().GetBool("token-stdin")
	filePath, _ := cmd.Flags().GetString("token-file")
	fromFile := cmd.Flags().Changed("token-file")

	count := 0
	if inline {
		count++
	}
	if fromStdin {
		count++
	}
	if fromFile {
		count++
	}
	switch {
	case count == 0:
		return "", fmt.Errorf("a token is required: pass --token-stdin (recommended), --token-file, or --token")
	case count > 1:
		return "", fmt.Errorf("--token, --token-stdin, and --token-file are mutually exclusive; pick one")
	}

	var raw string
	switch {
	case inline:
		raw, _ = cmd.Flags().GetString("token")
	case fromStdin:
		buf, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", fmt.Errorf("read --token-stdin: %w", err)
		}
		raw = string(buf)
	case fromFile:
		buf, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("read --token-file: %w", err)
		}
		raw = string(buf)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("token is empty")
	}
	return raw, nil
}

func runProjectEidetixSet(cmd *cobra.Command, args []string) error {
	token, err := resolveEidetixToken(cmd)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	body := map[string]any{"token": token}
	if v, _ := cmd.Flags().GetString("endpoint"); strings.TrimSpace(v) != "" {
		body["endpoint_url"] = strings.TrimSpace(v)
	}
	if v, _ := cmd.Flags().GetString("label"); strings.TrimSpace(v) != "" {
		body["graph_label"] = strings.TrimSpace(v)
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/projects/"+projectRef.ID+"/eidetix", body, &result); err != nil {
		return fmt.Errorf("set eidetix config: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Eidetix bound to project %s.\n", projectRef.Display)
	return printEidetixResult(cmd, result)
}

func runProjectEidetixShow(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	var result map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectRef.ID+"/eidetix", &result); err != nil {
		return fmt.Errorf("show eidetix config: %w", err)
	}
	return printEidetixResult(cmd, result)
}

func runProjectEidetixClear(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	if err := client.DeleteJSON(ctx, "/api/projects/"+projectRef.ID+"/eidetix"); err != nil {
		return fmt.Errorf("clear eidetix config: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Eidetix binding removed from project %s.\n", projectRef.Display)
	return nil
}

func runProjectEidetixToggle(cmd *cobra.Command, args []string, enabled bool) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	var result map[string]any
	if err := client.PatchJSON(ctx, "/api/projects/"+projectRef.ID+"/eidetix", map[string]any{"enabled": enabled}, &result); err != nil {
		return fmt.Errorf("toggle eidetix config: %w", err)
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Fprintf(os.Stderr, "Eidetix %s for project %s.\n", state, projectRef.Display)
	return printEidetixResult(cmd, result)
}

func printEidetixResult(cmd *cobra.Command, result map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	headers := []string{"CONFIGURED", "ENABLED", "ENDPOINT", "LABEL"}
	rows := [][]string{{
		fmt.Sprintf("%v", result["configured"]),
		fmt.Sprintf("%v", result["enabled"]),
		strVal(result, "endpoint_url"),
		strVal(result, "graph_label"),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}
