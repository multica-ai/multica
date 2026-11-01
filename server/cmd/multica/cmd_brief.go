package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
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

  multica brief digest --since 24h --json`,
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
	path := "/api/office/digest?" + url.Values{"since": {since.UTC().Format(time.RFC3339)}}.Encode()
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
}

func joinNames(items []any) string {
	names := make([]string, 0, len(items))
	for _, it := range items {
		m, _ := it.(map[string]any)
		names = append(names, strVal(m, "name"))
	}
	return strings.Join(names, ", ")
}

func addBriefDigestFlags(cmd *cobra.Command) {
	cmd.Flags().String("since", "24h", "Look back this far: a duration (30m, 24h, 7d) or an RFC 3339 timestamp")
	cmd.Flags().Bool("json", false, "Print JSON (same as --output json)")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

func init() {
	addBriefDigestFlags(briefDigestCmd)
	briefCmd.AddCommand(briefDigestCmd)
}
