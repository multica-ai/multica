// CEREBRO-PATCH(cerebro-permission-task-cli): FIR-3388 exposes one task's
// immutable access snapshot through the unified permissions command.
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

type permissionTaskAccess struct {
	EnforcementEnabled   bool     `json:"enforcement_enabled"`
	TaskID               string   `json:"task_id"`
	AgentID              string   `json:"agent_id"`
	AllowedTools         []string `json:"allowed_tools"`
	IssuedAt             string   `json:"issued_at"`
	ExpiresAt            string   `json:"expires_at"`
	Status               string   `json:"status"`
	ClaimGeneration      int64    `json:"claim_generation"`
	LifecycleState       string   `json:"lifecycle_state"`
	Producer             *string  `json:"producer,omitempty"`
	Finalizer            *string  `json:"finalizer,omitempty"`
	InventoryVersion     *string  `json:"inventory_version,omitempty"`
	DiscoveryVersion     *string  `json:"discovery_version,omitempty"`
	OfferedCount         int      `json:"offered_count"`
	AuthorizedCount      int      `json:"authorized_count"`
	FinalizedGrantDigest *string  `json:"grant_digest,omitempty"`
	Verdict              struct {
		Allowed        bool   `json:"allowed"`
		Code           string `json:"code"`
		RecoveryAction string `json:"recovery_action"`
		Message        string `json:"message"`
	} `json:"verdict"`
}

var permissionTaskCmd = &cobra.Command{
	Use:   "task <id>",
	Short: "Show the immutable permission snapshot for one task",
	Long: "Shows the exact tool allowlist captured when a task started and whether " +
		"the workspace currently enforces it.",
	Args: exactArgs(1),
	RunE: runPermissionTask,
}

func init() {
	permissionTaskCmd.Flags().String("output", "table", "Output format: table or json")
	permissionsCmd.AddCommand(permissionTaskCmd)
}

func runPermissionTask(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return showPermissionTask(ctx, client, args[0], output)
}

func showPermissionTask(ctx context.Context, client *cli.APIClient, taskID, output string) error {
	var access permissionTaskAccess
	if err := client.GetJSON(ctx, "/api/tasks/"+url.PathEscape(taskID)+"/access", &access); err != nil {
		return fmt.Errorf("show task access: %w", err)
	}
	if output == "json" {
		return cli.PrintJSON(os.Stdout, access)
	}
	enforcement := "off"
	if access.EnforcementEnabled {
		enforcement = "on"
	}
	rows := make([][]string, 0, len(access.AllowedTools))
	for _, tool := range access.AllowedTools {
		rows = append(rows, []string{tool, enforcement, access.Status, fmt.Sprintf("%d", access.ClaimGeneration), access.LifecycleState, access.Verdict.Code, access.Verdict.RecoveryAction})
	}
	cli.PrintTable(os.Stdout, []string{"ALLOWED TOOL", "ENFORCEMENT", "WINDOW", "GENERATION", "LIFECYCLE", "VERDICT", "RECOVERY"}, rows)
	fmt.Fprintf(os.Stderr, "enforcement=%s verdict=%s recovery=%s offered=%d authorized=%d producer=%s finalizer=%s inventory=%s discovery=%s digest=%s\n",
		enforcement, access.Verdict.Code, access.Verdict.RecoveryAction,
		access.OfferedCount, access.AuthorizedCount, optionalTaskAccessValue(access.Producer), optionalTaskAccessValue(access.Finalizer),
		optionalTaskAccessValue(access.InventoryVersion), optionalTaskAccessValue(access.DiscoveryVersion), optionalTaskAccessValue(access.FinalizedGrantDigest))
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "no tools were allowed for this task")
	}
	return nil
}

func optionalTaskAccessValue(value *string) string {
	if value == nil || *value == "" {
		return "-"
	}
	return *value
}
