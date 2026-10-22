package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/chatgpt"
	"github.com/multica-ai/multica/server/internal/cli"
)

// Keep interactive account management local to the human-owned CLI profile.
// The manager returns only safe account metadata to these commands.
type chatGPTManager interface {
	Login(context.Context, chatgpt.LoginOptions) (chatgpt.Account, error)
	Accounts() ([]chatgpt.Account, error)
	Status() (*chatgpt.Account, error)
	Select(context.Context, string) error
	Logout(context.Context, string) (chatgpt.LogoutResult, error)
	Models(context.Context, string) ([]chatgpt.Model, error)
}

var (
	newChatGPTManager  = func(path string) chatGPTManager { return chatgpt.NewManager(path) }
	openChatGPTBrowser = openBrowser
)

func init() {
	command := newChatGPTCommand()
	command.GroupID = groupRuntime
	rootCmd.AddCommand(command)
}

func newChatGPTCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "chatgpt",
		Short: "Manage local ChatGPT plan access for Codex runtimes",
		Long:  "Sign in with ChatGPT for this computer's Codex runtimes. Credentials stay in the local Multica profile; this does not change your Multica login.",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return requireHumanLocalCommand("chatgpt " + cmd.Name())
		},
	}
	login := &cobra.Command{Use: "login", Short: "Continue with ChatGPT", Args: cobra.NoArgs, RunE: runChatGPTLogin}
	login.Flags().String("account", "", "Reauthorize a saved issued client ID; omit to add an account")
	login.Flags().String("label", "", "A label for this saved account")
	login.Flags().Bool("no-browser", false, "Print the authorization URL instead of opening a browser")
	login.Flags().Bool("reconsent", false, "Request consent again to enable ChatGPT plan usage")
	login.Flags().Duration("timeout", 5*time.Minute, "Maximum time to complete sign-in")
	accounts := &cobra.Command{Use: "accounts", Short: "List saved ChatGPT registrations", Args: cobra.NoArgs, RunE: runChatGPTAccounts}
	status := &cobra.Command{Use: "status", Short: "Show the selected account without refreshing credentials", Args: cobra.NoArgs, RunE: runChatGPTStatus}
	use := &cobra.Command{Use: "use <client-id>", Short: "Select a saved ChatGPT account for Codex", Args: exactArgs(1), RunE: runChatGPTUse}
	logout := &cobra.Command{Use: "logout [client-id]", Short: "Sign out while retaining the account registration", Args: cobra.MaximumNArgs(1), RunE: runChatGPTLogout}
	models := &cobra.Command{Use: "models", Short: "List models available to the selected ChatGPT grant", Args: cobra.NoArgs, RunE: runChatGPTModels}
	disable := &cobra.Command{Use: "disable", Short: "Explicitly restore Codex's own authentication", Args: cobra.NoArgs, RunE: runChatGPTDisable}
	for _, cmd := range []*cobra.Command{accounts, status, models} {
		cmd.Flags().String("output", "table", "Output format: table or json")
	}
	command.AddCommand(login, accounts, status, use, logout, models, disable)
	return command
}

func chatGPTManagerForCommand(cmd *cobra.Command) (chatGPTManager, error) {
	path, err := cli.ChatGPTStorePathForProfile(resolveProfile(cmd))
	if err != nil {
		return nil, err
	}
	return newChatGPTManager(path), nil
}

func saveChatGPTSelection(profile, clientID string) error {
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return err
	}
	if cfg.Backends == nil {
		cfg.Backends = &cli.BackendOverrides{}
	}
	cfg.Backends.Codex = &cli.CodexOverride{AuthMode: cli.CodexAuthModeChatGPTPlan, ChatGPTClientID: clientID}
	return cli.SaveCLIConfigForProfile(cfg, profile)
}

func chatGPTSelectionNotice(cmd *cobra.Command) {
	fmt.Fprintln(cmd.OutOrStdout(), "New Codex runs in this profile use the selected ChatGPT account. Existing runs do not switch accounts; finish or stop them before changing accounts.")
}

