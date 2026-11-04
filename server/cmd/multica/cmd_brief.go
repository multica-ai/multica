package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var briefCmd = &cobra.Command{
	Use:   "brief",
	Short: "Read the company digest and write the boss's briefs",
	Long: `The Secretary's tools. The digest is the facts about the company since a point
in time, computed by the server and bounded in size; a brief is the written
report the boss reads.

  multica brief digest --since 24h --json
  multica brief publish --kind daily --title "Daily brief" --file brief.md
  multica brief ask --squad <id> --issue <id> "Why did the pilot slip?"
  multica brief feedback --recent 14d --json
  multica brief list
  multica brief show <id>`,
}

var briefAskCmd = &cobra.Command{
	Use:   "ask",
	Short: "Ask a squad leader a question about an issue",
	Long: `Ask the leader of a squad a question. The question is a comment on the issue,
mentioning the leader, so the leader is woken as for any mention. The answer is
the leader's next comment on that issue; the next digest carries it.

--issue is required: the question is a comment, and a comment needs an issue to
live on. Pick the issue the digest flagged.

At most three questions per workspace per UTC day. A fourth is refused with
inquiry_limit; put that question in the next brief instead.

  multica brief ask --squad <squad-id> --issue <issue-id> "What changed on the pilot?"`,
	Args: cobra.ExactArgs(1),
	RunE: runBriefAsk,
}

var briefPublishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish a brief to the boss",
	Long: `Publish the brief in a markdown file (or - for stdin). Only the workspace's
secretary agent or an admin member may publish. The boss gets one inbox item.

The body must have these H2 headings, in this order:

  ## Headline
  ## Needs your decision
  ## Progress
  ## Blockers and risks
  ## Watch
  ## FYI

and be at most 20000 characters. If a heading is missing the server says which;
fix the file and publish again. --proposal links a proposal the brief raises so
the app can show Approve and Reject beneath it; repeat it for more.

  multica brief publish --kind daily --title "Daily brief" --file brief.md \
    --proposal <proposal-id>`,
	Args: cobra.NoArgs,
	RunE: runBriefPublish,
}

var briefListCmd = &cobra.Command{
	Use:   "list",
	Short: "List briefs, newest first",
	Args:  cobra.NoArgs,
	RunE:  runBriefList,
}

var briefShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Print one brief",
	Args:  cobra.ExactArgs(1),
	RunE:  runBriefShow,
}

var briefFeedbackCmd = &cobra.Command{
	Use:   "feedback",
	Short: "Print the boss's recent ratings of briefs",
	Long: `Print the ratings the boss gave briefs in the last N days, newest first: the
rating, its tags and note, and the brief's kind and date. The Secretary reads
this at the start of every run and keeps standing preferences from it.

--recent is a whole number of days with a d suffix, from 1d to 35d. Only the
workspace's secretary agent or an admin member may read it.

  multica brief feedback --recent 14d --json`,
	Args: cobra.NoArgs,
	RunE: runBriefFeedback,
}

// briefRecentPattern admits only a whole number of days with a d suffix, so the
// window is never read two ways.
var briefRecentPattern = regexp.MustCompile(`^[1-9][0-9]*d$`)

const briefRecentMaxDays = 35

// parseBriefRecentDays reads --recent as a day count. It refuses hours, weeks,
// fractions and leading zeros, and a count above the server's 35-day limit.
func parseBriefRecentDays(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if !briefRecentPattern.MatchString(raw) {
		return 0, fmt.Errorf("invalid --recent %q: use a whole number of days with a d suffix, such as 14d", raw)
	}
	n, err := strconv.Atoi(strings.TrimSuffix(raw, "d"))
	if err != nil || n > briefRecentMaxDays {
		return 0, fmt.Errorf("invalid --recent %q: at most %dd", raw, briefRecentMaxDays)
	}
	return n, nil
}

