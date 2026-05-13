package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// RFC #1964 — outbound webhooks CLI surface. Mirrors the autopilot command
// shape: `multica webhook create / list / get / update / delete / deliveries
// / test / rotate-secret`. The secret travels back ONCE on create + rotate
// and is never returned again — the user is expected to capture it from
// stdout into their configured receiver.

var webhookCmd = &cobra.Command{
	Use:   "webhook",
	Short: "Manage outbound webhook subscriptions on bus events",
	Long: `Outbound HTTP webhooks let external systems (Teams, Slack, n8n, dashboards,
custom services) react to Multica events without polling and without burning
LLM tokens via orchestrator-driven writebacks. Each subscription is a workspace-
scoped URL + HMAC secret + event filter. See RFC #1964 for the full design.`,
}

var webhookListCmd = &cobra.Command{
	Use:   "list",
	Short: "List webhook subscriptions in the workspace",
	RunE:  runWebhookList,
}

var webhookGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get webhook subscription details",
	Args:  exactArgs(1),
	RunE:  runWebhookGet,
}

var webhookCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new webhook subscription (returns the secret ONCE)",
	RunE:  runWebhookCreate,
}

var webhookUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a webhook subscription",
	Args:  exactArgs(1),
	RunE:  runWebhookUpdate,
}

var webhookDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a webhook subscription",
	Args:  exactArgs(1),
	RunE:  runWebhookDelete,
}

var webhookRotateSecretCmd = &cobra.Command{
	Use:   "rotate-secret <id>",
	Short: "Rotate the HMAC secret (returns the new secret ONCE)",
	Args:  exactArgs(1),
	RunE:  runWebhookRotateSecret,
}

var webhookTestCmd = &cobra.Command{
	Use:   "test <id>",
	Short: "Enqueue a synthetic webhook:test event to verify connectivity",
	Args:  exactArgs(1),
	RunE:  runWebhookTest,
}

var webhookDeliveriesCmd = &cobra.Command{
	Use:   "deliveries <id>",
	Short: "List recent delivery attempts for a subscription",
	Args:  exactArgs(1),
	RunE:  runWebhookDeliveries,
}

func init() {
	webhookCmd.PersistentFlags().String("output", "table", "Output format: table or json")

	webhookCreateCmd.Flags().String("name", "", "Human-readable name (required)")
	webhookCreateCmd.Flags().String("url", "", "Receiver URL (required, https:// unless --allow-http)")
	webhookCreateCmd.Flags().StringSlice("events", nil, "Event filter — exact bus event-type strings, comma-separated, or '*' for all")
	webhookCreateCmd.Flags().Bool("allow-http", false, "Permit http:// URLs (SSRF protections still apply)")
	webhookCreateCmd.Flags().Int32("pause-threshold", 5, "Auto-pause after this many consecutive failures")
	webhookCreateCmd.Flags().Int32("timeout-seconds", 10, "Per-attempt HTTP timeout (1-30)")
	webhookCreateCmd.Flags().String("secret-stdin", "", "Read secret from stdin (use - for stdin); omit to auto-generate")
	webhookCreateCmd.Flags().String("secret-file", "", "Read secret from file path; omit to auto-generate")

	webhookUpdateCmd.Flags().String("name", "", "New name")
	webhookUpdateCmd.Flags().String("url", "", "New URL")
	webhookUpdateCmd.Flags().StringSlice("events", nil, "Replace event filter (use literal '*' for all)")
	webhookUpdateCmd.Flags().String("state", "", "New state: active|paused|disabled")
	webhookUpdateCmd.Flags().Int32("pause-threshold", 0, "New pause threshold")
	webhookUpdateCmd.Flags().Int32("timeout-seconds", 0, "New per-attempt timeout")

	webhookDeliveriesCmd.Flags().Int("limit", 50, "Max deliveries to return (1-500)")
	webhookDeliveriesCmd.Flags().Bool("include-body", false, "Include last_response_body_truncated in output (off by default for scannable listings)")

	webhookCmd.AddCommand(
		webhookListCmd, webhookGetCmd, webhookCreateCmd, webhookUpdateCmd,
		webhookDeleteCmd, webhookRotateSecretCmd, webhookTestCmd, webhookDeliveriesCmd,
	)
}

