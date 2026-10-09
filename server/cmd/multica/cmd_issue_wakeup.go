package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/pkg/eventcontract"
	"github.com/spf13/cobra"
)

func init() { issueCmd.AddCommand(newIssueWakeupCommand()) }

func newIssueWakeupCommand() *cobra.Command {
	wake := &cobra.Command{Use: "wakeup", Short: "Manage event and time wakeups that start ordinary runs"}
	wake.AddCommand(&cobra.Command{Use: "events", Short: "List supported event types and filters", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return cli.PrintJSON(os.Stdout, map[string]any{"event_types": eventcontract.WakeupTypes, "platform_only_event_types": eventcontract.LifecycleTypes, "platform_only_reason": "Creation precedes subscription; deletion withdraws the issue's wakeups. Cross-issue subscriptions are not supported.", "scope": "current issue", "filters": []string{"filter_agent_id (task events; legacy mutation alias)", "filter_task_id (task events only)", "filter_actor_type + filter_actor_id (member or agent; mutation events only)"}, "modes": []string{"once (default)", "continuous"}, "conditions": []string{"--until-status KEY", "--until-assignee member|agent|squad:ID", "--until-label LABEL_ID", "--until-property PROPERTY_ID=VALUE", "--until-children-done [--stage N]", "--until-pr checks|merged", "--until-issue ISSUE [--until-issue-state done|ended|in_review]"}, "loop_protection": "Events from the registering run and runs started by the same rule are excluded when source identity is available. Repeating rules stop after --max-fires runs (default 20 for continuous event rules). A rule is paused when its trigger chain passes through it a third time without a person in between, or when it starts more than 12 runs in an hour."})
	}})
	for _, action := range []string{"list", "get", "disable", "create", "update", "trigger", "delete", "checkin", "runs"} {
		action := action
		count := 1
		if action != "list" && action != "create" {
			count = 2
		}
		use := action + " <issue-id>"
		if count == 2 {
			use += " <wakeup-id>"
		}
		short := action + " issue wakeups"
		switch action {
		case "trigger":
			short = "Wake the target now, as if the rule fired"
		case "delete":
			short = "Delete a wakeup and withdraw its runs that have not started"
		case "checkin":
			short = "End a scheduled check silently (every/cron runs only)"
			c := &cobra.Command{Use: use, Args: cobra.ExactArgs(count), Short: short, RunE: func(cmd *cobra.Command, args []string) error { return runIssueWakeup(cmd, args, action) }}
			c.Long = "Run this from a run that an every or cron wakeup started, when the check found nothing that needs a reply. The note is shown on the rule and in the issue timeline, and the run then posts no comment. Anything else still ends with a comment."
			c.Flags().String("note", "", "What the check found, e.g. \"CI still running\" (up to 500 characters)")
			wake.AddCommand(c)
			continue
		case "runs":
			short = "List the latest runs a wakeup started"
		}
		c := &cobra.Command{Use: use, Args: cobra.ExactArgs(count), Short: short, RunE: func(cmd *cobra.Command, args []string) error { return runIssueWakeup(cmd, args, action) }}
		c.Flags().String("output", "json", "Output format (json or table)")
		if action == "create" || action == "update" {
			c.Long = "Create or replace the complete configuration. Events default to once; every/cron use continuous. Updating explicitly re-enables the configuration. Runs use normal comment delivery."
			c.Long += " Give waits an end with --expires-in or --expires-at; --on-timeout wake runs the target once if the deadline passes first."
			c.Long += " For task events, use --task-id for one run or --filter-agent-id for its agent. For comment/issue/reaction/attachment changes, use --filter-actor-type member|agent with --filter-actor-id. To wait for a person to comment, use --event comment.created --filter-actor-type member --filter-actor-id USER_ID. Actor filters identify who made the change, not the original author of an edited comment. Without a source filter, all matching events on this issue can wake the target."
			c.Flags().String("agent-id", "", "Agent to wake (defaults to authenticated agent)")
			c.Flags().String("instruction", "", "Instruction for the next run")
			c.Flags().String("instruction-file", "", "Read instruction from a UTF-8 file")
			c.Flags().String("kind", "event", "event, at, every or cron")
			c.Flags().String("mode", "", "once or continuous")
			c.Flags().StringSlice("event", nil, "Event types (comma-separated); see wakeup events")
			c.Flags().String("filter-actor-type", "", "Event actor: member or agent (requires --filter-actor-id)")
			c.Flags().String("filter-actor-id", "", "Actor user/agent UUID in this workspace; mutation events only")
			c.Flags().String("filter-agent-id", "", "Run agent filter; legacy alias of actor=agent for mutation-only events")
			c.Flags().String("task-id", "", "Match this specific run; terminal state is checked on registration")
			c.Flags().String("parent", "", "Comment thread for result delivery")
			c.Flags().String("after", "", "Delay for at, e.g. 10m")
			c.Flags().String("at", "", "Single RFC3339 timestamp")
			c.Flags().String("every", "", "Fixed interval, e.g. 1h (minimum 1m)")
			c.Flags().String("cron", "", "Five-field cron expression")
			c.Flags().String("timezone", "UTC", "IANA timezone for cron")
			c.Flags().String("expires-in", "", "End the rule after this wait, e.g. 72h; restarts when re-enabled (event, every, cron)")
			c.Flags().String("expires-at", "", "End the rule at this RFC3339 time (event, every, cron)")
			c.Flags().String("on-timeout", "", "At the deadline: end (default) or wake (event rules run the target once to handle it)")
			c.Long += " Conditions let the platform check stored facts itself and wake the target only when they hold: use one --until-* flag instead of --event. A condition that already holds fires on the next check, except --until-pr checks, which waits for a newer finished result."
			c.Flags().String("until-status", "", "Condition: this issue's status becomes KEY")
			c.Flags().String("until-assignee", "", "Condition: this issue is assigned to member|agent|squad:ID")
			c.Flags().String("until-label", "", "Condition: this issue has the label LABEL_ID")
			c.Flags().String("until-property", "", "Condition: property PROPERTY_ID equals VALUE (PROPERTY_ID=VALUE; VALUE may be JSON)")
			c.Flags().Bool("until-children-done", false, "Condition: every sub-issue is closed (or those up to --stage)")
			c.Flags().Int32("stage", 0, "With --until-children-done: wait for this stage and earlier ones")
			c.Flags().String("until-pr", "", "Condition: a linked pull request's checks finish (checks) or it merges (merged)")
			c.Flags().String("until-issue", "", "Condition: another issue in this workspace reaches --until-issue-state")
			c.Flags().String("until-issue-state", "done", "With --until-issue: done, ended (done or cancelled) or in_review")
			c.Flags().Int32("max-fires", 0, "Repeating rules: stop after this many runs (1-1000; continuous event rules default to 20)")
		}
		wake.AddCommand(c)
	}
	return wake
}