func runChatGPTLogin(cmd *cobra.Command, _ []string) error {
	timeout, _ := cmd.Flags().GetDuration("timeout")
	if timeout <= 0 {
		return fmt.Errorf("--timeout must be positive")
	}
	manager, err := chatGPTManagerForCommand(cmd)
	if err != nil {
		return err
	}
	accountID, _ := cmd.Flags().GetString("account")
	label, _ := cmd.Flags().GetString("label")
	reconsent, _ := cmd.Flags().GetBool("reconsent")
	noBrowser, _ := cmd.Flags().GetBool("no-browser")
	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()
	account, err := manager.Login(ctx, chatgpt.LoginOptions{
		ClientID: strings.TrimSpace(accountID), Label: strings.TrimSpace(label), Reconsent: reconsent,
		Authorize: func(authorization chatgpt.Authorization) error {
			if !noBrowser && openChatGPTBrowser(authorization.URL) == nil {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "Complete sign-in in your browser.")
				return err
			}
			if parsed, err := url.Parse(authorization.DisplayURL); err == nil {
				callback, err := url.Parse(parsed.Query().Get("redirect_uri"))
				if err == nil && callback.Scheme == "http" && callback.Hostname() == "127.0.0.1" && callback.Port() != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Callback listener: %s\n", callback.String())
					fmt.Fprintf(cmd.OutOrStdout(), "For a remote runtime, first forward this port from the computer running your browser:\nssh -N -L 127.0.0.1:%s:127.0.0.1:%s <runtime-host>\n", callback.Port(), callback.Port())
				}
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), authorization.DisplayURL)
			return err
		},
	})
	if err != nil {
		return err
	}
	if account.ClientID == "" {
		return fmt.Errorf("ChatGPT sign-in did not return an issued client ID")
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Signed in to ChatGPT: %s [%s].\n", chatGPTAccountLabel(account), account.ClientID)
	if !account.PlanEnabled {
		fmt.Fprintln(cmd.OutOrStdout(), "ChatGPT plan usage is disabled. Reauthorize this account with --account and --reconsent to enable it.")
		return nil
	}
	if err := saveChatGPTSelection(resolveProfile(cmd), account.ClientID); err != nil {
		return fmt.Errorf("signed in, but could not select the local Codex account: %w", err)
	}
	chatGPTSelectionNotice(cmd)
	return nil
}

func chatGPTOutputJSON(cmd *cobra.Command) (bool, error) {
	value, _ := cmd.Flags().GetString("output")
	switch value {
	case "json":
		return true, nil
	case "table":
		return false, nil
	default:
		return false, fmt.Errorf("--output must be table or json")
	}
}

func chatGPTAccountLabel(account chatgpt.Account) string {
	if account.Label != "" {
		return account.Label
	}
	if account.Email != "" {
		return account.Email
	}
	return account.Subject
}

func runChatGPTAccounts(cmd *cobra.Command, _ []string) error {
	jsonOutput, err := chatGPTOutputJSON(cmd)
	if err != nil {
		return err
	}
	manager, err := chatGPTManagerForCommand(cmd)
	if err != nil {
		return err
	}
	accounts, err := manager.Accounts()
	if err != nil {
		return err
	}
	if jsonOutput {
		return cli.PrintJSON(cmd.OutOrStdout(), accounts)
	}
	rows := make([][]string, 0, len(accounts))
	for _, account := range accounts {
		rows = append(rows, []string{account.ClientID, chatGPTAccountLabel(account), fmt.Sprint(account.Active), fmt.Sprint(account.SignedIn), fmt.Sprint(account.PlanEnabled)})
	}
	cli.PrintTable(cmd.OutOrStdout(), []string{"CLIENT ID", "ACCOUNT", "SELECTED", "SIGNED IN", "PLAN ENABLED"}, rows)
	return nil
}