func runBriefAsk(cmd *cobra.Command, args []string) error {
	squad, _ := cmd.Flags().GetString("squad")
	issue, _ := cmd.Flags().GetString("issue")
	question := strings.TrimSpace(args[0])

	squad = strings.TrimSpace(squad)
	if !uuidPattern.MatchString(squad) {
		return fmt.Errorf("--squad is required and must be a squad id; run `multica squad list`")
	}
	issue = strings.TrimSpace(issue)
	if issue == "" {
		return fmt.Errorf("--issue is required: a question is a comment on the issue it asks about")
	}
	if !uuidPattern.MatchString(issue) {
		return fmt.Errorf("--issue %q is not an issue id", issue)
	}
	if question == "" {
		return fmt.Errorf("the question is empty")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	req := map[string]any{"squad_id": squad, "issue_id": issue, "question": question}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/briefs/inquiries", req, &result); err != nil {
		return briefRefusalError(err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Question asked: %s\n", strVal(result, "id"))
	return nil
}

var briefDigestCmd = &cobra.Command{
	Use:   "digest",
	Short: "Print the company digest since a point in time",
	Long: `Print the digest: per squad (most in need of attention first) what was done,
what is in progress, blocked or stale, failed runs, leader no-action count,
pending proposals and the most recent completed issues as mention links; quiet
squads in one list; and the workspace's runs, cost and offline runtimes.

--since takes a duration (30m, 24h, 7d) or an RFC 3339 timestamp and defaults
to 24h. The server looks back at most 35 days.`,
	Args: cobra.NoArgs,
	RunE: runBriefDigest,
}

// parseBriefSince turns --since into the RFC 3339 timestamp the server wants.
// It accepts Go durations plus a d suffix for days.
func parseBriefSince(raw string, now time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return now.Add(-24 * time.Hour), nil
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts, nil
	}
	if days, ok := strings.CutSuffix(raw, "d"); ok {
		n, err := strconv.ParseFloat(days, 64)
		if err != nil || n <= 0 {
			return time.Time{}, fmt.Errorf("invalid --since %q: use a duration such as 30m, 24h or 7d, or an RFC 3339 timestamp", raw)
		}
		return now.Add(-time.Duration(n * float64(24*time.Hour))), nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return time.Time{}, fmt.Errorf("invalid --since %q: use a duration such as 30m, 24h or 7d, or an RFC 3339 timestamp", raw)
	}
	return now.Add(-d), nil
}

func runBriefDigest(cmd *cobra.Command, _ []string) error {
	raw, _ := cmd.Flags().GetString("since")
	since, err := parseBriefSince(raw, time.Now())
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var digest map[string]any
	query := url.Values{"since": {since.UTC().Format(time.RFC3339)}}
	// A floor chief's digest, or the Secretary's drill into a flagged floor (spec 4.8).
	if floor, _ := cmd.Flags().GetString("floor"); floor != "" {
		query.Set("floor_id", strings.TrimSpace(floor))
	}
	path := "/api/company/digest?" + query.Encode()
	if err := client.GetJSON(ctx, path, &digest); err != nil {
		return fmt.Errorf("get digest: %w", err)
	}

	asJSON, _ := cmd.Flags().GetBool("json")
	if output, _ := cmd.Flags().GetString("output"); asJSON || output == "json" {
		return cli.PrintJSON(os.Stdout, digest)
	}
	printDigest(os.Stdout, digest)
	return nil
}

func mapVal(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

func sliceVal(m map[string]any, key string) []any {
	v, _ := m[key].([]any)
	return v
}

func intVal(m map[string]any, key string) int {
	f, _ := m[key].(float64)
	return int(f)
}

// printDigest renders the digest for a person. Agents are expected to use
// --json.
func printDigest(w *os.File, d map[string]any) {
	fmt.Fprintf(w, "Digest %s to %s\n\n", strVal(d, "since"), strVal(d, "until"))
	ws := mapVal(d, "workspace")
	fmt.Fprintf(w, "Workspace: %d runs (%d failed), %d input / %d output tokens, $%.2f reported cost\n",
		intVal(ws, "runs"), intVal(ws, "failed_runs"), intVal(ws, "input_tokens"), intVal(ws, "output_tokens"),
		floatVal(ws, "reported_cost_usd"))
	if rt := mapVal(ws, "offline_runtimes"); intVal(rt, "count") > 0 {
		fmt.Fprintf(w, "  Offline runtimes (%d): %s\n", intVal(rt, "count"), joinNames(sliceVal(rt, "items")))
	}
	if ag := mapVal(ws, "agents_with_failed_runs"); intVal(ag, "count") > 0 {
		fmt.Fprintf(w, "  Agents with failed runs (%d): %s\n", intVal(ag, "count"), joinNames(sliceVal(ag, "items")))
	}
	if sq := mapVal(ws, "unseated_squads"); intVal(sq, "count") > 0 {
		fmt.Fprintf(w, "  Squads without a seat (%d): %s\n", intVal(sq, "count"), joinNames(sliceVal(sq, "items")))
	}

	for _, item := range sliceVal(d, "squads") {
		sq, _ := item.(map[string]any)
		fmt.Fprintf(w, "\n%s\n  done %d, in progress %d, blocked %d, stale %d, failed runs %d, leader no-action %d, pending proposals %d\n",
			strVal(sq, "name"), intVal(sq, "done"), intVal(sq, "in_progress"), intVal(sq, "blocked"), intVal(sq, "stale"),
			intVal(sq, "failed_runs"), intVal(sq, "leader_no_action"), intVal(sq, "pending_proposal_count"))
		for _, p := range sliceVal(sq, "pending_proposals") {
			pm, _ := p.(map[string]any)
			fmt.Fprintf(w, "  proposal %s: %s\n", strVal(pm, "id"), strVal(pm, "title"))
		}
		for _, i := range sliceVal(sq, "recent_done") {
			im, _ := i.(map[string]any)
			fmt.Fprintf(w, "  done %s %s\n", strVal(im, "mention"), strVal(im, "title"))
		}
	}
	if n := intVal(d, "omitted_squad_count"); n > 0 {
		fmt.Fprintf(w, "\n%d more squads with news were ranked below the cut.\n", n)
	}
	if q := mapVal(d, "quiet"); intVal(q, "count") > 0 {
		fmt.Fprintf(w, "\nQuiet (%d): %s\n", intVal(q, "count"), joinNames(sliceVal(q, "squads")))
	}
	for _, item := range sliceVal(d, "inquiries") {
		im, _ := item.(map[string]any)
		state := "no answer yet"
		if b, _ := im["answered"].(bool); b {
			state = "answered: " + strVal(im, "answer_excerpt")
		}
		fmt.Fprintf(w, "\nQuestion to a leader (%s): %s\n", state, strVal(im, "question"))
	}
}

func joinNames(items []any) string {
	names := make([]string, 0, len(items))
	for _, it := range items {
		m, _ := it.(map[string]any)
		names = append(names, strVal(m, "name"))
	}
	return strings.Join(names, ", ")
}

// briefRefusalError prints the server's own message for a refused brief or
// question (a validation, permission or daily-limit error written for the agent),
// unchanged and without a status prefix.
func briefRefusalError(err error) error {
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) || (httpErr.StatusCode != 400 && httpErr.StatusCode != 403 && httpErr.StatusCode != 404 && httpErr.StatusCode != 429) {
		return err
	}
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(httpErr.Body), &body) != nil || body.Error == "" {
		return err
	}
	return cli.WithUserMessage(body.Error, err)
}

