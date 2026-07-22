package main

import (
	"net/url"

	"github.com/spf13/cobra"
)

var commandCmd = &cobra.Command{Use: "command", Short: "Manage reusable workflow commands"}

func init() {
	commands := []*cobra.Command{
		{Use: "list", Short: "List commands", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return evalGet(cmd, "/api/cerebro/commands") }},
		{Use: "get <command-id>", Short: "Get a command", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			return evalGet(cmd, "/api/cerebro/commands/"+url.PathEscape(args[0]))
		}},
		{Use: "create", Short: "Create a command from JSON", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return evalWrite(cmd, "POST", "/api/cerebro/commands") }},
		{Use: "update <command-id>", Short: "Replace a command from JSON", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			return evalWrite(cmd, "PUT", "/api/cerebro/commands/"+url.PathEscape(args[0]))
		}},
		{Use: "delete <command-id>", Short: "Delete a command", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			return evalDelete(cmd, "/api/cerebro/commands/"+url.PathEscape(args[0]), args[0])
		}},
	}
	for _, command := range commands {
		command.Flags().String("output", "json", "Output format: json")
	}
	for _, command := range []*cobra.Command{commands[2], commands[3]} {
		command.Flags().String("file", "", "Read the complete JSON document from a file")
		command.Flags().Bool("stdin", false, "Read the complete JSON document from stdin")
	}
	commandCmd.AddCommand(commands...)
	rootCmd.AddCommand(commandCmd)
}