func runIssueWakeup(cmd *cobra.Command, args []string, action string) error {
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
	path := "/api/issues/" + url.PathEscape(ref.ID) + "/wakeups"
	var result any
	if action == "list" || action == "get" {
		var rows []map[string]any
		if err = client.GetJSON(ctx, path, &rows); err != nil {
			return err
		}
		result = rows
		if action == "get" {
			result = nil
			for _, row := range rows {
				if strVal(row, "id") == args[1] {
					result = row
					break
				}
			}
			if result == nil {
				return fmt.Errorf("wakeup not found")
			}
		}
	} else if action == "disable" {
		var row map[string]any
		err = client.PostJSON(ctx, path+"/"+url.PathEscape(args[1])+"/disable", map[string]any{}, &row)
		result = row
	} else if action == "trigger" {
		if err = client.PostJSON(ctx, path+"/"+url.PathEscape(args[1])+"/trigger", map[string]any{}, nil); err != nil {
			return err
		}
		result = map[string]any{"id": args[1], "triggered": true}
	} else if action == "delete" {
		if err = client.DeleteJSON(ctx, path+"/"+url.PathEscape(args[1])); err != nil {
			return err
		}
		result = map[string]any{"id": args[1], "deleted": true}
	} else if action == "checkin" {
		note, _ := cmd.Flags().GetString("note")
		if note == "" {
			return fmt.Errorf("--note is required")
		}
		if err = client.PostJSON(ctx, path+"/"+url.PathEscape(args[1])+"/checkin", map[string]any{"note": note}, nil); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "Checked in; this run will not post a comment.")
		return nil
	} else if action == "runs" {
		var rows []map[string]any
		if err = client.GetJSON(ctx, path+"/"+url.PathEscape(args[1])+"/runs", &rows); err != nil {
			return err
		}
		output, _ := cmd.Flags().GetString("output")
		if output == "table" {
			cells := [][]string{}
			for _, r := range rows {
				cells = append(cells, []string{strVal(r, "id"), strVal(r, "status"), strVal(r, "created_at"), strVal(r, "checkin_note")})
			}
			cli.PrintTable(os.Stdout, []string{"RUN", "STATUS", "CREATED", "CHECK-IN"}, cells)
			return nil
		}
		return cli.PrintJSON(os.Stdout, rows)
	} else {
		body := map[string]any{}
		for flag, key := range map[string]string{"agent-id": "agent_id", "kind": "kind", "mode": "mode", "filter-agent-id": "filter_agent_id", "filter-actor-type": "filter_actor_type", "filter-actor-id": "filter_actor_id", "task-id": "filter_task_id", "parent": "parent_comment_id", "at": "at", "cron": "cron_expression", "timezone": "timezone", "expires-at": "expires_at", "on-timeout": "on_timeout"} {
			v, _ := cmd.Flags().GetString(flag)
			if v != "" {
				body[key] = v
			}
		}
		instruction, _, e := resolveTextFlag(cmd, "instruction")
		if e != nil {
			return e
		}
		body["instruction"] = instruction
		ev, _ := cmd.Flags().GetStringSlice("event")
		if len(ev) > 0 {
			body["event_types"] = ev
		}
		condition, e := wakeupConditionFromFlags(ctx, cmd, client)
		if e != nil {
			return e
		}
		if condition != nil {
			body["condition"] = condition
		}
		if n, _ := cmd.Flags().GetInt32("max-fires"); n != 0 {
			body["max_fires"] = n
		}
		for flag, key := range map[string]string{"after": "after_seconds", "every": "interval_seconds", "expires-in": "expires_in_seconds"} {
			v, _ := cmd.Flags().GetString(flag)
			if v != "" {
				d, e := time.ParseDuration(v)
				if e != nil || d <= 0 || d%time.Second != 0 {
					return fmt.Errorf("--%s requires a positive whole-second duration", flag)
				}
				body[key] = int64(d / time.Second)
			}
		}
		var row map[string]any
		if action == "update" {
			err = client.PutJSON(ctx, path+"/"+url.PathEscape(args[1]), body, &row)
		} else {
			err = client.PostJSON(ctx, path, body, &row)
			if isWakeupSourceBusy(err) {
				// The server reports this only after rolling back the NOWAIT
				// transaction. Retry once after a short commit window; never retry
				// ambiguous transport failures or other non-idempotent POST errors.
				timer := time.NewTimer(250 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-timer.C:
					err = client.PostJSON(ctx, path, body, &row)
				}
			}
		}
		result = row
	}
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		rows := []map[string]any{}
		switch r := result.(type) {
		case []map[string]any:
			rows = r
		case map[string]any:
			rows = append(rows, r)
		}
		cells := [][]string{}
		for _, r := range rows {
			cells = append(cells, []string{strVal(r, "id"), strVal(r, "kind"), strVal(r, "mode"), fmt.Sprint(r["enabled"]), strVal(r, "next_fire_at"), strVal(r, "last_task_id")})
		}
		cli.PrintTable(os.Stdout, []string{"ID", "KIND", "MODE", "ENABLED", "NEXT", "LAST RUN"}, cells)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func isWakeupSourceBusy(err error) bool {
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusConflict {
		return false
	}
	var body struct {
		Code string `json:"code"`
	}
	return json.Unmarshal([]byte(httpErr.Body), &body) == nil && body.Code == "wakeup_source_busy"
}

