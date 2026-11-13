package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Service accounts (MUL-9163) are workspace-scoped machine identities for
// daemons and CI: a user row the workspace owner/admin mints together with an
// ordinary mul_ PAT. The CLI wraps the owner/admin endpoints; the credential
// behaves like any CLI token but acts only inside its one workspace.

var serviceAccountCmd = &cobra.Command{
	Use:   "service-account",
	Short: "Manage workspace service accounts (machine identities)",
	Long: "Service accounts are machine identities scoped to exactly one workspace. " +
		"Enroll a daemon, CI runner, or bot with its own credential instead of a " +
		"person's PAT: everything it does is credited to the service account, and " +
		"it only ever sees the workspace that minted it.\n\n" +
		"Minting and revoking credentials requires owner or admin. The raw token " +
		"is shown once at creation — store it before leaving.",
}

var serviceAccountCreateCmd = &cobra.Command{
	Use:   "create --name <name> [--expires-in-days N] [workspace-id|slug|prefix]",
	Short: "Create a service account and mint its first credential",
	Long: "Creates a service account in the workspace, adds it as a member, and " +
		"mints its first mul_ credential. The token is printed once; store it in " +
		"your secret manager before running anything with it. Optional workspace " +
		"argument accepts a full UUID, a slug, or a short UUID prefix (≥4 hex " +
		"chars); if omitted the current default workspace is used.",
	Example: "  multica service-account create --name \"ci-runner\" --slug prod",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runServiceAccountCreate,
}

var serviceAccountListCmd = &cobra.Command{
	Use:   "list [workspace-id|slug|prefix]",
	Short: "List the workspace's service accounts and token state",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runServiceAccountList,
}

var serviceAccountTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Mint or revoke credentials for a service account",
	Long: "Rotation without downtime: mint a replacement credential, deploy it, " +
		"then revoke the old one. Only workspace owner/admin can do either.",
}

var serviceAccountTokenMintCmd = &cobra.Command{
	Use:   "mint <account-id> [workspace-id|slug|prefix]",
	Short: "Mint an additional credential for a service account",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runServiceAccountTokenMint,
}

var serviceAccountTokenRevokeCmd = &cobra.Command{
	Use:   "revoke <token-id> [workspace-id|slug|prefix]",
	Short: "Revoke one of a service account's credentials",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runServiceAccountTokenRevoke,
}

func init() {
	serviceAccountCreateCmd.Flags().String("name", "", "Display name for the service account (required)")
	serviceAccountCreateCmd.Flags().Int("expires-in-days", 0, "Token expiry in days (0 or omitted = never expires)")
	serviceAccountTokenMintCmd.Flags().Int("expires-in-days", 0, "Token expiry in days (0 or omitted = never expires)")
	serviceAccountCmd.AddCommand(serviceAccountCreateCmd)
	serviceAccountCmd.AddCommand(serviceAccountListCmd)
	serviceAccountCmd.AddCommand(serviceAccountTokenCmd)
	serviceAccountTokenCmd.AddCommand(serviceAccountTokenMintCmd)
	serviceAccountTokenCmd.AddCommand(serviceAccountTokenRevokeCmd)
}

type serviceAccountListItem struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Email        string  `json:"email"`
	CreatedAt    string  `json:"created_at"`
	ActiveTokens int64   `json:"active_tokens"`
	LastUsedAt   *string `json:"last_used_at"`
}

type serviceAccountIssued struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	CreatedAt    string `json:"created_at"`
	Token        string `json:"token"`
	ActiveTokens int64  `json:"active_tokens"`
}

func printIssuedServiceAccount(cmd *cobra.Command, issued serviceAccountIssued) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, issued)
	}
	cli.PrintTable(os.Stdout,
		[]string{"ID", "NAME", "EMAIL", "TOKEN"},
		[][]string{{issued.ID, issued.Name, issued.Email, issued.Token}},
	)
	fmt.Fprintln(os.Stderr, "Store this token now — it is not shown again.")
	return nil
}

func serviceAccountWorkspaceID(cmd *cobra.Command, args []string) (string, error) {
	wsID, err := resolveWorkspaceArg(cmd, args)
	if err != nil {
		return "", err
	}
	if wsID == "" {
		return "", fmt.Errorf("workspace ID is required: pass an id/slug/prefix as argument or set MULTICA_WORKSPACE_ID")
	}
	return wsID, nil
}

func serviceAccountPath(wsID string) string {
	return "/api/workspaces/" + wsID + "/service-accounts"
}

func runServiceAccountCreate(cmd *cobra.Command, args []string) error {
	wsID, err := serviceAccountWorkspaceID(cmd, args)
	if err != nil {
		return err
	}
	name, _ := cmd.Flags().GetString("name")
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("--name is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"name": name}
	if days, _ := cmd.Flags().GetInt("expires-in-days"); days > 0 {
		body["expires_in_days"] = days
	}

	var issued serviceAccountIssued
	if err := client.PostJSON(ctx, serviceAccountPath(wsID), body, &issued); err != nil {
		return fmt.Errorf("create service account: %w", err)
	}
	return printIssuedServiceAccount(cmd, issued)
}

func runServiceAccountList(cmd *cobra.Command, args []string) error {
	wsID, err := serviceAccountWorkspaceID(cmd, args)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var accounts []serviceAccountListItem
	if err := client.GetJSON(ctx, serviceAccountPath(wsID), &accounts); err != nil {
		return fmt.Errorf("list service accounts: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, accounts)
	}

	headers := []string{"ID", "NAME", "EMAIL", "ACTIVE TOKENS", "LAST USED"}
	rows := make([][]string, 0, len(accounts))
	for _, a := range accounts {
		lastUsed := ""
		if a.LastUsedAt != nil {
			lastUsed = *a.LastUsedAt
		}
		rows = append(rows, []string{
			a.ID,
			a.Name,
			a.Email,
			strconv.FormatInt(a.ActiveTokens, 10),
			lastUsed,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runServiceAccountTokenMint(cmd *cobra.Command, args []string) error {
	wsID, err := serviceAccountWorkspaceID(cmd, args[1:])
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{}
	if days, _ := cmd.Flags().GetInt("expires-in-days"); days > 0 {
		body["expires_in_days"] = days
	}

	var issued serviceAccountIssued
	if err := client.PostJSON(ctx, serviceAccountPath(wsID)+"/"+args[0]+"/tokens", body, &issued); err != nil {
		return fmt.Errorf("mint service account token: %w", err)
	}
	return printIssuedServiceAccount(cmd, issued)
}

func runServiceAccountTokenRevoke(cmd *cobra.Command, args []string) error {
	wsID, err := serviceAccountWorkspaceID(cmd, args[1:])
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, serviceAccountPath(wsID)+"/tokens/"+args[0]); err != nil {
		return fmt.Errorf("revoke service account token: %w", err)
	}
	fmt.Fprintln(os.Stdout, "Token revoked.")
	return nil
}
