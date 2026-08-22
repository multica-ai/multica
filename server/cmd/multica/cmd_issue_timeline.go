package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// noneMarker stands in for a missing side of a transition — an issue that had
// no assignee before, or was unassigned after.
const noneMarker = "(none)"

// headerTimelineTruncated mirrors handler.HeaderTimelineTruncated. Declared as
// a literal rather than imported because the CLI does not depend on the
// handler package (same convention as the X-Multica-Next-Before cursors in
// `issue comment list`). Value is a comma-separated kind list: "activity",
// "comment", or "activity,comment".
const headerTimelineTruncated = "X-Timeline-Truncated"

var issueTimelineCmd = &cobra.Command{
	Use:     "timeline <id>",
	Aliases: []string{"history"},
	Short:   "Chronological issue history — when status/assignee changed, how long it has been stuck",
	Long: `Chronological history of an issue: the activity log — status / priority /
assignee / title / date changes, plus whatever task_completed / task_failed
records the server already writes — merged with comments, oldest first.

Use it for the questions the current issue fields cannot answer:

  - When did this issue move to in_review?
  - How long has it been sitting in its current status?
  - What changed on this issue since yesterday?

For "what is the state RIGHT NOW", prefer ` + "`issue get`" + ` — that is the authoritative
current snapshot. This command explains how it got there, which comments alone
cannot: a status change writes an activity record, never a comment, so a merge
that flipped the issue to done leaves no trace in the comment stream.

Comments are included by default. --activity-only drops comment bodies and
keeps every activity record; to isolate the state transitions behind "how long
has this been stuck", filter by action instead.

The server caps how far back it will read. When it reports that the response
was truncated, a warning naming the affected kinds is printed to stderr —
durations and "first entered <status>" cannot be concluded from a truncated
read, because the transition you are looking for may be the one that fell off.

Examples:
  # How long has MUL-123 been in its current status?
  multica issue timeline MUL-123 --action status_changed

  # Every state change, no comment bodies
  multica issue timeline MUL-123 --activity-only --output json

  # What changed since yesterday?
  multica issue timeline MUL-123 --since 2026-08-19T00:00:00Z`,
	Args: exactArgs(1),
	RunE: runIssueTimeline,
}

func init() {
	issueTimelineCmd.Flags().String("output", "table", "Output format: table or json")
	issueTimelineCmd.Flags().Bool("activity-only", false, "Drop comments and return every activity record — including the task_completed / task_failed entries the server already writes, not just field changes. Much cheaper to read than the full timeline; use --action when you want only state transitions.")
	issueTimelineCmd.Flags().StringSlice("action", nil, "Only return activities with these actions (repeatable or comma-separated). Implies --activity-only, since comments carry no action. Known actions: created, status_changed, priority_changed, assignee_changed, title_changed, description_updated, start_date_changed, due_date_changed, task_completed, task_failed, squad_leader_evaluated.")
	issueTimelineCmd.Flags().String("since", "", "Only return entries created after this timestamp (RFC3339)")
	issueTimelineCmd.Flags().Int("tail", 0, "Only return the N most recent entries (applied after every other filter)")
	issueTimelineCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	issueCmd.AddCommand(issueTimelineCmd)
}

func runIssueTimeline(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	filter, err := timelineFilterFromFlags(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	// Deliberately sent without limit/before/after/around: the endpoint only
	// returns the flat oldest-first array when no pagination param is present.
	var entries []map[string]any
	respHeaders, err := client.GetJSONWithHeaders(ctx, "/api/issues/"+url.PathEscape(issueRef.ID)+"/timeline", &entries)
	if err != nil {
		return fmt.Errorf("list issue timeline: %w", err)
	}
	warnTimelineTruncated(os.Stderr, respHeaders.Get(headerTimelineTruncated))

	entries = filter.apply(entries)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, entries)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	printIssueTimelineTable(entries, loadActorDisplayLookup(ctx, client), fullID)
	return nil
}

// warnTimelineTruncated surfaces the server's truncation signal on stderr, so
// it never contaminates a piped --output json read.
//
// The endpoint caps each kind independently and reports which ones lost rows.
// Swallowing that makes a partial read look complete, which is precisely wrong
// for this command's headline question: if the transition that set the current
// status fell off the back of the window, --action status_changed returns a
// plausible, shorter, wrong answer to "how long has this been stuck".
func warnTimelineTruncated(w io.Writer, kinds string) {
	if kinds == "" {
		return
	}
	fmt.Fprintf(w, "warning: timeline truncated by the server cap (%s): older entries are missing. "+
		"Durations and \"first entered <status>\" cannot be concluded from this read.\n", kinds)
}

// timelineFilter narrows a timeline response client-side. The endpoint takes no
// filter parameters and returns the whole timeline in one shot, so every flag
// here is applied after the fetch: it bounds what the caller has to read, not
// what the server has to do.
type timelineFilter struct {
	activityOnly bool
	actions      map[string]bool
	since        time.Time
	tail         int
}

