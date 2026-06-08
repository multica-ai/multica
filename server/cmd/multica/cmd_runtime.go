package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// fetchLatestReleaseFn is the function used to fetch the latest GitHub release.
// Exported as a variable so tests can replace it with a mock.
var fetchLatestReleaseFn = cli.FetchLatestRelease

var runtimeCmd = &cobra.Command{
	Use:   "runtime",
	Short: "Work with agent runtimes",
}

var runtimeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List runtimes in the workspace",
	RunE:  runRuntimeList,
}

var runtimeUsageCmd = &cobra.Command{
	Use:   "usage <runtime-id>",
	Short: "Get token usage for a runtime",
	Args:  exactArgs(1),
	RunE:  runRuntimeUsage,
}

var runtimeActivityCmd = &cobra.Command{
	Use:   "activity <runtime-id>",
	Short: "Get hourly task activity for a runtime",
	Args:  exactArgs(1),
	RunE:  runRuntimeActivity,
}

var runtimeUpdateCmd = &cobra.Command{
	Use:   "update [runtime-id]",
	Short: "Initiate a CLI update on a runtime",
	Args:  maximumNArgs(1),
	RunE:  runRuntimeUpdate,
}

func init() {
	runtimeCmd.AddCommand(runtimeListCmd)
	runtimeCmd.AddCommand(runtimeUsageCmd)
	runtimeCmd.AddCommand(runtimeActivityCmd)
	runtimeCmd.AddCommand(runtimeUpdateCmd)

	// runtime list
	runtimeListCmd.Flags().String("output", "table", "Output format: table or json")

	// runtime usage
	runtimeUsageCmd.Flags().String("output", "table", "Output format: table or json")
	runtimeUsageCmd.Flags().Int("days", 90, "Number of days of usage data to retrieve (max 365)")

	// runtime activity
	runtimeActivityCmd.Flags().String("output", "table", "Output format: table or json")

	// runtime update
	runtimeUpdateCmd.Flags().String("target-version", "", "Target version to update to (required without --latest)")
	runtimeUpdateCmd.Flags().String("output", "json", "Output format: table or json")
	runtimeUpdateCmd.Flags().Bool("wait", false, "Wait for update to complete (poll until done)")
	runtimeUpdateCmd.Flags().Bool("latest", false, "Fetch the latest release from GitHub and use it as target version")
	runtimeUpdateCmd.Flags().Bool("all", false, "Update all runtimes in the workspace (mutually exclusive with runtime-id)")
}

// ---------------------------------------------------------------------------
// Runtime commands
// ---------------------------------------------------------------------------

