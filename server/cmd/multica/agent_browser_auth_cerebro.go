package main

// FIR-3006 — provision Agent Vault credentials into agent-browser's encrypted
// auth vault without placing the password in argv, stdout, stderr, or logs.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/internalbrowserqa"
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
	// Page is omitted when unset so this CLI keeps working against a server
	// build that predates the flag: the server rejects unknown fields.
	Page  string `json:"page,omitempty"`
	Async bool   `json:"async,omitempty"`
}

type internalBrowserVerifyResponse struct {
	JobID         string   `json:"job_id"`
	State         string   `json:"state"`
	Error         string   `json:"error"`
	App           string   `json:"app"`
	InternalHost  string   `json:"internal_host"`
	FinalURL      string   `json:"final_url"`
	Markers       []string `json:"markers"`
	Errors        []string `json:"errors"`
	ScreenshotPNG []byte   `json:"screenshot_png"`
	VersionCommit string   `json:"version_commit"`
	FailureStage  string   `json:"failure_stage"`
	FailureCause  string   `json:"failure_cause"`
	FailureDetail string   `json:"failure_detail"`
}

// internalBrowserFailureHint turns a stage and cause into the one sentence a
// reader actually needs. An unrecognised pair falls through to stage + cause
// rather than being swallowed, so a new cause is never silently hidden.
func internalBrowserFailureHint(stage, cause string) string {
	switch cause {
	case "dns":
		return "the internal address does not exist — check the hostname"
	case "dns-timeout":
		return "the internal address could not be looked up in time — check the hostname and private DNS"
	case "connection":
		return "the address resolved but nothing accepted the connection — check the port and that the app is running"
	case "browser-launch":
		return "the verifier could not start its browser"
	case "not-found":
		if stage == "markers" {
			return "the page loaded but the expected text was missing — check login and the page content"
		}
		return "the element the verifier looked for was not on the page"
	case "timeout":
		return "the app resolved and connected but answered too slowly"
	}
	if stage == "" && cause == "" {
		return ""
	}
	return "failed at stage " + stage + " (" + cause + ")"
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

func verifyInternalAgentBrowser(ctx context.Context, client *cli.APIClient, out io.Writer, app, page, screenshotPath string) error {
	var response internalBrowserVerifyResponse
	status, err := client.PostJSONWithDiagnostic(ctx,
		"/api/cerebro/agent-browser/internal-verify",
		internalBrowserVerifyRequest{App: app, Page: page, Async: true}, &response, http.StatusUnprocessableEntity)
	if err != nil {
		return err
	}
	if status == http.StatusAccepted {
		if response.JobID == "" {
			return fmt.Errorf("internal browser verification returned an invalid job")
		}
		response, status, err = pollInternalBrowserVerification(ctx, client, response.JobID)
		if err != nil {
			return err
		}
	}
	if status == http.StatusUnprocessableEntity {
		return reportInternalBrowserFailure(out, response, screenshotPath)
	}
	if !bytes.HasPrefix(response.ScreenshotPNG, []byte("\x89PNG\r\n\x1a\n")) {
		return fmt.Errorf("internal browser verification returned an invalid screenshot")
	}
	absPath, err := writeInternalBrowserScreenshot(response.ScreenshotPNG, screenshotPath)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(map[string]any{
		"app": response.App, "internal_host": response.InternalHost, "final_url": response.FinalURL,
		"markers": response.Markers, "errors": response.Errors, "screenshot": absPath,
		"version_commit": response.VersionCommit,
	})
}

func pollInternalBrowserVerification(ctx context.Context, client *cli.APIClient, jobID string) (internalBrowserVerifyResponse, int, error) {
	path := "/api/cerebro/agent-browser/internal-verify/" + jobID
	for {
		var response internalBrowserVerifyResponse
		status, err := client.GetJSONWithDiagnostic(ctx, path, &response, http.StatusUnprocessableEntity)
		if err != nil {
			return internalBrowserVerifyResponse{}, status, err
		}
		if status != http.StatusAccepted {
			return response, status, nil
		}
		select {
		case <-ctx.Done():
			return internalBrowserVerifyResponse{}, 0, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func writeInternalBrowserScreenshot(screenshot []byte, screenshotPath string) (string, error) {
	absPath, err := filepath.Abs(screenshotPath)
	if err != nil {
		return "", fmt.Errorf("resolve screenshot path")
	}
	file, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("write screenshot")
	}
	if _, err := file.Write(screenshot); err != nil {
		_ = file.Close()
		_ = os.Remove(absPath)
		return "", fmt.Errorf("write screenshot")
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(absPath)
		return "", fmt.Errorf("write screenshot")
	}
	if err := os.Chmod(absPath, 0o600); err != nil {
		_ = os.Remove(absPath)
		return "", fmt.Errorf("secure screenshot")
	}
	return absPath, nil
}

// reportInternalBrowserFailure prints the diagnostics for a failed verification
// and saves the failure screenshot when the verifier managed to take one, then
// returns a non-nil error so the command still exits non-zero.
func reportInternalBrowserFailure(out io.Writer, response internalBrowserVerifyResponse, screenshotPath string) error {
	report := map[string]any{
		"ok": false, "app": response.App, "error": response.Error,
		"internal_host": response.InternalHost, "attempted": response.FailureDetail,
		"failure_stage": response.FailureStage, "failure_cause": response.FailureCause,
	}
	if hint := internalBrowserFailureHint(response.FailureStage, response.FailureCause); hint != "" {
		report["hint"] = hint
	}
	if bytes.HasPrefix(response.ScreenshotPNG, []byte("\x89PNG\r\n\x1a\n")) {
		if absPath, err := writeInternalBrowserScreenshot(response.ScreenshotPNG, screenshotPath); err == nil {
			report["screenshot"] = absPath
		}
	}
	if err := json.NewEncoder(out).Encode(report); err != nil {
		return err
	}
	message := response.Error
	if message == "" {
		message = "internal browser verification failed"
	}
	return fmt.Errorf("%s", message)
}

// The caller must outlast the verifier's own worst case, plus a margin for the
// request itself. At two minutes it did not: the verifier can spend 155s on the
// open stage alone once the cold-start retry fires, so the CLI hung up mid-retry
// and reported "context deadline exceeded" for apps that were merely idle.
const internalBrowserVerifyTimeout = internalbrowserqa.MaxVerificationDuration + time.Minute

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
		page, _ := cmd.Flags().GetString("page")
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
		return verifyInternalAgentBrowser(ctx, client, os.Stdout, app, page, screenshotPath)
	},
}

func init() {
	agentBrowserProvisionAuthCmd.Flags().String("profile-name", "", "agent-browser auth profile name")
	agentBrowserProvisionAuthCmd.Flags().String("url", "", "Login page URL")
	agentBrowserProvisionAuthCmd.Flags().String("vault", "", "App-specific Agent Vault box (Shared/browser-login/<app>)")
	agentBrowserProvisionAuthCmd.Flags().String("username-key", "", "Credential key containing the login username")
	agentBrowserProvisionAuthCmd.Flags().String("password-key", "", "Credential key containing the login password")
	agentBrowserCmd.AddCommand(agentBrowserProvisionAuthCmd)
	agentBrowserInternalVerifyCmd.Flags().String("app", "", "Allowlisted app: multica, multica-permissions, cerebro, cerebro-permission-profiles, registry, finance, pricing, customer-service, warehouse, data-catalog, data-catalog-reader, data-catalog-reader-permissions, or data-catalog-unknown")
	agentBrowserInternalVerifyCmd.Flags().String("page", "", "In-app path to end on after login, e.g. /controls (default: the app's own verification page)")
	agentBrowserInternalVerifyCmd.Flags().String("screenshot", "", "PNG output path (default: <app>-internal-verify.png)")
	agentBrowserCmd.AddCommand(agentBrowserInternalVerifyCmd)
}