func timelineFilterFromFlags(cmd *cobra.Command) (timelineFilter, error) {
	var f timelineFilter

	f.activityOnly, _ = cmd.Flags().GetBool("activity-only")

	f.tail, _ = cmd.Flags().GetInt("tail")
	if f.tail < 0 {
		return f, fmt.Errorf("--tail must be >= 0")
	}

	actions, _ := cmd.Flags().GetStringSlice("action")
	for _, a := range actions {
		if a = strings.TrimSpace(a); a == "" {
			continue
		}
		if f.actions == nil {
			f.actions = map[string]bool{}
		}
		f.actions[a] = true
	}
	if len(f.actions) > 0 {
		// Comments carry no action, so filtering by action can only ever mean
		// activities.
		f.activityOnly = true
	}

	if raw, _ := cmd.Flags().GetString("since"); raw != "" {
		ts, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return f, fmt.Errorf("invalid --since %q: expected RFC3339, e.g. 2026-08-19T00:00:00Z", raw)
		}
		f.since = ts
	}

	return f, nil
}

func (f timelineFilter) apply(entries []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if f.activityOnly && strVal(e, "type") != "activity" {
			continue
		}
		if len(f.actions) > 0 && !f.actions[strVal(e, "action")] {
			continue
		}
		if !f.since.IsZero() {
			// An entry whose timestamp will not parse cannot be shown to fall
			// inside the window, so a time-bounded read drops it.
			ts, err := time.Parse(time.RFC3339, strVal(e, "created_at"))
			if err != nil || !ts.After(f.since) {
				continue
			}
		}
		out = append(out, e)
	}

	// Entries arrive oldest-first, so the most recent N is the trailing slice.
	if f.tail > 0 && len(out) > f.tail {
		out = out[len(out)-f.tail:]
	}
	return out
}

func printIssueTimelineTable(entries []map[string]any, actors actorDisplayLookup, fullID bool) {
	headers := []string{"TIME", "TYPE", "ACTOR", "DETAIL"}
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		when := strVal(e, "created_at")
		if len(when) >= 16 {
			when = when[:16]
		}
		// An activity is far more legible labelled by its action than by the
		// literal "activity".
		kind := strVal(e, "type")
		if action := strVal(e, "action"); action != "" {
			kind = action
		}
		rows = append(rows, []string{
			when,
			kind,
			timelineActor(strVal(e, "actor_type"), strVal(e, "actor_id"), actors, fullID),
			timelineDetail(e, actors, fullID),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

// timelineActor renders an actor as "member:Alice", falling back to a shortened
// id when the workspace lookup cannot name it (deleted member, foreign id).
func timelineActor(actorType, actorID string, actors actorDisplayLookup, fullID bool) string {
	switch {
	case actorType == "" && actorID == "":
		return ""
	case actorID == "":
		// System-authored activities carry a type with no id, and the lookup
		// returns nothing for those. A GitHub PR merge flipping the issue to
		// done is exactly this shape, so leaving the column blank would erase
		// the actor on the transition this command exists to explain.
		return actorType
	case actorType == "":
		if fullID {
			return actorID
		}
		return truncateID(actorID)
	}
	display := actors.actor(actorType, actorID)
	if !fullID && strings.HasSuffix(display, ":"+actorID) {
		return actorType + ":" + truncateID(actorID)
	}
	return display
}

// timelineDetail renders one entry's payload for table output: a comment's
// clipped body, or an activity's transition. Activity details are free-form
// JSON keyed per action, so unrecognised shapes fall back to sorted key=value
// rather than being dropped.
func timelineDetail(entry map[string]any, actors actorDisplayLookup, fullID bool) string {
	if strVal(entry, "type") == "comment" {
		return clipTimelineText(singleLineText(strVal(entry, "content")), 60)
	}

	details, _ := entry["details"].(map[string]any)
	if len(details) == 0 {
		return ""
	}

	// status / priority / title / date changes: {"from": ..., "to": ...}
	if _, ok := details["from"]; ok {
		return transitionText(strVal(details, "from"), strVal(details, "to"))
	}
	if _, ok := details["to"]; ok {
		return transitionText("", strVal(details, "to"))
	}

	// assignee_changed: {"from_type","from_id","to_type","to_id"}, any of which
	// is absent when that side was unassigned.
	if hasAnyKey(details, "from_type", "from_id", "to_type", "to_id") {
		return transitionText(
			timelineActor(strVal(details, "from_type"), strVal(details, "from_id"), actors, fullID),
			timelineActor(strVal(details, "to_type"), strVal(details, "to_id"), actors, fullID),
		)
	}

	keys := make([]string, 0, len(details))
	for k := range details {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+strVal(details, k))
	}
	return clipTimelineText(strings.Join(parts, " "), 60)
}

func transitionText(from, to string) string {
	if from == "" {
		from = noneMarker
	}
	if to == "" {
		to = noneMarker
	}
	return from + " → " + to
}

func hasAnyKey(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func singleLineText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func clipTimelineText(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
