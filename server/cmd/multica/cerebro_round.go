// CEREBRO-PATCH(cerebro-rounds-cli): FIR-2736 controlled inbox rounds.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var roundCmd = &cobra.Command{Use: "round", Short: "Manage controlled inbox rounds"}
var roundListCmd = &cobra.Command{Use: "list", Short: "List rounds", RunE: runRoundList}
var roundCreateCmd = &cobra.Command{Use: "create", Short: "Create a round", RunE: runRoundCreate}
var roundAddCmd = &cobra.Command{Use: "add <issue>", Short: "Add an issue to a round", Args: exactArgs(1), RunE: runRoundAdd}
var roundRemoveCmd = &cobra.Command{Use: "remove <issue>", Short: "Remove an issue from a round", Args: exactArgs(1), RunE: runRoundRemove}
var roundStartCmd = &cobra.Command{Use: "start", Short: "Start a round", RunE: runRoundStart}
var roundStatusCmd = &cobra.Command{Use: "status", Short: "Show round status", RunE: runRoundStatus}

func init() {
	roundCmd.AddCommand(roundListCmd, roundCreateCmd, roundAddCmd, roundRemoveCmd, roundStartCmd, roundStatusCmd)
	for _, c := range []*cobra.Command{roundListCmd, roundCreateCmd, roundAddCmd, roundRemoveCmd, roundStartCmd, roundStatusCmd} {
		c.Flags().String("output", "json", "Output format")
	}
	roundCreateCmd.Flags().String("name", "", "Round name (required)")
	roundCreateCmd.Flags().String("schedule-cron", "", "Optional five-field cron schedule")
	roundCreateCmd.Flags().String("timezone", "UTC", "IANA timezone")
	for _, c := range []*cobra.Command{roundAddCmd, roundRemoveCmd, roundStartCmd, roundStatusCmd} {
		c.Flags().String("round", "", "Round ID (required)")
	}
}
func roundID(cmd *cobra.Command) (string, error) {
	v, _ := cmd.Flags().GetString("round")
	v = strings.TrimSpace(v)
	if v == "" {
		return "", fmt.Errorf("--round is required")
	}
	return v, nil
}
func printRound(cmd *cobra.Command, v any) error {
	_, _ = cmd.Flags().GetString("output")
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
func runRoundList(cmd *cobra.Command, args []string) error {
	c, e := newAPIClient(cmd)
	if e != nil {
		return e
	}
	var v map[string]any
	if e = c.GetJSON(context.Background(), "/api/cerebro/rounds", &v); e != nil {
		return e
	}
	return printRound(cmd, v)
}
func runRoundCreate(cmd *cobra.Command, args []string) error {
	c, e := newAPIClient(cmd)
	if e != nil {
		return e
	}
	name, _ := cmd.Flags().GetString("name")
	schedule, _ := cmd.Flags().GetString("schedule-cron")
	timezone, _ := cmd.Flags().GetString("timezone")
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("--name is required")
	}
	var v map[string]any
	e = c.PostJSON(context.Background(), "/api/cerebro/rounds", map[string]any{"name": name, "schedule_cron": schedule, "timezone": timezone}, &v)
	if e != nil {
		return e
	}
	return printRound(cmd, v)
}
func runRoundAdd(cmd *cobra.Command, args []string) error {
	id, e := roundID(cmd)
	if e != nil {
		return e
	}
	c, e := newAPIClient(cmd)
	if e != nil {
		return e
	}
	var v map[string]any
	e = c.PostJSON(context.Background(), "/api/cerebro/rounds/"+url.PathEscape(id)+"/members", map[string]any{"issue_id": args[0]}, &v)
	if e != nil {
		return e
	}
	return printRound(cmd, v)
}
func runRoundRemove(cmd *cobra.Command, args []string) error {
	id, e := roundID(cmd)
	if e != nil {
		return e
	}
	c, e := newAPIClient(cmd)
	if e != nil {
		return e
	}
	if e = c.DeleteJSON(context.Background(), "/api/cerebro/rounds/"+url.PathEscape(id)+"/members/"+url.PathEscape(args[0])); e != nil {
		return e
	}
	return printRound(cmd, map[string]any{"removed": true})
}
func runRoundStart(cmd *cobra.Command, args []string) error {
	id, e := roundID(cmd)
	if e != nil {
		return e
	}
	c, e := newAPIClient(cmd)
	if e != nil {
		return e
	}
	var v map[string]any
	if e = c.PostJSON(context.Background(), "/api/cerebro/rounds/"+url.PathEscape(id)+"/start", map[string]any{}, &v); e != nil {
		return e
	}
	return printRound(cmd, v)
}
func runRoundStatus(cmd *cobra.Command, args []string) error {
	id, e := roundID(cmd)
	if e != nil {
		return e
	}
	c, e := newAPIClient(cmd)
	if e != nil {
		return e
	}
	var v map[string]any
	if e = c.GetJSON(context.Background(), "/api/cerebro/rounds/"+url.PathEscape(id)+"/status", &v); e != nil {
		return e
	}
	return printRound(cmd, v)
}
