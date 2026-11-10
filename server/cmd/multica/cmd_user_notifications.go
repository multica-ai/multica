package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

const notificationPreferencesPath = "/api/notification-preferences"

// focusedNotificationFlags maps each CLI flag to its preference key, in the
// order the table prints them.
var focusedNotificationFlags = []struct {
	flag  string
	key   string
	label string
	usage string
}{
	{"assigned", "focused_assigned", "ASSIGNED", "Notify when you are made the assignee"},
	{"blocked", "focused_blocked", "BLOCKED", "Notify when an issue assigned to you moves to Blocked"},
	{"mentioned", "focused_mentioned", "MENTIONED", "Notify when you are @mentioned directly in a comment (not @all or a squad)"},
	{"thread-reply", "focused_thread_reply", "THREAD REPLY", "Notify when someone replies in a comment thread you started"},
	{"agent-activity", "focused_agent_activity", "AGENT ACTIVITY", "Notify when an agent comments on or changes the status of an issue assigned to you"},
}

var userNotificationsCmd = &cobra.Command{
	Use:   "notifications",
	Short: "Get or set your inbox notification filter for the current workspace",
	Long: "Manage which events create inbox notifications for you in the current workspace.\n\n" +
		"Settings are per workspace and apply to new notifications only: existing inbox items " +
		"are not recomputed, and your issue subscriptions are not changed.\n\n" +
		"Modes:\n" +
		"  all       every notification your subscriptions produce, minus muted groups (default)\n" +
		"  focused   only the events enabled with the focused flags (--assigned, --blocked,\n" +
		"            --mentioned, --thread-reply, --agent-activity); muted groups are ignored",
}

var userNotificationsGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Show your inbox notification filter",
	Args:  cobra.NoArgs,
	RunE:  runUserNotificationsGet,
}

var userNotificationsSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Update your inbox notification filter",
	Long: "Update the inbox notification filter for the current workspace. Only the flags you " +
		"pass are sent; everything else keeps its current value.\n\n" +
		"The setting is per workspace and applies to new notifications only. It does not change " +
		"which issues you are subscribed to.\n\n" +
		"The focused flags (--assigned, --blocked, --mentioned, --thread-reply, --agent-activity) " +
		"only take effect while --mode is focused; in all mode they are stored but ignored.",
	Example: "  # Only get notified about what needs you\n" +
		"  multica user notifications set --mode focused\n\n" +
		"  # Stay focused but skip the Blocked status notifications\n" +
		"  multica user notifications set --mode focused --blocked=false\n\n" +
		"  # Go back to receiving everything\n" +
		"  multica user notifications set --mode all",
	Args: cobra.NoArgs,
	RunE: runUserNotificationsSet,
}

func init() {
	userCmd.AddCommand(userNotificationsCmd)
	userNotificationsCmd.AddCommand(userNotificationsGetCmd)
	userNotificationsCmd.AddCommand(userNotificationsSetCmd)

	userNotificationsGetCmd.Flags().String("output", "table", "Output format: table or json")

	userNotificationsSetCmd.Flags().String("mode", "", "Inbox mode: all or focused")
	for _, f := range focusedNotificationFlags {
		userNotificationsSetCmd.Flags().Bool(f.flag, false, f.usage+" (focused mode; use --"+f.flag+"=false to turn off)")
	}
	userNotificationsSetCmd.Flags().String("output", "table", "Output format: table or json")
}

func runUserNotificationsGet(cmd *cobra.Command, _ []string) error {
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.GetJSON(ctx, notificationPreferencesPath, &result); err != nil {
		return fmt.Errorf("get notification preferences: %w", err)
	}
	return printNotificationPreferences(cmd, result)
}

func runUserNotificationsSet(cmd *cobra.Command, _ []string) error {
	prefs := map[string]string{}
	if cmd.Flags().Changed("mode") {
		mode, _ := cmd.Flags().GetString("mode")
		mode = strings.ToLower(strings.TrimSpace(mode))
		if mode != "all" && mode != "focused" {
			return fmt.Errorf("invalid --mode %q: must be all or focused", mode)
		}
		prefs["inbox_mode"] = mode
	}
	for _, f := range focusedNotificationFlags {
		if !cmd.Flags().Changed(f.flag) {
			continue
		}
		on, _ := cmd.Flags().GetBool(f.flag)
		prefs[f.key] = onOff(on)
	}
	if len(prefs) == 0 {
		return fmt.Errorf("nothing to update; pass --mode or at least one of --assigned, --blocked, --mentioned, --thread-reply, --agent-activity")
	}

	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	body := map[string]any{"preferences": prefs}
	if err := client.PatchJSON(ctx, notificationPreferencesPath, body, &result); err != nil {
		return fmt.Errorf("update notification preferences: %w", err)
	}
	return printNotificationPreferences(cmd, result)
}

func printNotificationPreferences(cmd *cobra.Command, result map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	prefs, _ := result["preferences"].(map[string]any)
	printNotificationPreferencesTable(os.Stdout, prefs)
	return nil
}

// printNotificationPreferencesTable renders absent keys with their server
// defaults (mode all, every focused event on).
func printNotificationPreferencesTable(out io.Writer, prefs map[string]any) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	defer w.Flush()

	mode := strVal(prefs, "inbox_mode")
	if mode == "" {
		mode = "all"
	}
	fmt.Fprintf(w, "MODE\t%s\n", mode)
	for _, f := range focusedNotificationFlags {
		state := "ON"
		if strVal(prefs, f.key) == "off" {
			state = "OFF"
		}
		fmt.Fprintf(w, "%s\t%s\n", f.label, state)
	}
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
