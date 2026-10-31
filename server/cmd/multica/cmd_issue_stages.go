package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

func init() {
	stages := &cobra.Command{
		Use:   "stages <issue-id>",
		Short: "Show or set the squad the server assigns this issue's stages to",
		Long: "With --squad the server assigns the unassigned sub-issues of the first open stage to the squad as soon as\n" +
			"every earlier stage is closed. A stage that closed with a cancelled sub-issue halts dispatch and wakes\n" +
			"this issue's assignee instead. --off stops it. Requires the stage_dispatch_v1 feature flag on the server.",
		Args: exactArgs(1),
		RunE: runIssueStages,
	}
	stages.Flags().String("squad", "", "Squad name or ID to dispatch the stages to")
	stages.Flags().Bool("off", false, "Stop dispatching the stages")
	stages.Flags().String("output", "json", "Output format: json")
	issueCmd.AddCommand(stages)
}

func runIssueStages(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	ref, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return err
	}
	path := "/api/issues/" + url.PathEscape(ref.ID) + "/stage-dispatch"
	squad, _ := cmd.Flags().GetString("squad")
	off, _ := cmd.Flags().GetBool("off")
	var out any
	switch {
	case squad != "" && off:
		return fmt.Errorf("--squad and --off are mutually exclusive")
	case off:
		err = client.PutJSON(ctx, path, map[string]any{"squad_id": nil}, &out)
	case squad != "":
		id, rerr := resolveSquadID(ctx, client, squad)
		if rerr != nil {
			return rerr
		}
		err = client.PutJSON(ctx, path, map[string]any{"squad_id": id}, &out)
	default:
		err = client.GetJSON(ctx, path, &out)
	}
	if err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}

// resolveSquadID accepts a squad ID or its exact name (case-insensitive).
func resolveSquadID(ctx context.Context, client *cli.APIClient, input string) (string, error) {
	var squads []map[string]any
	if err := client.GetJSON(ctx, "/api/squads", &squads); err != nil {
		return "", fmt.Errorf("list squads: %w", err)
	}
	var found []string
	for _, s := range squads {
		if id := strVal(s, "id"); id == input {
			return id, nil
		}
		if strings.EqualFold(strVal(s, "name"), input) {
			found = append(found, strVal(s, "id"))
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return "", fmt.Errorf("squad %q not found", input)
	}
	return "", fmt.Errorf("squad name %q is ambiguous; use the squad id", input)
}
