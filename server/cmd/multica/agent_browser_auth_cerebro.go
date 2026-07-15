package main

// FIR-3006 — provision Agent Vault credentials into agent-browser's encrypted
// auth vault without placing the password in argv, stdout, stderr, or logs.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

type agentBrowserProvisionRequest struct {
	Vault       string `json:"vault"`
	UsernameKey string `json:"username_key"`
	PasswordKey string `json:"password_key"`
	Host        string `json:"host"`
}

type agentBrowserProvisionAudit struct {
	Vault       string `json:"vault"`
	UsernameKey string `json:"username_key"`
	PasswordKey string `json:"password_key"`
}

type agentBrowserProvisionWireResponse struct {
	Username string                     `json:"username"`
	Password string                     `json:"password"`
	Audit    agentBrowserProvisionAudit `json:"audit"`
}

type agentBrowserProvisionOptions struct {
	Profile     string
	LoginURL    string
	Vault       string
	UsernameKey string
	PasswordKey string
}

type agentBrowserAuthSaver interface {
	Save(ctx context.Context, profile, loginURL, username, password string) error
}

type execAgentBrowserAuthSaver struct{}

func agentBrowserAuthSaveArgs(profile, loginURL, username string) []string {
	return []string{"auth", "save", profile, "--url", loginURL, "--username", username, "--password-stdin"}
}

func (execAgentBrowserAuthSaver) Save(ctx context.Context, profile, loginURL, username, password string) error {
	command := exec.CommandContext(ctx, "agent-browser", agentBrowserAuthSaveArgs(profile, loginURL, username)...)
	command.Stdin = strings.NewReader(password)
	// Never forward child output: a future agent-browser version must not be
	// able to echo supplied credentials into the parent CLI's output.
	if err := command.Run(); err != nil {
		return fmt.Errorf("agent-browser auth save failed")
	}
	return nil
}

func provisionAgentBrowserAuth(ctx context.Context, client *cli.APIClient, saver agentBrowserAuthSaver, out io.Writer, opts agentBrowserProvisionOptions) error {
	var response agentBrowserProvisionWireResponse
	err := client.PostJSON(ctx, "/api/cerebro/agent-browser/provision-auth", agentBrowserProvisionRequest{
		Vault: opts.Vault, UsernameKey: opts.UsernameKey, PasswordKey: opts.PasswordKey, Host: opts.LoginURL,
	}, &response)
	if err != nil {
		return err
	}
	if response.Username == "" || response.Password == "" {
		return fmt.Errorf("Agent Vault returned an incomplete browser credential")
	}
	if err := saver.Save(ctx, opts.Profile, opts.LoginURL, response.Username, response.Password); err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(map[string]any{
		"ok": true, "profile": opts.Profile, "vault": response.Audit.Vault,
		"username_key": response.Audit.UsernameKey, "password_key": response.Audit.PasswordKey,
	})
}

var agentBrowserCmd = &cobra.Command{Use: "agent-browser", Short: "Provision and operate agent-browser with Multica-managed access"}

var agentBrowserProvisionAuthCmd = &cobra.Command{
	Use:   "provision-auth",
	Short: "Provision an Agent Vault login into agent-browser's encrypted auth vault",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		profile, _ := cmd.Flags().GetString("profile-name")
		loginURL, _ := cmd.Flags().GetString("url")
		vault, _ := cmd.Flags().GetString("vault")
		usernameKey, _ := cmd.Flags().GetString("username-key")
		passwordKey, _ := cmd.Flags().GetString("password-key")
		if profile == "" || loginURL == "" || vault == "" || usernameKey == "" || passwordKey == "" {
			return fmt.Errorf("--profile-name, --url, --vault, --username-key, and --password-key are required")
		}
		client, ctx, cancel, err := appClient(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		return provisionAgentBrowserAuth(ctx, client, execAgentBrowserAuthSaver{}, os.Stdout, agentBrowserProvisionOptions{
			Profile: profile, LoginURL: loginURL, Vault: vault, UsernameKey: usernameKey, PasswordKey: passwordKey,
		})
	},
}

func init() {
	agentBrowserProvisionAuthCmd.Flags().String("profile-name", "", "agent-browser auth profile name")
	agentBrowserProvisionAuthCmd.Flags().String("url", "", "Login page URL")
	agentBrowserProvisionAuthCmd.Flags().String("vault", "", "App-specific Agent Vault box (Shared/browser-login/<app>)")
	agentBrowserProvisionAuthCmd.Flags().String("username-key", "", "Credential key containing the login username")
	agentBrowserProvisionAuthCmd.Flags().String("password-key", "", "Credential key containing the login password")
	agentBrowserCmd.AddCommand(agentBrowserProvisionAuthCmd)
}
