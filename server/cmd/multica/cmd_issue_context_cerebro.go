package main

// CEREBRO-PATCH(issue-context-cli): FIR-2384 step 2 — `multica issue context
// <id>` fetches the issue, its recent comment thread, the workspace members,
// and the issue's labels in ONE platform call (GET /api/issues/{id}/context),
// the agent-facing half of the bundled_read cost saving. Replaces the separate
// `issue get` + `comment list` (+ `workspace members` + `issue label list`)
// round-trips an agent makes at the start of a run.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

func runIssueContext(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/context", &result); err != nil {
		return fmt.Errorf("get issue context: %w", err)
	}

	return cli.PrintJSON(os.Stdout, result)
}
