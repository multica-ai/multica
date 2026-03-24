package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check server health",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, err := client.HealthCheck(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Server unreachable: %v\n", err)
		return err
	}

	fmt.Fprintf(os.Stdout, "Server: %s\n", client.BaseURL)
	fmt.Fprintf(os.Stdout, "Status: %s\n", body)
	return nil
}