// readBriefFile reads the brief body from a path, or stdin for "-".
func readBriefFile(path string) (string, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("read --file: %w", err)
	}
	return string(data), nil
}

func runBriefPublish(cmd *cobra.Command, _ []string) error {
	kind, _ := cmd.Flags().GetString("kind")
	title, _ := cmd.Flags().GetString("title")
	file, _ := cmd.Flags().GetString("file")
	proposals, _ := cmd.Flags().GetStringArray("proposal")

	switch kind {
	case "daily", "weekly", "ad_hoc":
	case "":
		return fmt.Errorf("--kind is required: daily, weekly or ad_hoc")
	default:
		return fmt.Errorf("invalid --kind %q; valid values: daily, weekly, ad_hoc", kind)
	}
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("--title is required")
	}
	if file == "" {
		return fmt.Errorf("--file is required: a markdown file with the brief, or - for stdin")
	}
	body, err := readBriefFile(file)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	req := map[string]any{"kind": kind, "title": title, "body": body, "proposal_ids": trimmedAll(proposals)}
	// A floor brief names its floor; a company brief names none (spec 4.8).
	if scope, _ := cmd.Flags().GetString("scope"); scope == "floor" {
		floor, _ := cmd.Flags().GetString("floor")
		if strings.TrimSpace(floor) == "" {
			return fmt.Errorf("--floor is required with --scope floor: the floor's id")
		}
		req["scope_type"], req["scope_id"] = "floor", strings.TrimSpace(floor)
	} else if floor, _ := cmd.Flags().GetString("floor"); floor != "" {
		return fmt.Errorf("--floor applies only with --scope floor")
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/briefs", req, &result); err != nil {
		return briefRefusalError(err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Brief published: %s\n", strVal(result, "id"))
	return nil
}

func runBriefList(cmd *cobra.Command, _ []string) error {
	query := url.Values{}
	if kind, _ := cmd.Flags().GetString("kind"); kind != "" {
		query.Set("kind", kind)
	}
	if limit, _ := cmd.Flags().GetInt("limit"); limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	path := "/api/briefs"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var briefs []map[string]any
	if err := client.GetJSON(ctx, path, &briefs); err != nil {
		return briefRefusalError(fmt.Errorf("list briefs: %w", err))
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if output, _ := cmd.Flags().GetString("output"); asJSON || output == "json" {
		if briefs == nil {
			briefs = []map[string]any{}
		}
		return cli.PrintJSON(os.Stdout, briefs)
	}
	if len(briefs) == 0 {
		fmt.Fprintln(os.Stderr, "No briefs found.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tKIND\tCREATED\tTITLE")
	for _, b := range briefs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", strVal(b, "id"), strVal(b, "kind"), strVal(b, "created_at"), strVal(b, "title"))
	}
	return w.Flush()
}

func runBriefFeedback(cmd *cobra.Command, _ []string) error {
	raw, _ := cmd.Flags().GetString("recent")
	days, err := parseBriefRecentDays(raw)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var entries []map[string]any
	path := "/api/briefs/feedback/recent?" + url.Values{"days": {strconv.Itoa(days)}}.Encode()
	if err := client.GetJSON(ctx, path, &entries); err != nil {
		return briefRefusalError(fmt.Errorf("get brief feedback: %w", err))
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if output, _ := cmd.Flags().GetString("output"); asJSON || output == "json" {
		if entries == nil {
			entries = []map[string]any{}
		}
		return cli.PrintJSON(os.Stdout, entries)
	}
	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "No brief feedback in the last %d days.\n", days)
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "BRIEF\tRATING\tTAGS\tNOTE")
	for _, e := range entries {
		tags := make([]string, 0)
		for _, t := range sliceVal(e, "tags") {
			tag, _ := t.(string)
			tags = append(tags, tag)
		}
		fmt.Fprintf(w, "%s %s\t%s\t%s\t%s\n", strVal(e, "brief_kind"), strVal(e, "brief_created_at"),
			strVal(e, "rating"), strings.Join(tags, ","), strVal(e, "note"))
	}
	return w.Flush()
}

func runBriefShow(cmd *cobra.Command, args []string) error {
	id := strings.TrimSpace(args[0])
	if !uuidPattern.MatchString(id) {
		return fmt.Errorf("%q is not a brief id; run `multica brief list`", id)
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var brief map[string]any
	if err := client.GetJSON(ctx, "/api/briefs/"+url.PathEscape(id), &brief); err != nil {
		return fmt.Errorf("get brief: %w", err)
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if output, _ := cmd.Flags().GetString("output"); asJSON || output == "json" {
		return cli.PrintJSON(os.Stdout, brief)
	}
	fmt.Printf("# %s\n\n%s %s to %s\n\n%s\n", strVal(brief, "title"), strVal(brief, "kind"),
		strVal(brief, "period_start"), strVal(brief, "period_end"), strVal(brief, "body"))
	return nil
}

func addBriefPublishFlags(cmd *cobra.Command) {
	cmd.Flags().String("kind", "", "daily, weekly or ad_hoc — required")
	cmd.Flags().String("title", "", "Title of the brief (at most 200 characters) — required")
	cmd.Flags().String("file", "", "Markdown file with the brief, or - for stdin — required")
	cmd.Flags().StringArray("proposal", nil, "A proposal id the brief raises; repeat for more")
	cmd.Flags().String("scope", "company", "company, or floor for a floor chief's brief")
	cmd.Flags().String("floor", "", "The floor's id, with --scope floor")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

func addBriefListFlags(cmd *cobra.Command) {
	cmd.Flags().String("kind", "", "Only briefs of this kind: daily, weekly or ad_hoc")
	cmd.Flags().Int("limit", 0, "Return at most this many briefs (default 30, at most 50)")
	cmd.Flags().Bool("json", false, "Print JSON (same as --output json)")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

func addBriefAskFlags(cmd *cobra.Command) {
	cmd.Flags().String("squad", "", "Id of the squad whose leader is asked — required")
	cmd.Flags().String("issue", "", "Id of the issue the question is about — required")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

func addBriefShowFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "Print JSON (same as --output json)")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

func addBriefFeedbackFlags(cmd *cobra.Command) {
	cmd.Flags().String("recent", "14d", "Ratings given in this many days: a whole number with a d suffix, 1d to 35d")
	cmd.Flags().Bool("json", false, "Print JSON (same as --output json)")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

func addBriefDigestFlags(cmd *cobra.Command) {
	cmd.Flags().String("since", "24h", "Look back this far: a duration (30m, 24h, 7d) or an RFC 3339 timestamp")
	cmd.Flags().String("floor", "", "Only this floor's digest, by floor id: a floor chief's view or a drill into a flagged floor")
	cmd.Flags().Bool("json", false, "Print JSON (same as --output json)")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

func init() {
	addBriefDigestFlags(briefDigestCmd)
	addBriefPublishFlags(briefPublishCmd)
	addBriefListFlags(briefListCmd)
	addBriefShowFlags(briefShowCmd)
	addBriefAskFlags(briefAskCmd)
	addBriefFeedbackFlags(briefFeedbackCmd)
	briefCmd.AddCommand(briefFeedbackCmd)
	briefCmd.AddCommand(briefDigestCmd)
	briefCmd.AddCommand(briefPublishCmd)
	briefCmd.AddCommand(briefAskCmd)
	briefCmd.AddCommand(briefListCmd)
	briefCmd.AddCommand(briefShowCmd)
}
