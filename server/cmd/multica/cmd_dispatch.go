package main

import (
	"github.com/spf13/cobra"
)

var dispatchCmd = &cobra.Command{
	Use:   "dispatch",
	Short: "Manage cross-squad dispatch contracts",
	Long: `Create, list, view, and cancel dispatch contracts for cross-squad
agent collaboration. A dispatch contract defines a callback loop
that automatically notifies the dispatching party when a delegated
sub-issue reaches a terminal state.`,
}

var dispatchCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new cross-squad dispatch (issue + contract)",
	Long: `Creates a sub-issue assigned to another squad and atomically
attaches a dispatch contract that defines the callback configuration.

This is equivalent to 'multica issue create' + establishing a
callback loop that automatically notifies the dispatcher when the
sub-issue is completed.`,
	RunE: runDispatchCreate,
}

var dispatchListCmd = &cobra.Command{
	Use:   "list",
	Short: "List dispatch contracts",
	RunE:  runDispatchList,
}

var dispatchShowCmd = &cobra.Command{
	Use:   "show <contract-id>",
	Short: "Show dispatch contract details",
	Args:  exactArgs(1),
	RunE:  runDispatchShow,
}

var dispatchCancelCmd = &cobra.Command{
	Use:   "cancel <contract-id>",
	Short: "Cancel a dispatch contract",
	Args:  exactArgs(1),
	RunE:  runDispatchCancel,
}

func init() {
	dispatchCmd.GroupID = groupCore

	dispatchCmd.AddCommand(dispatchCreateCmd)
	dispatchCmd.AddCommand(dispatchListCmd)
	dispatchCmd.AddCommand(dispatchShowCmd)
	dispatchCmd.AddCommand(dispatchCancelCmd)

	// Create flags
	dispatchCreateCmd.Flags().String("title", "", "Title of the dispatched issue")
	dispatchCreateCmd.Flags().String("to-squad", "", "Target squad ID or name")
	dispatchCreateCmd.Flags().String("from-issue", "", "Source issue ID (the issue making the dispatch)")
	dispatchCreateCmd.Flags().String("callback-target", "", "Where to post callback (defaults to from-issue)")
	dispatchCreateCmd.Flags().String("description", "", "Issue description")
	dispatchCreateCmd.Flags().String("description-stdin", "", "Read description from stdin")
	dispatchCreateCmd.Flags().String("description-file", "", "Read description from file")
	dispatchCreateCmd.Flags().String("status", "todo", "Initial status")
	dispatchCreateCmd.Flags().String("priority", "medium", "Priority (urgent/high/medium/low/none)")
	dispatchCreateCmd.MarkFlagRequired("title")
	dispatchCreateCmd.MarkFlagRequired("to-squad")
	dispatchCreateCmd.MarkFlagRequired("from-issue")

	// List flags
	dispatchListCmd.Flags().String("status", "", "Filter by contract status (pending/triggered/fulfilled/cancelled)")

	// Cancel flags
	dispatchCancelCmd.Flags().String("reason", "", "Reason for cancellation")

	rootCmd.AddCommand(dispatchCmd)
}

func runDispatchCreate(cmd *cobra.Command, args []string) error {
	// Placeholder — will be fully implemented in Phase 3 (DispatchContract table)
	cmd.Println("⚠️  'multica dispatch create' requires the DispatchContract table (coming in Phase 3).")
	cmd.Println("For now, use 'multica issue create' with --parent and set metadata manually.")
	return nil
}

func runDispatchList(cmd *cobra.Command, args []string) error {
	cmd.Println("⚠️  'multica dispatch list' requires the DispatchContract table (coming in Phase 3).")
	return nil
}

func runDispatchShow(cmd *cobra.Command, args []string) error {
	cmd.Println("⚠️  'multica dispatch show' requires the DispatchContract table (coming in Phase 3).")
	return nil
}

func runDispatchCancel(cmd *cobra.Command, args []string) error {
	cmd.Println("⚠️  'multica dispatch cancel' requires the DispatchContract table (coming in Phase 3).")
	return nil
}
