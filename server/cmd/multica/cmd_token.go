package main

import (
	"context"
	"fmt"
	"os"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var tokenCmd = &cobra.Command{Use: "token", Short: "Manage your personal access tokens"}
var tokenListCmd = &cobra.Command{
	Use:   "list",
	Short: "List safe metadata for your personal access tokens",
	Args:  cobra.NoArgs,
	RunE:  runTokenList,
}
var tokenRevokeCmd = &cobra.Command{
	Use: "revoke <token-id>", Short: "Revoke one of your personal access tokens",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		ctx, cancel := cli.APIContext(context.Background())
		defer cancel()
		if err := client.DeleteJSON(ctx, "/api/tokens/"+args[0]); err != nil {
			return err
		}
		_, err = fmt.Fprintln(os.Stdout, "Personal access token revoked.")
		return err
	},
}
var tokenCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a personal access token and save it to a private file",
	Args:  cobra.NoArgs,
	RunE:  runTokenCreate,
}

func init() {
	tokenListCmd.Flags().String("output", "table", "Output format: table or json")
	tokenCreateCmd.Flags().String("name", "", "Token name (required)")
	tokenCreateCmd.Flags().Bool("external-relay", false, "Mark this token for external relay use")
	tokenCreateCmd.Flags().Int("expires-in-days", 0, "Expire the token after this many days (0 means no expiry)")
	tokenCreateCmd.Flags().String("output-file", "", "Write the raw token to this new file (required; mode 0600)")
	_ = tokenCreateCmd.MarkFlagRequired("name")
	_ = tokenCreateCmd.MarkFlagRequired("output-file")
	tokenCmd.AddCommand(tokenListCmd)
	tokenCmd.AddCommand(tokenCreateCmd)
	tokenCmd.AddCommand(tokenRevokeCmd)
	rootCmd.AddCommand(tokenCmd)
}

type tokenCreateAPIResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Token     string  `json:"token"`
	ExpiresAt *string `json:"expires_at"`
}

func runTokenCreate(cmd *cobra.Command, _ []string) (retErr error) {
	name, _ := cmd.Flags().GetString("name")
	outputFile, _ := cmd.Flags().GetString("output-file")
	externalRelay, _ := cmd.Flags().GetBool("external-relay")
	expiresInDays, _ := cmd.Flags().GetInt("expires-in-days")
	if name == "" {
		return fmt.Errorf("token name must not be empty")
	}
	if outputFile == "" {
		return fmt.Errorf("output file must not be empty")
	}
	if expiresInDays < 0 {
		return fmt.Errorf("expires-in-days must be zero or greater")
	}

	// Reserve the destination before issuing a credential, so an existing path
	// cannot cause a successfully-created secret to be lost.
	f, err := os.OpenFile(outputFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create token output file: %w", err)
	}
	keepFile := false
	closed := false
	defer func() {
		if !closed {
			if closeErr := f.Close(); retErr == nil && closeErr != nil {
				retErr = fmt.Errorf("close token output file")
			}
		}
		if !keepFile {
			_ = os.Remove(outputFile)
		}
	}()

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	var expiry *int
	if expiresInDays > 0 {
		expiry = &expiresInDays
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var result tokenCreateAPIResponse
	if err := client.PostJSON(ctx, "/api/tokens", map[string]any{
		"name": name, "is_external_relay": externalRelay, "expires_in_days": expiry,
	}, &result); err != nil {
		return fmt.Errorf("create personal access token: %w", err)
	}
	if result.ID == "" || result.Name == "" || result.Token == "" {
		return fmt.Errorf("create personal access token: server returned an incomplete response")
	}
	if _, err := f.WriteString(result.Token); err != nil {
		return fmt.Errorf("write token output file")
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync token output file")
	}
	if err := f.Close(); err != nil {
		closed = true
		return fmt.Errorf("close token output file")
	}
	closed = true
	keepFile = true
	id := result.ID
	if len(id) > 8 {
		id = id[:8]
	}
	expires := "never"
	if result.ExpiresAt != nil {
		expires = *result.ExpiresAt
	}
	if _, err := fmt.Fprintf(os.Stdout, "ID: %s\nName: %s\nExpires: %s\n", id, result.Name, expires); err != nil {
		return err
	}
	return nil
}

type tokenListAPIItem struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  *string `json:"expires_at"`
}

type tokenListOutputItem struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	CreatedAt string  `json:"created_at"`
	LastUsed  *string `json:"last_used_at"`
	Expires   *string `json:"expires_at"`
}

func runTokenList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var tokens []tokenListAPIItem
	if err := client.GetJSON(ctx, "/api/tokens", &tokens); err != nil {
		return fmt.Errorf("list personal access tokens: %w", err)
	}
	items := make([]tokenListOutputItem, 0, len(tokens))
	for _, token := range tokens {
		id := token.ID
		if len(id) > 8 {
			id = id[:8]
		}
		items = append(items, tokenListOutputItem{
			ID: id, Name: token.Name, CreatedAt: token.CreatedAt,
			LastUsed: token.LastUsedAt, Expires: token.ExpiresAt,
		})
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, items)
	}
	if output != "table" {
		return fmt.Errorf("unsupported output format %q; use table or json", output)
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		lastUsed, expires := "", ""
		if item.LastUsed != nil {
			lastUsed = *item.LastUsed
		}
		if item.Expires != nil {
			expires = *item.Expires
		}
		rows = append(rows, []string{item.ID, item.Name, item.CreatedAt, lastUsed, expires})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "NAME", "CREATED", "LAST USED", "EXPIRES"}, rows)
	return nil
}