// wakeupConditionFromFlags builds the one platform-evaluated condition the
// --until-* flags describe, or nil when none is set.
func wakeupConditionFromFlags(ctx context.Context, cmd *cobra.Command, client *cli.APIClient) (map[string]any, error) {
	var conditions []map[string]any
	if v, _ := cmd.Flags().GetString("until-status"); v != "" {
		conditions = append(conditions, map[string]any{"type": "issue_field", "field": "status", "value": v})
	}
	if v, _ := cmd.Flags().GetString("until-assignee"); v != "" {
		kind, id, ok := strings.Cut(v, ":")
		if !ok || id == "" {
			return nil, fmt.Errorf("--until-assignee takes member|agent|squad:ID")
		}
		conditions = append(conditions, map[string]any{"type": "issue_field", "field": "assignee", "assignee_type": kind, "assignee_id": id})
	}
	if v, _ := cmd.Flags().GetString("until-label"); v != "" {
		conditions = append(conditions, map[string]any{"type": "issue_field", "field": "label", "label_id": v})
	}
	if v, _ := cmd.Flags().GetString("until-property"); v != "" {
		id, raw, ok := strings.Cut(v, "=")
		if !ok || id == "" || raw == "" {
			return nil, fmt.Errorf("--until-property takes PROPERTY_ID=VALUE")
		}
		var value any = raw
		if json.Valid([]byte(raw)) {
			value = json.RawMessage(raw)
		}
		conditions = append(conditions, map[string]any{"type": "issue_field", "field": "property", "property_id": id, "value": value})
	}
	stage, _ := cmd.Flags().GetInt32("stage")
	if done, _ := cmd.Flags().GetBool("until-children-done"); done {
		c := map[string]any{"type": "children_done"}
		if stage != 0 {
			c["stage"] = stage
		}
		conditions = append(conditions, c)
	} else if stage != 0 {
		return nil, fmt.Errorf("--stage requires --until-children-done")
	}
	if v, _ := cmd.Flags().GetString("until-pr"); v != "" {
		event := map[string]string{"checks": "checks_finished", "merged": "merged"}[v]
		if event == "" {
			return nil, fmt.Errorf("--until-pr takes checks or merged")
		}
		conditions = append(conditions, map[string]any{"type": "pull_request", "event": event})
	}
	if v, _ := cmd.Flags().GetString("until-issue"); v != "" {
		ref, err := resolveIssueRef(ctx, client, v)
		if err != nil {
			return nil, err
		}
		state, _ := cmd.Flags().GetString("until-issue-state")
		conditions = append(conditions, map[string]any{"type": "other_issue", "issue_id": ref.ID, "state": state})
	}
	switch len(conditions) {
	case 0:
		return nil, nil
	case 1:
		return conditions[0], nil
	default:
		return nil, fmt.Errorf("use one --until-* condition per wakeup")
	}
}
