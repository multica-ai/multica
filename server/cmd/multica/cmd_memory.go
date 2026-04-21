package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Manage workspace memory entries",
}

var memoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List workspace memory entries (index only — no content)",
	RunE:  runMemoryList,
}

var memoryGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get the full content of a memory entry",
	Args:  exactArgs(1),
	RunE:  runMemoryGet,
}

var memoryAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new workspace memory entry",
	RunE:  runMemoryAdd,
}

var memoryUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a workspace memory entry",
	Args:  exactArgs(1),
	RunE:  runMemoryUpdate,
}

var memoryDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a workspace memory entry",
	Args:  exactArgs(1),
	RunE:  runMemoryDelete,
}

func init() {
	memoryCmd.AddCommand(memoryListCmd)
	memoryCmd.AddCommand(memoryGetCmd)
	memoryCmd.AddCommand(memoryAddCmd)
	memoryCmd.AddCommand(memoryUpdateCmd)
	memoryCmd.AddCommand(memoryDeleteCmd)

	memoryListCmd.Flags().String("output", "table", "Output format: table or json")
	memoryListCmd.Flags().String("project-id", "", "Filter by project ID (omit for global memory)")

	memoryGetCmd.Flags().String("output", "json", "Output format: table or json")

	memoryAddCmd.Flags().String("name", "", "Memory entry name (required)")
	memoryAddCmd.Flags().String("description", "", "One-line description of this entry")
	memoryAddCmd.Flags().String("content", "", "Full content of the memory entry (required)")
	memoryAddCmd.Flags().String("project-id", "", "Scope this memory to a specific project (omit for global)")
	memoryAddCmd.Flags().String("output", "json", "Output format: table or json")

	memoryUpdateCmd.Flags().String("name", "", "New name")
	memoryUpdateCmd.Flags().String("description", "", "New description")
	memoryUpdateCmd.Flags().String("content", "", "New content")
	memoryUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	memoryDeleteCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
}

func runMemoryList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/memory"
	if projectID, _ := cmd.Flags().GetString("project-id"); projectID != "" {
		path += "?project_id=" + projectID
	}

	var entries []map[string]any
	if err := client.GetJSON(ctx, path, &entries); err != nil {
		return fmt.Errorf("list memory: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, entries)
	}

	if len(entries) == 0 {
		fmt.Println("No memory entries found.")
		return nil
	}
	fmt.Printf("%-36s  %-30s  %s\n", "ID", "Name", "Description")
	fmt.Printf("%-36s  %-30s  %s\n", "------------------------------------", "------------------------------", "-----------")
	for _, e := range entries {
		id, _ := e["id"].(string)
		name, _ := e["name"].(string)
		desc, _ := e["description"].(string)
		if len(name) > 30 {
			name = name[:27] + "..."
		}
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		fmt.Printf("%-36s  %-30s  %s\n", id, name, desc)
	}
	return nil
}

func runMemoryGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var entry map[string]any
	if err := client.GetJSON(ctx, "/api/memory/"+args[0], &entry); err != nil {
		return fmt.Errorf("get memory: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, entry)
	}

	// Default: print content directly (useful for agents)
	if content, ok := entry["content"].(string); ok {
		fmt.Print(content)
		if len(content) > 0 && content[len(content)-1] != '\n' {
			fmt.Println()
		}
	}
	return nil
}

func runMemoryAdd(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	content, _ := cmd.Flags().GetString("content")
	if content == "" {
		return fmt.Errorf("--content is required")
	}
	description, _ := cmd.Flags().GetString("description")

	body := map[string]any{
		"name":        name,
		"description": description,
		"content":     content,
	}
	if projectID, _ := cmd.Flags().GetString("project-id"); projectID != "" {
		body["project_id"] = projectID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/memory", body, &result); err != nil {
		return fmt.Errorf("add memory: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	id, _ := result["id"].(string)
	fmt.Printf("Memory entry created: %s\n", id)
	return nil
}

func runMemoryUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	body := map[string]any{}
	if v, _ := cmd.Flags().GetString("name"); v != "" {
		body["name"] = v
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if v, _ := cmd.Flags().GetString("content"); v != "" {
		body["content"] = v
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/memory/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update memory: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Println("Memory entry updated.")
	return nil
}

func runMemoryDelete(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		fmt.Printf("Delete memory entry %s? [y/N] ", args[0])
		var confirm string
		fmt.Scanln(&confirm)
		if confirm != "y" && confirm != "Y" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/memory/"+args[0]); err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	fmt.Println("Memory entry deleted.")
	return nil
}
