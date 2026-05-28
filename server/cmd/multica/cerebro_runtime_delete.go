// CEREBRO-PATCH(cerebro-runtime-delete-cli): FIR-2453 cerebro-only file —
// expose the existing `DELETE /api/runtimes/:id` handler as
// `multica runtime delete <id>` so owners can prune stale rows
// (e.g. gateway runtimes left behind by daemons that no longer register).
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var runtimeDeleteCmd = &cobra.Command{
	Use:   "delete <runtime-id>",
	Short: "Delete a runtime (refuses if active agents are bound — use the UI cascade dialog for that)",
	Args:  exactArgs(1),
	RunE:  runRuntimeDelete,
}

func init() {
	runtimeCmd.AddCommand(runtimeDeleteCmd)
}

func runRuntimeDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	runtimeID := args[0]
	if err := client.DeleteJSON(ctx, "/api/runtimes/"+url.PathEscape(runtimeID)); err != nil {
		return fmt.Errorf("delete runtime: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Runtime %s deleted.\n", runtimeID)
	return nil
}