func runRuntimeList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var runtimes []map[string]any
	if err := client.GetJSON(ctx, "/api/runtimes", &runtimes); err != nil {
		return fmt.Errorf("list runtimes: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, runtimes)
	}

	headers := []string{"ID", "NAME", "MODE", "PROVIDER", "STATUS", "LAST_SEEN"}
	rows := make([][]string, 0, len(runtimes))
	for _, rt := range runtimes {
		rows = append(rows, []string{
			strVal(rt, "id"),
			strVal(rt, "name"),
			strVal(rt, "runtime_mode"),
			strVal(rt, "provider"),
			strVal(rt, "status"),
			strVal(rt, "last_seen_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runRuntimeUsage(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	days, _ := cmd.Flags().GetInt("days")
	if days < 1 || days > 365 {
		return fmt.Errorf("--days must be between 1 and 365")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var usage []map[string]any
	path := fmt.Sprintf("/api/runtimes/%s/usage?days=%d", args[0], days)
	if err := client.GetJSON(ctx, path, &usage); err != nil {
		return fmt.Errorf("get runtime usage: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, usage)
	}

	headers := []string{"DATE", "PROVIDER", "MODEL", "INPUT_TOKENS", "OUTPUT_TOKENS", "CACHE_READ", "CACHE_WRITE"}
	rows := make([][]string, 0, len(usage))
	for _, u := range usage {
		rows = append(rows, []string{
			strVal(u, "date"),
			strVal(u, "provider"),
			strVal(u, "model"),
			strVal(u, "input_tokens"),
			strVal(u, "output_tokens"),
			strVal(u, "cache_read_tokens"),
			strVal(u, "cache_write_tokens"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runRuntimeActivity(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var activity []map[string]any
	if err := client.GetJSON(ctx, "/api/runtimes/"+args[0]+"/activity", &activity); err != nil {
		return fmt.Errorf("get runtime activity: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, activity)
	}

	headers := []string{"HOUR", "COUNT"}
	rows := make([][]string, 0, len(activity))
	for _, a := range activity {
		rows = append(rows, []string{
			strVal(a, "hour"),
			strVal(a, "count"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runRuntimeUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	targetVersion, _ := cmd.Flags().GetString("target-version")
	latest, _ := cmd.Flags().GetBool("latest")
	all, _ := cmd.Flags().GetBool("all")

	// Resolve target version.
	if latest {
		rel, err := fetchLatestReleaseFn()
		if err != nil {
			return fmt.Errorf("fetch latest release: %w", err)
		}
		targetVersion = rel.TagName
	} else if targetVersion == "" {
		return fmt.Errorf("one of --target-version or --latest is required")
	}

	// Collect runtime IDs.
	var runtimeIDs []string
	if all {
		if len(args) == 1 {
			return fmt.Errorf("--all is mutually exclusive with specifying a runtime-id")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		var runtimes []map[string]any
		if err := client.GetJSON(ctx, "/api/runtimes", &runtimes); err != nil {
			return fmt.Errorf("list runtimes: %w", err)
		}

		for _, rt := range runtimes {
			id, ok := rt["id"].(string)
			if !ok || id == "" {
				continue
			}
			runtimeIDs = append(runtimeIDs, id)
		}

		if len(runtimeIDs) == 0 {
			fmt.Fprintln(os.Stderr, "No runtimes found in workspace.")
			return nil
		}
	} else {
		if len(args) != 1 {
			return fmt.Errorf("exactly one runtime-id is required when not using --all")
		}
		runtimeIDs = append(runtimeIDs, args[0])
	}

	wait, _ := cmd.Flags().GetBool("wait")
	output, _ := cmd.Flags().GetString("output")

	// Track updates across all runtimes for --wait mode.
	type updateInfo struct {
		runtimeID string
		updateID  string
		update    map[string]any
	}

	var pending []updateInfo
	results := make([]updateInfo, 0, len(runtimeIDs))

	for _, rtID := range runtimeIDs {
		postCtx, postCancel := context.WithTimeout(context.Background(), 150*time.Second)
		body := map[string]any{
			"target_version": strings.TrimPrefix(targetVersion, "v"),
		}

		var update map[string]any
		if err := client.PostJSON(postCtx, "/api/runtimes/"+rtID+"/update", body, &update); err != nil {
			postCancel()
			results = append(results, updateInfo{runtimeID: rtID, update: map[string]any{"error": err.Error()}})
			continue
		}
		postCancel()

		updateID := strVal(update, "id")

		if !wait {
			results = append(results, updateInfo{runtimeID: rtID, updateID: updateID, update: update})
		} else {
			pending = append(pending, updateInfo{runtimeID: rtID, updateID: updateID, update: update})
		}
	}

	// Poll updates in --wait mode.
	if wait && len(pending) > 0 {
		pollCtx, pollCancel := context.WithTimeout(context.Background(), 150*time.Second)
		defer pollCancel()

		var completed, failed, timedOut int
		done := make(map[string]bool)

	pollLoop:
	for len(done) < len(pending) {
		select {
		case <-pollCtx.Done():
			// Mark remaining as timed out.
			for _, u := range pending {
				if !done[u.updateID] {
					timedOut++
					u.update["status"] = "timeout"
					u.update["error"] = "timed out waiting for update"
					done[u.updateID] = true
					results = append(results, u)
				}
			}
			break pollLoop
		case <-time.After(2 * time.Second):
				for _, u := range pending {
					if done[u.updateID] {
						continue
					}

					var update map[string]any
					if err := client.GetJSON(pollCtx, "/api/runtimes/"+u.runtimeID+"/update/"+u.updateID, &update); err != nil {
						// Polling failed, try next round.
						continue
					}

					status := strVal(update, "status")
					if status == "completed" || status == "failed" || status == "timeout" {
						done[u.updateID] = true
						if status == "completed" {
							completed++
						} else if status == "failed" {
							failed++
						} else {
							timedOut++
						}
						results = append(results, updateInfo{runtimeID: u.runtimeID, updateID: u.updateID, update: update})
					}
				}
			}
		}

		// Print summary.
		if output == "json" {
			type summaryEntry struct {
				RuntimeID string `json:"runtime_id"`
				UpdateID  string `json:"update_id"`
				Status    string `json:"status"`
				Output    string `json:"output,omitempty"`
				Error     string `json:"error,omitempty"`
			}
			entries := make([]summaryEntry, 0, len(results))
			for _, r := range results {
				entries = append(entries, summaryEntry{
					RuntimeID: r.runtimeID,
					UpdateID:  r.updateID,
					Status:    strVal(r.update, "status"),
					Output:    strVal(r.update, "output"),
					Error:     strVal(r.update, "error"),
				})
			}
			return cli.PrintJSON(os.Stdout, map[string]any{
				"completed": completed,
				"failed":    failed,
				"timed_out": timedOut,
				"results":   entries,
			})
		}

		fmt.Printf("Update summary: %d completed, %d failed, %d timed out (target: %s)\n",
			completed, failed, timedOut, targetVersion)
		for _, r := range results {
			status := strVal(r.update, "status")
			if status == "completed" {
				fmt.Printf("  %s: completed — %s\n", r.runtimeID, strVal(r.update, "output"))
			} else {
				fmt.Printf("  %s: %s — %s\n", r.runtimeID, status, strVal(r.update, "error"))
			}
		}
		return nil
	}

	// Non-wait mode output.
	if output == "json" {
		if len(results) == 1 && !all {
			return cli.PrintJSON(os.Stdout, results[0].update)
		}
		var allResults []map[string]any
		for _, r := range results {
			allResults = append(allResults, r.update)
		}
		return cli.PrintJSON(os.Stdout, allResults)
	}

	for _, r := range results {
		fmt.Printf("Runtime %s: update %s (status: %s)\n",
			r.runtimeID, strVal(r.update, "id"), strVal(r.update, "status"))
	}
	return nil
}
