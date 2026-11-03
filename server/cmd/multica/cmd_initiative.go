package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// `multica initiative` reads the pipeline and moves initiatives (spec 3.1). The
// server decides who may do what; the CLI only sends the request.
var initiativeCmd = &cobra.Command{
	Use:   "initiative",
	Short: "List, show and move initiatives through the pipeline",
}

var initiativeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List initiatives",
	Long:  "List initiatives, newest first. --status, --owner and --pipeline narrow the list.",
	Args:  cobra.NoArgs,
	RunE:  runInitiativeList,
}

var initiativeShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show an initiative with its artifacts, timeline and gates",
	Args:  cobra.ExactArgs(1),
	RunE:  runInitiativeShow,
}

var initiativeAttestCmd = &cobra.Command{
	Use:   "attest <id>",
	Short: "Attest the current stage as the owning squad (advances when its artifacts are in place)",
	Args:  cobra.ExactArgs(1),
	RunE:  runInitiativeMove("/attest", "attested"),
}

var initiativeAdvanceCmd = &cobra.Command{
	Use:   "advance <id>",
	Short: "Advance past the current stage when its exit allows it",
	Args:  cobra.ExactArgs(1),
	RunE:  runInitiativeMove("/advance", "advanced"),
}

var initiativeShelveCmd = &cobra.Command{
	Use:   "shelve <id>",
	Short: "Pause an initiative (owners and admins only)",
	Args:  cobra.ExactArgs(1),
	RunE:  runInitiativeMove("/shelve", "shelved"),
}

func init() {
	initiativeListCmd.Flags().String("status", "", "Only initiatives in this status: active, shelved, operating or closed")
	initiativeListCmd.Flags().String("owner", "", "Only initiatives the squad (id) owns")
	initiativeListCmd.Flags().String("pipeline", "", "Only initiatives in this pipeline (id)")
	initiativeListCmd.Flags().String("output", "table", "Output format: table or json")
	initiativeShowCmd.Flags().String("output", "table", "Output format: table or json")
	initiativeAttestCmd.Flags().String("note", "", "A note for the timeline")
	initiativeAdvanceCmd.Flags().String("note", "", "A note for the timeline")
	initiativeShelveCmd.Flags().String("note", "", "Why the initiative is paused")
	for _, c := range []*cobra.Command{initiativeAttestCmd, initiativeAdvanceCmd, initiativeShelveCmd} {
		c.Flags().String("output", "table", "Output format: table or json")
	}

	initiativeCmd.AddCommand(initiativeListCmd)
	initiativeCmd.AddCommand(initiativeShowCmd)
	initiativeCmd.AddCommand(initiativeAttestCmd)
	initiativeCmd.AddCommand(initiativeAdvanceCmd)
	initiativeCmd.AddCommand(initiativeShelveCmd)
}

func runInitiativeList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	q := url.Values{}
	for flag, param := range map[string]string{"status": "status", "owner": "owner_squad_id", "pipeline": "pipeline_id"} {
		if v, _ := cmd.Flags().GetString(flag); v != "" {
			q.Set(param, v)
		}
	}
	path := "/api/initiatives"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out struct {
		Initiatives []map[string]any `json:"initiatives"`
	}
	if err := client.GetJSON(ctx, path, &out); err != nil {
		return fmt.Errorf("list initiatives: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, out)
	}
	if len(out.Initiatives) == 0 {
		fmt.Fprintln(os.Stderr, "No initiatives found.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTITLE\tSTAGE\tSTATUS\tDAYS\tSLA\tARTIFACTS")
	for _, i := range out.Initiatives {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\t%s\t%v/%v\n",
			strVal(i, "identifier"), strVal(i, "title"), strVal(i, "stage_key"), strVal(i, "status"),
			i["days_in_stage"], strVal(i, "sla_state"), i["artifacts_present"], i["artifacts_required"])
	}
	return w.Flush()
}

func runInitiativeShow(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var detail map[string]any
	if err := client.GetJSON(ctx, "/api/initiatives/"+url.PathEscape(args[0]), &detail); err != nil {
		return fmt.Errorf("show initiative: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, detail)
	}
	ini, _ := detail["initiative"].(map[string]any)
	fmt.Printf("%s  %s\n", strVal(ini, "identifier"), strVal(ini, "title"))
	fmt.Printf("Stage: %s   Status: %s   Days in stage: %v   SLA: %s\n",
		strVal(ini, "stage_key"), strVal(ini, "status"), ini["days_in_stage"], strVal(ini, "sla_state"))
	if thesis := strVal(ini, "thesis"); thesis != "" {
		fmt.Printf("Thesis: %s\n", thesis)
	}
	if artifacts, _ := detail["artifacts"].([]any); len(artifacts) > 0 {
		fmt.Println("\nArtifacts:")
		for _, raw := range artifacts {
			a, _ := raw.(map[string]any)
			state := "missing"
			switch {
			case a["waived"] == true:
				state = "waived"
			case a["knowledge_link_id"] != nil:
				state = "present"
			}
			fmt.Printf("  %-22s %-12s required=%v  %s\n", strVal(a, "kind"), strVal(a, "stage_key"), a["required"], state)
		}
	}
	if events, _ := detail["events"].([]any); len(events) > 0 {
		fmt.Println("\nTimeline:")
		for _, raw := range events {
			e, _ := raw.(map[string]any)
			fmt.Printf("  %s  %-12s %s -> %s  %s\n", strVal(e, "created_at"), strVal(e, "kind"),
				strVal(e, "from_stage"), strVal(e, "to_stage"), strVal(e, "note"))
		}
	}
	return nil
}

// runInitiativeMove returns the handler of a transition that takes an optional
// note and prints the initiative's new stage.
func runInitiativeMove(suffix, past string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		ctx, cancel := cli.APIContext(context.Background())
		defer cancel()

		note, _ := cmd.Flags().GetString("note")
		var out map[string]any
		if err := client.PostJSON(ctx, "/api/initiatives/"+url.PathEscape(args[0])+suffix, map[string]any{"note": note}, &out); err != nil {
			return fmt.Errorf("%s initiative: %w", past, err)
		}
		if output, _ := cmd.Flags().GetString("output"); output == "json" {
			return cli.PrintJSON(os.Stdout, out)
		}
		fmt.Printf("%s %s: now at %s (%s)\n", strVal(out, "identifier"), past, strVal(out, "stage_key"), strVal(out, "status"))
		return nil
	}
}
