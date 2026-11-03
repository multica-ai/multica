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
	Short: "File, revise and list proposals for the boss's decision",
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

var proposalReviseCmd = &cobra.Command{
	Use:   "revise <proposal-id>",
	Short: "Resubmit a proposal the boss sent back for changes",
	Long: `The boss asked for changes to a proposal, so it is back with your squad. Find
it with "multica proposal list --state revising", then resubmit the whole body.
Every section is required, as for a new proposal:

  multica proposal revise <proposal-id> --title "..." \
    --summary "..." --evidence <url> [--evidence <url> ...] \
    --recommendation "..." --option "..." [--option "..."] \
    --cost-risk "..." --ask "..."

The revised proposal goes back into the boss's queue with its revision number
raised. A revision is not a new proposal, so the weekly limit does not count it,
but the pending limits do.`,
	Args: cobra.ExactArgs(1),
	RunE: runProposalRevise,
}

var proposalListCmd = &cobra.Command{
	Use:   "list",
	Short: "List proposals, newest first",
	Args:  cobra.NoArgs,
	RunE:  runProposalList,
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// proposalStates are the values --state accepts, in the order the help lists them.
var proposalStates = []string{"pending", "approved", "rejected", "revising", "expired"}

// proposalInput is what the create and revise flags carry, before squads are
// resolved to ids.
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

// normalizeBody trims the fields that make up the proposal body.
func (in *proposalInput) normalizeBody() {
	in.Title = strings.TrimSpace(in.Title)
	in.Summary = strings.TrimSpace(in.Summary)
	in.Recommendation = strings.TrimSpace(in.Recommendation)
	in.CostRisk = strings.TrimSpace(in.CostRisk)
	in.Ask = strings.TrimSpace(in.Ask)
	in.Evidence = trimmedAll(in.Evidence)
	in.Options = trimmedAll(in.Options)
}

// validateBody fails on the first missing section, naming the flag, so the
// filing agent can fix the command without a round trip to the server.
func (in *proposalInput) validateBody() error {
	in.normalizeBody()
	for _, f := range []struct{ flag, value string }{
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
	return nil
}

// validate is validateBody plus what only a new filing has: the squad, and the
// urgency rules.
func (in *proposalInput) validate() error {
	in.Squad = strings.TrimSpace(in.Squad)
	in.HandoffSquad = strings.TrimSpace(in.HandoffSquad)
	in.UrgentReason = strings.TrimSpace(in.UrgentReason)
	if in.Squad == "" {
		return fmt.Errorf("--squad is required")
	}
	if err := in.validateBody(); err != nil {
		return err
	}
	if in.Urgent && in.UrgentReason == "" {
		return fmt.Errorf("--urgent-reason is required with --urgent")
	}
	if !in.Urgent && in.UrgentReason != "" {
		return fmt.Errorf("--urgent-reason only applies with --urgent")
	}
	return nil
}

// bodyFields is the proposal body both create and revise send.
func (in proposalInput) bodyFields() map[string]any {
	return map[string]any{
		"title":          in.Title,
		"summary":        in.Summary,
		"evidence":       in.Evidence,
		"recommendation": in.Recommendation,
		"options":        in.Options,
		"cost_risk":      in.CostRisk,
		"ask":            in.Ask,
	}
}

// body renders the create request body. squadID and handoffSquadID are already
// resolved.
func (in proposalInput) body(squadID, handoffSquadID string) map[string]any {
	body := in.bodyFields()
	body["squad_id"] = squadID
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

// readProposalBody reads the body flags shared by create and revise.
func readProposalBody(cmd *cobra.Command) proposalInput {
	in := proposalInput{}
	in.Title, _ = cmd.Flags().GetString("title")
	in.Summary, _ = cmd.Flags().GetString("summary")
	in.Evidence, _ = cmd.Flags().GetStringArray("evidence")
	in.Recommendation, _ = cmd.Flags().GetString("recommendation")
	in.Options, _ = cmd.Flags().GetStringArray("option")
	in.CostRisk, _ = cmd.Flags().GetString("cost-risk")
	in.Ask, _ = cmd.Flags().GetString("ask")
	return in
}

func runProposalCreate(cmd *cobra.Command, _ []string) error {
	in := readProposalBody(cmd)
	in.Squad, _ = cmd.Flags().GetString("squad")
	in.HandoffSquad, _ = cmd.Flags().GetString("handoff-squad")
	in.Urgent, _ = cmd.Flags().GetBool("urgent")
	in.UrgentReason, _ = cmd.Flags().GetString("urgent-reason")
	kind, payload, err := proposalKindFromFlags(cmd)
	if err != nil {
		return err
	}
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
	body := in.body(squadID, handoffID)
	if kind != "" && kind != "decision" {
		body["kind"] = kind
		body["payload"] = payload
	}
	if err := client.PostJSON(ctx, "/api/proposals", body, &result); err != nil {
		return proposalLimitError(err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Proposal filed: %s (issue %s, %s)\n", strVal(result, "id"), strVal(result, "issue_identifier"), proposalDeliveryNote(result))
	if related := proposalRelatedLines(result); len(related) > 0 {
		fmt.Println("Related earlier work (cite it in the summary where it bears on the decision):")
		for _, line := range related {
			fmt.Println("  " + line)
		}
	}
	return nil
}

// proposalRelatedLines describes the related entries the server returned, one
// line each. Proposals show their identifier and state; decisions show their
// title.
func proposalRelatedLines(p map[string]any) []string {
	items, _ := p["related"].([]any)
	var lines []string
	for _, item := range items {
		r, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch strVal(r, "kind") {
		case "decision":
			lines = append(lines, fmt.Sprintf("decision: %s", strVal(r, "title")))
		default:
			lines = append(lines, fmt.Sprintf("proposal %s (%s): %s", strVal(r, "issue_identifier"), strVal(r, "state"), strVal(r, "title")))
		}
	}
	return lines
}

func proposalDeliveryNote(p map[string]any) string {
	if urgent, _ := p["urgent"].(bool); urgent {
		return "urgent: assigned to the boss"
	}
	return "queued for the next brief"
}

func runProposalRevise(cmd *cobra.Command, args []string) error {
	id := strings.TrimSpace(args[0])
	if !uuidPattern.MatchString(id) {
		return fmt.Errorf("proposal id must be a UUID; find it with `multica proposal list --state revising`")
	}
	in := readProposalBody(cmd)
	if err := in.validateBody(); err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/proposals/"+url.PathEscape(id)+"/revision", in.bodyFields(), &result); err != nil {
		return proposalLimitError(err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Proposal revised: %s (revision %v, %s)\n", strVal(result, "id"), result["revision"], "back in the boss's queue")
	return nil
}

func runProposalList(cmd *cobra.Command, _ []string) error {
	query := url.Values{}
	if state, _ := cmd.Flags().GetString("state"); state != "" {
		if !containsString(proposalStates, state) {
			return fmt.Errorf("invalid --state %q; valid values: %s", state, strings.Join(proposalStates, ", "))
		}
		query.Set("state", state)
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

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// addProposalBodyFlags registers the flags of a proposal body, which create and
// revise share.
func addProposalBodyFlags(cmd *cobra.Command) {
	cmd.Flags().String("title", "", "Short title of the proposal — required")
	cmd.Flags().String("summary", "", "What you propose and why now — required")
	cmd.Flags().StringArray("evidence", nil, "A link supporting the proposal; repeat for more — at least one required")
	cmd.Flags().String("recommendation", "", "What you recommend the boss decide — required")
	cmd.Flags().StringArray("option", nil, "An option you considered; repeat for more — at least one required")
	cmd.Flags().String("cost-risk", "", "Cost and risk of the recommendation — required")
	cmd.Flags().String("ask", "", "The decision you are asking for — required")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

// The flag sets are built by functions so tests can run the real flags on a
// fresh command instead of sharing the package-level command's flag state.
func addProposalCreateFlags(cmd *cobra.Command) {
	cmd.Flags().String("squad", "", "Proposing squad (id or name) — required")
	addProposalBodyFlags(cmd)
	cmd.Flags().String("handoff-squad", "", "Squad (id or name) that starts the work if the boss approves")
	cmd.Flags().String("kind", "decision", "Proposal kind: decision (the default) or opportunity. Stage gates are filed by the system")
	cmd.Flags().String("thesis", "", "Opportunity: why this is worth doing — required with --kind opportunity")
	cmd.Flags().String("market", "", "Opportunity: the market it is in")
	cmd.Flags().StringArray("evidence-doc", nil, "Opportunity: a knowledge document id that supports it; repeat for more")
	cmd.Flags().String("target-stage", "", "Opportunity: the pipeline stage it starts at (default validate)")
	cmd.Flags().Bool("urgent", false, "Only for a decision that cannot wait a day; assigns the boss at once")
	cmd.Flags().String("urgent-reason", "", "Why this cannot wait a day — required with --urgent")
}

func addProposalReviseFlags(cmd *cobra.Command) {
	addProposalBodyFlags(cmd)
}

func addProposalListFlags(cmd *cobra.Command) {
	cmd.Flags().String("state", "", "Only proposals in this state: pending, approved, rejected, revising or expired")
	cmd.Flags().String("squad", "", "Only proposals from this squad (id or name)")
	cmd.Flags().Bool("json", false, "Print JSON (same as --output json)")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

func init() {
	addProposalCreateFlags(proposalCreateCmd)
	addProposalReviseFlags(proposalReviseCmd)
	addProposalListFlags(proposalListCmd)
	proposalDecideCmd.Flags().String("decision", "", "approve or reject — required")
	proposalDecideCmd.Flags().String("note", "", "Why the proposal is rejected; optional on approval")
	proposalDecideCmd.Flags().String("outcome", "", "At an operate review gate: continue, scale or sunset. Required to approve one")

	proposalCmd.AddCommand(proposalCreateCmd)
	proposalCmd.AddCommand(proposalReviseCmd)
	proposalCmd.AddCommand(proposalListCmd)
	proposalCmd.AddCommand(proposalDecideCmd)
}

// proposalDecideCmd settles a proposal the boss can decide. The server refuses an
// agent, so this runs for a person with an owner or admin login.
var proposalDecideCmd = &cobra.Command{
	Use:   "decide <proposal-id>",
	Short: "Approve or reject a proposal; an operate review takes an outcome",
	Long: `Approve or reject a pending proposal. An operate-stage review gate (at 30 and
90 days after launch) is approved with its outcome:

  multica proposal decide <proposal-id> --decision approve --outcome continue
  multica proposal decide <proposal-id> --decision approve --outcome scale
  multica proposal decide <proposal-id> --decision approve --outcome sunset

Continue keeps the initiative operating; scale starts a follow-on at Define;
sunset closes it. Other gates and proposals take no outcome.`,
	Args: cobra.ExactArgs(1),
	RunE: runProposalDecide,
}

// proposalDecideOutcomes are the outcomes of an operate review, as the server accepts them.
var proposalDecideOutcomes = []string{"continue", "scale", "sunset"}

func runProposalDecide(cmd *cobra.Command, args []string) error {
	id := strings.TrimSpace(args[0])
	if !uuidPattern.MatchString(id) {
		return fmt.Errorf("proposal id must be a UUID; find it with `multica proposal list --state pending`")
	}
	decision, _ := cmd.Flags().GetString("decision")
	if decision != "approve" && decision != "reject" {
		return fmt.Errorf("--decision must be approve or reject")
	}
	outcome, _ := cmd.Flags().GetString("outcome")
	if outcome != "" && !containsString(proposalDecideOutcomes, outcome) {
		return fmt.Errorf("invalid --outcome %q; valid values: %s", outcome, strings.Join(proposalDecideOutcomes, ", "))
	}
	note, _ := cmd.Flags().GetString("note")
	body := map[string]any{"decision": decision, "note": strings.TrimSpace(note), "outcome": outcome}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/proposals/"+url.PathEscape(id)+"/decision", body, &result); err != nil {
		return err
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Proposal %s: %s\n", strVal(result, "id"), decision+"d")
	return nil
}

// proposalKindFromFlags reads --kind and the kind's own flags. The decision kind
// has no payload. An opportunity needs its thesis; its other fields are optional.
// A kind the CLI does not know is sent as given, and the server refuses it.
func proposalKindFromFlags(cmd *cobra.Command) (string, map[string]any, error) {
	kind, _ := cmd.Flags().GetString("kind")
	thesis, _ := cmd.Flags().GetString("thesis")
	market, _ := cmd.Flags().GetString("market")
	docs, _ := cmd.Flags().GetStringArray("evidence-doc")
	target, _ := cmd.Flags().GetString("target-stage")
	if kind == "" || kind == "decision" {
		if thesis != "" || market != "" || len(docs) > 0 || target != "" {
			return "", nil, fmt.Errorf("--thesis, --market, --evidence-doc and --target-stage need --kind opportunity")
		}
		return "decision", nil, nil
	}
	if kind != "opportunity" {
		return kind, map[string]any{}, nil
	}
	if thesis == "" {
		return "", nil, fmt.Errorf("--thesis is required with --kind opportunity: say why this is worth doing")
	}
	payload := map[string]any{"thesis": thesis}
	if market != "" {
		payload["market"] = market
	}
	if len(docs) > 0 {
		payload["evidence_doc_ids"] = docs
	}
	if target != "" {
		payload["target_stage"] = target
	}
	return kind, payload, nil
}
