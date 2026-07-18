package main

// FIR-3006 — provision Agent Vault credentials into agent-browser's encrypted
// auth vault without placing the password in argv, stdout, stderr, or logs.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

type internalBrowserVerifyRequest struct {
	App string `json:"app"`
}

type internalBrowserVerifyResponse struct {
	App           string   `json:"app"`
	InternalHost  string   `json:"internal_host"`
	FinalURL      string   `json:"final_url"`
	Markers       []string `json:"markers"`
	Errors        []string `json:"errors"`
	ScreenshotPNG []byte   `json:"screenshot_png"`
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

func verifyInternalAgentBrowser(ctx context.Context, client *cli.APIClient, out io.Writer, app, screenshotPath string) error {
	var response internalBrowserVerifyResponse
	if err := client.PostJSON(ctx, "/api/cerebro/agent-browser/internal-verify", internalBrowserVerifyRequest{App: app}, &response); err != nil {
		return err
	}
	if !bytes.HasPrefix(response.ScreenshotPNG, []byte("\x89PNG\r\n\x1a\n")) {
		return fmt.Errorf("internal browser verification returned an invalid screenshot")
	}
	absPath, err := filepath.Abs(screenshotPath)
	if err != nil {
		return fmt.Errorf("resolve screenshot path")
	}
	file, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("write screenshot")
	}
	if _, err := file.Write(response.ScreenshotPNG); err != nil {
		_ = file.Close()
		_ = os.Remove(absPath)
		return fmt.Errorf("write screenshot")
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(absPath)
		return fmt.Errorf("write screenshot")
	}
	if err := os.Chmod(absPath, 0o600); err != nil {
		_ = os.Remove(absPath)
		return fmt.Errorf("secure screenshot")
	}
	return json.NewEncoder(out).Encode(map[string]any{
		"app": response.App, "internal_host": response.InternalHost, "final_url": response.FinalURL,
		"markers": response.Markers, "errors": response.Errors, "screenshot": absPath,
	})
}

const internalBrowserVerifyTimeout = 2 * time.Minute

func configureInternalBrowserVerifyClient(client *cli.APIClient) {
	client.HTTPClient.Timeout = internalBrowserVerifyTimeout
}

var agentBrowserInternalVerifyCmd = &cobra.Command{
	Use:   "internal-verify",
	Short: "Verify an allowlisted app through its Sliplane internal host",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		app, _ := cmd.Flags().GetString("app")
		if app == "" {
			return fmt.Errorf("--app is required")
		}
		screenshotPath, _ := cmd.Flags().GetString("screenshot")
		if screenshotPath == "" {
			screenshotPath = fmt.Sprintf("%s-internal-verify.png", app)
		}
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		configureInternalBrowserVerifyClient(client)
		ctx, cancel := context.WithTimeout(cmd.Context(), internalBrowserVerifyTimeout)
		defer cancel()
		return verifyInternalAgentBrowser(ctx, client, os.Stdout, app, screenshotPath)
	},
}

func init() {
	agentBrowserProvisionAuthCmd.Flags().String("profile-name", "", "agent-browser auth profile name")
	agentBrowserProvisionAuthCmd.Flags().String("url", "", "Login page URL")
	agentBrowserProvisionAuthCmd.Flags().String("vault", "", "App-specific Agent Vault box (Shared/browser-login/<app>)")
	agentBrowserProvisionAuthCmd.Flags().String("username-key", "", "Credential key containing the login username")
	agentBrowserProvisionAuthCmd.Flags().String("password-key", "", "Credential key containing the login password")
	agentBrowserCmd.AddCommand(agentBrowserProvisionAuthCmd)
	agentBrowserInternalVerifyCmd.Flags().String("app", "", "Allowlisted app: multica, cerebro, registry, finance, pricing, or customer-service")
	agentBrowserInternalVerifyCmd.Flags().String("screenshot", "", "PNG output path (default: <app>-internal-verify.png)")
	agentBrowserCmd.AddCommand(agentBrowserInternalVerifyCmd)
}