func runChatGPTStatus(cmd *cobra.Command, _ []string) error {
	jsonOutput, err := chatGPTOutputJSON(cmd)
	if err != nil {
		return err
	}
	manager, err := chatGPTManagerForCommand(cmd)
	if err != nil {
		return err
	}
	account, err := manager.Status()
	if err != nil {
		return err
	}
	cfg, err := cli.LoadCLIConfigForProfile(resolveProfile(cmd))
	if err != nil {
		return err
	}
	status := struct {
		AuthMode           string           `json:"auth_mode"`
		ConfiguredClientID string           `json:"configured_client_id,omitempty"`
		Account            *chatgpt.Account `json:"account"`
	}{AuthMode: "codex", Account: account}
	if cfg.Backends != nil && cfg.Backends.Codex != nil && cfg.Backends.Codex.AuthMode != "" {
		status.AuthMode = cfg.Backends.Codex.AuthMode
		status.ConfiguredClientID = cfg.Backends.Codex.ChatGPTClientID
	}
	if jsonOutput {
		return cli.PrintJSON(cmd.OutOrStdout(), status)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Codex authentication: %s\n", status.AuthMode)
	if account == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "No ChatGPT account selected.")
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Account: %s [%s]\nSigned in: %t\nPlan enabled: %t\n", chatGPTAccountLabel(*account), account.ClientID, account.SignedIn, account.PlanEnabled)
	if !account.ExpiresAt.IsZero() {
		fmt.Fprintf(cmd.OutOrStdout(), "Access expires: %s (renewable: %t)\n", account.ExpiresAt.UTC().Format(time.RFC3339), account.CanRefresh)
	}
	return nil
}

func runChatGPTUse(cmd *cobra.Command, args []string) error {
	manager, err := chatGPTManagerForCommand(cmd)
	if err != nil {
		return err
	}
	if err := manager.Select(cmd.Context(), args[0]); err != nil {
		return err
	}
	if err := saveChatGPTSelection(resolveProfile(cmd), args[0]); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Selected ChatGPT registration %s.\n", args[0])
	chatGPTSelectionNotice(cmd)
	return nil
}

func runChatGPTLogout(cmd *cobra.Command, args []string) error {
	manager, err := chatGPTManagerForCommand(cmd)
	if err != nil {
		return err
	}
	clientID := ""
	if len(args) > 0 {
		clientID = args[0]
	}
	result, err := manager.Logout(cmd.Context(), clientID)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Signed out of ChatGPT locally. The Codex authentication mode is unchanged.")
	if !result.RevocationConfirmed {
		return fmt.Errorf("remote revocation was not confirmed; disconnect the app in ChatGPT Settings: https://chatgpt.com/settings")
	}
	return nil
}

func runChatGPTModels(cmd *cobra.Command, _ []string) error {
	jsonOutput, err := chatGPTOutputJSON(cmd)
	if err != nil {
		return err
	}
	manager, err := chatGPTManagerForCommand(cmd)
	if err != nil {
		return err
	}
	account, err := manager.Status()
	if err != nil {
		return err
	}
	if account == nil {
		return fmt.Errorf("sign in with ChatGPT before listing models")
	}
	models, err := manager.Models(cmd.Context(), account.ClientID)
	if err != nil {
		return err
	}
	if jsonOutput {
		return cli.PrintJSON(cmd.OutOrStdout(), models)
	}
	rows := make([][]string, 0, len(models))
	for _, model := range models {
		rows = append(rows, []string{model.Slug, model.DisplayName})
	}
	cli.PrintTable(cmd.OutOrStdout(), []string{"MODEL", "NAME"}, rows)
	return nil
}

func runChatGPTDisable(cmd *cobra.Command, _ []string) error {
	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return err
	}
	if cfg.Backends != nil {
		cfg.Backends.Codex = nil
	}
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "New Codex runs use Codex's own configured authentication. Existing runs do not change. Saved ChatGPT accounts remain; use logout to revoke one.")
	return nil
}
