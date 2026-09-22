package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/pkg/eventcontract"
	"github.com/spf13/cobra"
)

func init() { issueCmd.AddCommand(newIssueWakeupCommand()) }

func newIssueWakeupCommand() *cobra.Command {
	wake := &cobra.Command{Use: "wakeup", Short: "Manage event and time wakeups that start ordinary runs"}
	wake.AddCommand(&cobra.Command{Use: "events", Short: "List supported event types and filters", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return cli.PrintJSON(os.Stdout, map[string]any{"event_types": eventcontract.WakeupTypes, "platform_only_event_types": eventcontract.LifecycleTypes, "platform_only_reason": "Creation precedes subscription; deletion withdraws the issue's wakeups. Cross-issue subscriptions are not supported.", "scope": "current issue", "filters": []string{"filter_agent_id (task events; legacy mutation alias)", "filter_task_id (task events only)", "filter_actor_type + filter_actor_id (member or agent; mutation events only)"}, "modes": []string{"once (default)", "continuous"}, "loop_protection": "Events from the registering run and runs started by the same rule are excluded when source identity is available. Cross-rule cycles are not prevented; avoid mutually triggering continuous comment subscriptions."})
	}})
	for _, action := range []string{"list", "get", "disable", "create", "update"} {
		action := action
		count := 1
		if action == "get" || action == "disable" || action == "update" {
			count = 2
		}
		use := action + " <issue-id>"
		if count == 2 {
			use += " <wakeup-id>"
		}
		c := &cobra.Command{Use: use, Args: cobra.ExactArgs(count), Short: action + " issue wakeups", RunE: func(cmd *cobra.Command, args []string) error { return runIssueWakeup(cmd, args, action) }}
		c.Flags().String("output", "json", "Output format (json or table)")
		if action == "create" || action == "update" {
			c.Long = "Create or replace the complete configuration. Events default to once; every/cron use continuous. Updating explicitly re-enables the configuration. Runs use normal comment delivery."
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
	} else {
		body := map[string]any{}
		for flag, key := range map[string]string{"agent-id": "agent_id", "kind": "kind", "mode": "mode", "filter-agent-id": "filter_agent_id", "filter-actor-type": "filter_actor_type", "filter-actor-id": "filter_actor_id", "task-id": "filter_task_id", "parent": "parent_comment_id", "at": "at", "cron": "cron_expression", "timezone": "timezone"} {
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
		for flag, key := range map[string]string{"after": "after_seconds", "every": "interval_seconds"} {
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
