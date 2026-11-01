package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var proposalCmd = &cobra.Command{
	Use:   "proposal",
	Short: "File and list proposals for the boss's decision",
	Long: `A proposal is a squad's structured request for a decision by the boss.
It waits in a queue and reaches the boss through the Secretary's brief; it does
not notify anyone by itself.

The server limits how many proposals a squad may have: 3 pending, 3 filed per
rolling 7 days, 1 urgent per rolling 7 days, and 10 pending across the
workspace.`,
}

var proposalCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "File a proposal for the boss's decision",
	Long: `File a proposal for a squad and create the issue that backs it.

Every section is required: the boss reads a fixed shape (summary, evidence,
recommendation, options considered, cost and risk, ask).

  multica proposal create --squad <squad> --title "Pilot usage pricing" \
    --summary "..." --evidence <url> [--evidence <url> ...] \
    --recommendation "..." --option "..." [--option "..."] \
    --cost-risk "..." --ask "..." [--handoff-squad <squad>]

--urgent is only for a decision that cannot wait a day, and needs
--urgent-reason. It assigns the proposal to the boss at once.

If the server refuses because a limit is reached, its message is printed as is.`,
	Args: cobra.NoArgs,
	RunE: runProposalCreate,
}

var proposalListCmd = &cobra.Command{
	Use:   "list",
	Short: "List proposals, newest first",
	Args:  cobra.NoArgs,
	RunE:  runProposalList,
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// proposalInput is what the create flags carry, before squads are resolved to
// ids.
type proposalInput struct {
	Squad          string
	Title          string
	Summary        string
	Evidence       []string
	Recommendation string
	Options        []string
	CostRisk       string
	Ask            string
	HandoffSquad   string
	Urgent         bool
	UrgentReason   string
}

func trimmedAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// validate fails on the first missing piece, naming the flag, so the filing
// agent can fix the command without a round trip to the server.
func (in *proposalInput) validate() error {
	in.Squad = strings.TrimSpace(in.Squad)
	in.Title = strings.TrimSpace(in.Title)
	in.Summary = strings.TrimSpace(in.Summary)
	in.Recommendation = strings.TrimSpace(in.Recommendation)
	in.CostRisk = strings.TrimSpace(in.CostRisk)
	in.Ask = strings.TrimSpace(in.Ask)
	in.HandoffSquad = strings.TrimSpace(in.HandoffSquad)
	in.UrgentReason = strings.TrimSpace(in.UrgentReason)
	in.Evidence = trimmedAll(in.Evidence)
	in.Options = trimmedAll(in.Options)

	for _, f := range []struct{ flag, value string }{
		{"--squad", in.Squad},
		{"--title", in.Title},
		{"--summary", in.Summary},
		{"--recommendation", in.Recommendation},
		{"--cost-risk", in.CostRisk},
		{"--ask", in.Ask},
	} {
		if f.value == "" {
			return fmt.Errorf("%s is required", f.flag)
		}
	}
	if len(in.Evidence) == 0 {
		return fmt.Errorf("--evidence is required: give at least one link (repeat the flag for more)")
	}
	if len(in.Options) == 0 {
		return fmt.Errorf("--option is required: name at least one option you considered (repeat the flag for more)")
	}
	if in.Urgent && in.UrgentReason == "" {
		return fmt.Errorf("--urgent-reason is required with --urgent")
	}
	if !in.Urgent && in.UrgentReason != "" {
		return fmt.Errorf("--urgent-reason only applies with --urgent")
	}
	return nil
}

// body renders the request body. squadID and handoffSquadID are already
// resolved.
func (in proposalInput) body(squadID, handoffSquadID string) map[string]any {
	body := map[string]any{
		"squad_id":       squadID,
		"title":          in.Title,
		"summary":        in.Summary,
		"evidence":       in.Evidence,
		"recommendation": in.Recommendation,
		"options":        in.Options,
		"cost_risk":      in.CostRisk,
		"ask":            in.Ask,
	}
	if handoffSquadID != "" {
		body["handoff_squad_id"] = handoffSquadID
	}
	if in.Urgent {
		body["urgent"] = true
		body["urgent_reason"] = in.UrgentReason
	}
	return body
}

// resolveSquadRef accepts a squad id or an exact squad name.
func resolveSquadRef(ctx context.Context, client *cli.APIClient, ref string) (string, error) {
	if uuidPattern.MatchString(ref) {
		return ref, nil
	}
	var squads []map[string]any
	if err := client.GetJSON(ctx, "/api/squads", &squads); err != nil {
		return "", fmt.Errorf("look up squad %q: %w", ref, err)
	}
	var matches []string
	for _, s := range squads {
		if strings.EqualFold(strVal(s, "name"), ref) {
			matches = append(matches, strVal(s, "id"))
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no squad named %q; pass the squad id", ref)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("more than one squad is named %q; pass the squad id", ref)
	}
}

// proposalLimitError returns the server's proposal_limit message as the error
// to print, unchanged and without a status prefix, so an agent reads exactly
// what the server told it. Any other error is returned as is.
func proposalLimitError(err error) error {
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 409 {
		return err
	}
	var body struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(httpErr.Body), &body) != nil || body.Code != "proposal_limit" || body.Error == "" {
		return err
	}
	return cli.WithUserMessage(body.Error, err)
}

func runProposalCreate(cmd *cobra.Command, _ []string) error {
	in := proposalInput{}
	in.Squad, _ = cmd.Flags().GetString("squad")
	in.Title, _ = cmd.Flags().GetString("title")
	in.Summary, _ = cmd.Flags().GetString("summary")
	in.Evidence, _ = cmd.Flags().GetStringArray("evidence")
	in.Recommendation, _ = cmd.Flags().GetString("recommendation")
	in.Options, _ = cmd.Flags().GetStringArray("option")
	in.CostRisk, _ = cmd.Flags().GetString("cost-risk")
	in.Ask, _ = cmd.Flags().GetString("ask")
	in.HandoffSquad, _ = cmd.Flags().GetString("handoff-squad")
	in.Urgent, _ = cmd.Flags().GetBool("urgent")
	in.UrgentReason, _ = cmd.Flags().GetString("urgent-reason")
	if err := in.validate(); err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	squadID, err := resolveSquadRef(ctx, client, in.Squad)
	if err != nil {
		return err
	}
	handoffID := ""
	if in.HandoffSquad != "" {
		if handoffID, err = resolveSquadRef(ctx, client, in.HandoffSquad); err != nil {
			return fmt.Errorf("--handoff-squad: %w", err)
		}
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/proposals", in.body(squadID, handoffID), &result); err != nil {
		return proposalLimitError(err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Proposal filed: %s (issue %s, %s)\n", strVal(result, "id"), strVal(result, "issue_identifier"), proposalDeliveryNote(result))
	return nil
}

func proposalDeliveryNote(p map[string]any) string {
	if urgent, _ := p["urgent"].(bool); urgent {
		return "urgent: assigned to the boss"
	}
	return "queued for the next brief"
}

func runProposalList(cmd *cobra.Command, _ []string) error {
	query := url.Values{}
	if state, _ := cmd.Flags().GetString("state"); state != "" {
		switch state {
		case "pending", "approved", "rejected":
			query.Set("state", state)
		default:
			return fmt.Errorf("invalid --state %q; valid values: pending, approved, rejected", state)
		}
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if squad, _ := cmd.Flags().GetString("squad"); squad != "" {
		squadID, err := resolveSquadRef(ctx, client, squad)
		if err != nil {
			return err
		}
		query.Set("squad_id", squadID)
	}
	path := "/api/proposals"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}

	var proposals []map[string]any
	if err := client.GetJSON(ctx, path, &proposals); err != nil {
		return fmt.Errorf("list proposals: %w", err)
	}

	asJSON, _ := cmd.Flags().GetBool("json")
	if output, _ := cmd.Flags().GetString("output"); asJSON || output == "json" {
		if proposals == nil {
			proposals = []map[string]any{}
		}
		return cli.PrintJSON(os.Stdout, proposals)
	}
	if len(proposals) == 0 {
		fmt.Fprintln(os.Stderr, "No proposals found.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tISSUE\tSTATE\tURGENT\tTITLE")
	for _, p := range proposals {
		urgent := "-"
		if v, _ := p["urgent"].(bool); v {
			urgent = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			strVal(p, "id"), strVal(p, "issue_identifier"), strVal(p, "state"), urgent, strVal(p, "title"))
	}
	return w.Flush()
}

// The flag sets are built by functions so tests can run the real flags on a
// fresh command instead of sharing the package-level command's flag state.
func addProposalCreateFlags(cmd *cobra.Command) {
	cmd.Flags().String("squad", "", "Proposing squad (id or name) — required")
	cmd.Flags().String("title", "", "Short title of the proposal — required")
	cmd.Flags().String("summary", "", "What you propose and why now — required")
	cmd.Flags().StringArray("evidence", nil, "A link supporting the proposal; repeat for more — at least one required")
	cmd.Flags().String("recommendation", "", "What you recommend the boss decide — required")
	cmd.Flags().StringArray("option", nil, "An option you considered; repeat for more — at least one required")
	cmd.Flags().String("cost-risk", "", "Cost and risk of the recommendation — required")
	cmd.Flags().String("ask", "", "The decision you are asking for — required")
	cmd.Flags().String("handoff-squad", "", "Squad (id or name) that starts the work if the boss approves")
	cmd.Flags().Bool("urgent", false, "Only for a decision that cannot wait a day; assigns the boss at once")
	cmd.Flags().String("urgent-reason", "", "Why this cannot wait a day — required with --urgent")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

func addProposalListFlags(cmd *cobra.Command) {
	cmd.Flags().String("state", "", "Only proposals in this state: pending, approved or rejected")
	cmd.Flags().String("squad", "", "Only proposals from this squad (id or name)")
	cmd.Flags().Bool("json", false, "Print JSON (same as --output json)")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

func init() {
	addProposalCreateFlags(proposalCreateCmd)
	addProposalListFlags(proposalListCmd)

	proposalCmd.AddCommand(proposalCreateCmd)
	proposalCmd.AddCommand(proposalListCmd)
}