func runWebhookList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var resp map[string]any
	if err := client.GetJSON(ctx, "/api/webhooks", &resp); err != nil {
		return fmt.Errorf("list webhooks: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	hooks, _ := resp["webhooks"].([]any)
	headers := []string{"ID", "NAME", "STATE", "URL", "EVENTS", "FAILS"}
	rows := make([][]string, 0, len(hooks))
	for _, h := range hooks {
		w, _ := h.(map[string]any)
		rows = append(rows, []string{
			shortenID(strVal(w, "id"), 8),
			strVal(w, "name"),
			strVal(w, "state"),
			truncate(strVal(w, "url"), 40),
			joinEvents(w["event_filter"]),
			fmt.Sprintf("%v", w["consecutive_failures"]),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runWebhookGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var resp map[string]any
	if err := client.GetJSON(ctx, "/api/webhooks/"+args[0], &resp); err != nil {
		return fmt.Errorf("get webhook: %w", err)
	}
	return cli.PrintJSON(os.Stdout, resp)
}

func runWebhookCreate(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	name, _ := cmd.Flags().GetString("name")
	url, _ := cmd.Flags().GetString("url")
	events, _ := cmd.Flags().GetStringSlice("events")
	allowHTTP, _ := cmd.Flags().GetBool("allow-http")
	pause, _ := cmd.Flags().GetInt32("pause-threshold")
	timeoutSec, _ := cmd.Flags().GetInt32("timeout-seconds")
	stdinFlag, _ := cmd.Flags().GetString("secret-stdin")
	fileFlag, _ := cmd.Flags().GetString("secret-file")

	if name == "" {
		return fmt.Errorf("--name is required")
	}
	if url == "" {
		return fmt.Errorf("--url is required")
	}
	if len(events) == 0 {
		return fmt.Errorf("--events is required (e.g. --events 'task:completed,task:failed' or --events '*')")
	}

	body := map[string]any{
		"name":         name,
		"url":          url,
		"event_filter": events,
	}
	if allowHTTP {
		body["allow_http"] = true
	}
	if pause > 0 && pause != 5 {
		body["pause_threshold"] = pause
	}
	if timeoutSec > 0 && timeoutSec != 10 {
		body["per_attempt_timeout_seconds"] = timeoutSec
	}

	// secret-stdin / secret-file mirror the agent --custom-env-stdin /
	// --custom-env-file pattern. Empty means "auto-generate server-side".
	if stdinFlag != "" {
		secret, err := readSecretInput(stdinFlag)
		if err != nil {
			return fmt.Errorf("read secret: %w", err)
		}
		body["secret"] = secret
	} else if fileFlag != "" {
		bb, err := os.ReadFile(fileFlag)
		if err != nil {
			return fmt.Errorf("read secret file: %w", err)
		}
		body["secret"] = strings.TrimSpace(string(bb))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var resp map[string]any
	if err := client.PostJSON(ctx, "/api/webhooks", body, &resp); err != nil {
		return fmt.Errorf("create webhook: %w", err)
	}

	// CRITICAL: secret is in the response ONCE. Print it prominently.
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  ⚠  Save this secret now. It is NOT recoverable from any future API call.")
	fmt.Fprintln(os.Stderr, "")
	return cli.PrintJSON(os.Stdout, resp)
}

func runWebhookUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	body := map[string]any{}
	if v, _ := cmd.Flags().GetString("name"); v != "" {
		body["name"] = v
	}
	if v, _ := cmd.Flags().GetString("url"); v != "" {
		body["url"] = v
	}
	if v, _ := cmd.Flags().GetStringSlice("events"); len(v) > 0 {
		body["event_filter"] = v
	}
	if v, _ := cmd.Flags().GetString("state"); v != "" {
		body["state"] = v
	}
	if v, _ := cmd.Flags().GetInt32("pause-threshold"); v > 0 {
		body["pause_threshold"] = v
	}
	if v, _ := cmd.Flags().GetInt32("timeout-seconds"); v > 0 {
		body["per_attempt_timeout_seconds"] = v
	}
	if len(body) == 0 {
		return fmt.Errorf("nothing to update — pass at least one flag")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var resp map[string]any
	if err := client.PatchJSON(ctx, "/api/webhooks/"+args[0], body, &resp); err != nil {
		return fmt.Errorf("update webhook: %w", err)
	}
	return cli.PrintJSON(os.Stdout, resp)
}

func runWebhookDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.DeleteJSON(ctx, "/api/webhooks/"+args[0]); err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	fmt.Println("Webhook deleted:", args[0])
	return nil
}

func runWebhookRotateSecret(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var resp map[string]any
	if err := client.PostJSON(ctx, "/api/webhooks/"+args[0]+"/rotate-secret", nil, &resp); err != nil {
		return fmt.Errorf("rotate secret: %w", err)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  ⚠  Save the rotated secret now. It is NOT recoverable from any future API call.")
	fmt.Fprintln(os.Stderr, "")
	return cli.PrintJSON(os.Stdout, resp)
}

func runWebhookTest(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var resp map[string]any
	if err := client.PostJSON(ctx, "/api/webhooks/"+args[0]+"/test", nil, &resp); err != nil {
		return fmt.Errorf("test webhook: %w", err)
	}
	return cli.PrintJSON(os.Stdout, resp)
}

func runWebhookDeliveries(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	limit, _ := cmd.Flags().GetInt("limit")
	includeBody, _ := cmd.Flags().GetBool("include-body")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := fmt.Sprintf("/api/webhooks/%s/deliveries?limit=%d", args[0], limit)
	if includeBody {
		path += "&include_body=true"
	}
	var resp map[string]any
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return fmt.Errorf("list deliveries: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	deliveries, _ := resp["deliveries"].([]any)
	headers := []string{"DELIVERY_ID", "EVENT_TYPE", "STATUS", "ATTEMPT", "HTTP", "CREATED"}
	rows := make([][]string, 0, len(deliveries))
	for _, d := range deliveries {
		dd, _ := d.(map[string]any)
		rows = append(rows, []string{
			shortenID(strVal(dd, "id"), 8),
			strVal(dd, "event_type"),
			strVal(dd, "status"),
			fmt.Sprintf("%v", dd["attempt"]),
			fmt.Sprintf("%v", dd["last_response_status"]),
			strVal(dd, "created_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// readSecretInput accepts "-" for stdin or a literal value. Mirrors the
// --custom-env-stdin convention from cmd_agent.go where "-" reads stdin.
func readSecretInput(arg string) (string, error) {
	if arg == "-" {
		bb, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(bb)), nil
	}
	return strings.TrimSpace(arg), nil
}

func shortenID(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func joinEvents(v any) string {
	arr, ok := v.([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			parts = append(parts, s)
		}
	}
	if len(parts) > 3 {
		return strings.Join(parts[:3], ",") + fmt.Sprintf(" (+%d)", len(parts)-3)
	}
	return strings.Join(parts, ",")
}
