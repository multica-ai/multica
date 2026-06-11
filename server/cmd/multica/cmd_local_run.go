package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var localRunCmd = &cobra.Command{
	Use:   "local-run",
	Short: "Manage local run sync state",
}

var localRunSyncPendingCmd = &cobra.Command{
	Use:   "sync-pending",
	Short: "Sync pending local run messages from the local spool",
	RunE:  runLocalRunSyncPending,
}

func init() {
	localRunCmd.AddCommand(localRunSyncPendingCmd)
}

func runLocalRunSyncPending(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	spool, err := newLocalRunMessageSpoolForCommand(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sent, remaining, err := syncPendingLocalRunSpool(ctx, client, spool, "")
	if err != nil && remaining > 0 {
		fmt.Fprintf(os.Stderr, "Synced %d pending local run messages; %d remain queued: %v\n", sent, remaining, err)
		return nil
	}
	fmt.Fprintf(os.Stdout, "Synced %d pending local run messages; %d remain queued.\n", sent, remaining)
	return err
}
