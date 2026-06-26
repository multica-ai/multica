package main

// FIR-2037 — `multica cerebro-browser` — the agent-facing CLI for the in-app
// PERSONAL BROWSER. This is what an agent's Bash tool invokes (taught by the
// `multica-personal-browser` built-in skill) to drive the Multica-owned browser
// pane the user is logged into, inside the desktop app.
//
// It is a cerebro-zone file (`cerebro_*.go`), so it carries no per-line
// CEREBRO-PATCH markers; the single upstream touch is its registration line in
// main.go.
//
// Three gates protect the user's logged-in browser (see
// daemon_personal_browser_cerebro.go and cerebro-browser-control-server.ts):
//   1. Capability (authoritative): the daemon only sets MULTICA_PERSONAL_BROWSER
//      for agents whose `tools:personal-browser` resolves allow/ask. This CLI
//      refuses to run without it — so an ungranted agent cannot act even if it
//      finds the loopback sidecar.
//   2. Loopback + token: the desktop control server binds 127.0.0.1 and requires
//      the bearer token from the 0600 sidecar file.
//   3. Audit: the desktop side logs every action against the real session.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

// personalBrowserGrantEnvCLI mirrors handler.personalBrowserGrantEnv — the grant
// signal the daemon injects only for allowed agents. Duplicated as a const here
// because the CLI must not import the server handler package.
const personalBrowserGrantEnvCLI = "MULTICA_PERSONAL_BROWSER"

const cerebroBrowserClientTimeout = 60 * time.Second

// cerebroBrowserSidecar is the rendezvous file the desktop control server writes
// (apps/desktop/src/main/cerebro-browser-control-server.ts). Same path on both
// sides: ~/.multica/cerebro-browser-control.json.
type cerebroBrowserSidecar struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

func cerebroBrowserSidecarPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".multica", "cerebro-browser-control.json"), nil
}

// callCerebroBrowser POSTs one action to the desktop control server and prints
// the JSON response to stdout (the agent reads it as the tool result).
func callCerebroBrowser(cmd *cobra.Command, action string, payload map[string]any) error {
	if os.Getenv(personalBrowserGrantEnvCLI) == "" {
		return fmt.Errorf(
			"personal browser is not enabled for this workspace — ask an admin to turn on the Browser feature, then allow the 'tools:personal-browser' capability for you in Settings → Permissions",
		)
	}

	// Forward the agent's own token so the desktop control server can ask the
	// Multica server, AS THIS AGENT, whether the action is allowed on the target
	// host. This is the authoritative per-action gate — without a valid agent
	// token the action is denied server-side, so the env check above is only a
	// fast local fail, never the security boundary.
	if payload == nil {
		payload = map[string]any{}
	}
	payload["agentToken"] = os.Getenv("MULTICA_TOKEN")

	path, err := cerebroBrowserSidecarPath()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf(
				"the personal browser is not running — open the Browser tab in the Multica desktop app first",
			)
		}
		return fmt.Errorf("reading personal-browser sidecar: %w", err)
	}
	var side cerebroBrowserSidecar
	if err := json.Unmarshal(raw, &side); err != nil {
		return fmt.Errorf("parsing personal-browser sidecar: %w", err)
	}
	if side.Port == 0 || side.Token == "" {
		return fmt.Errorf("personal-browser sidecar is incomplete — restart the Browser tab")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cerebroBrowserClientTimeout)
	defer cancel()

	url := fmt.Sprintf("http://127.0.0.1:%d/agent/%s", side.Port, action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+side.Token)

	resp, err := (&http.Client{Timeout: cerebroBrowserClientTimeout}).Do(req)
	if err != nil {
		return fmt.Errorf("reaching the personal browser: %w", err)
	}
	defer resp.Body.Close()

	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(out, &e)
		if e.Error != "" {
			return fmt.Errorf("personal browser: %s", e.Error)
		}
		return fmt.Errorf("personal browser returned status %d", resp.StatusCode)
	}

	// Pass the JSON result straight through to the agent.
	fmt.Fprintln(cmd.OutOrStdout(), string(bytes.TrimRight(out, "\n")))
	return nil
}

// sessionFlag returns the --session value (empty → default session server-side).
func sessionFlag(cmd *cobra.Command) string {
	v, _ := cmd.Flags().GetString("session")
	return v
}

func payloadWithSession(cmd *cobra.Command, base map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	if s := sessionFlag(cmd); s != "" {
		base["sessionId"] = s
	}
	return base
}

var cerebroBrowserCmd = &cobra.Command{
	Use:   "cerebro-browser",
	Short: "Drive the in-app personal browser (the Multica-owned browser you are logged into)",
	Long: "Drive the personal browser pane in the Multica desktop app — the same browser, " +
		"with the same saved logins, the user is signed into. Capability-gated " +
		"(tools:personal-browser) and audited. Read a page with `snapshot`, then act " +
		"on elements by their @ref with `click`/`fill`.",
}

func init() {
	cerebroBrowserCmd.PersistentFlags().String("session", "", "Browser session id (omit for the default session)")

	snapshotCmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Read the current page as a ref-based accessibility tree (@e1, @e2, …)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return callCerebroBrowser(cmd, "snapshot", payloadWithSession(cmd, nil))
		},
	}

	clickCmd := &cobra.Command{
		Use:   "click <ref>",
		Short: "Click an element by its accessibility ref (e.g. @e12)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return callCerebroBrowser(cmd, "click", payloadWithSession(cmd, map[string]any{"ref": args[0]}))
		},
	}

	fillCmd := &cobra.Command{
		Use:   "fill <ref> <value>",
		Short: "Type a value into an input/textarea by its ref",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return callCerebroBrowser(cmd, "fill", payloadWithSession(cmd, map[string]any{"ref": args[0], "value": args[1]}))
		},
	}

	navigateCmd := &cobra.Command{
		Use:   "navigate <url>",
		Short: "Load a URL in the (optionally named) session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return callCerebroBrowser(cmd, "navigate", payloadWithSession(cmd, map[string]any{"url": args[0]}))
		},
	}

	sessionsCmd := &cobra.Command{
		Use:   "sessions",
		Short: "List the open browser sessions and which one is active",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return callCerebroBrowser(cmd, "sessions", map[string]any{})
		},
	}

	logoutCmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out of a session — wipe its cookies, storage and cache",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return callCerebroBrowser(cmd, "logout", payloadWithSession(cmd, nil))
		},
	}

	clearCookiesCmd := &cobra.Command{
		Use:   "clear-cookies",
		Short: "Clear only the cookies for a session (lighter than logout)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return callCerebroBrowser(cmd, "clear-cookies", payloadWithSession(cmd, nil))
		},
	}

	cerebroBrowserCmd.AddCommand(snapshotCmd, clickCmd, fillCmd, navigateCmd, sessionsCmd, logoutCmd, clearCookiesCmd)
}
