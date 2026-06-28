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
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
)

// Desktop app identity, discovered from apps/desktop/electron-builder.yml
// (appId + productName). Used by `open` to launch the app when it is not
// already running. Kept in sync with that file by hand — a CLI build cannot
// read the desktop build config at runtime.
const (
	desktopAppBundleID = "ai.multica.desktop"
	desktopAppName     = "Multica"
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
				"the personal browser is not running — run `multica cerebro-browser open` to start it",
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

// sidecarReady reports whether the desktop control server's sidecar file
// exists and is complete (port + token present). os.IsNotExist and parse
// errors both count as "not ready" — the app simply has not brought the
// transport up yet.
func sidecarReady() bool {
	path, err := cerebroBrowserSidecarPath()
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var side cerebroBrowserSidecar
	if err := json.Unmarshal(raw, &side); err != nil {
		return false
	}
	return side.Port != 0 && side.Token != ""
}

// launchDesktopApp starts the Multica desktop app detached (best-effort). It
// does not block on the child process. Returns an error only when the platform
// has no known way to launch the app.
func launchDesktopApp() error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// Prefer launch-by-bundle-id (robust to where the .app lives); fall back
		// to launch-by-name. `open` returns immediately, so the app is detached.
		if _, err := exec.LookPath("open"); err != nil {
			return fmt.Errorf("cannot launch the Multica desktop app: `open` not found")
		}
		if err := exec.Command("open", "-b", desktopAppBundleID).Start(); err == nil {
			return nil
		}
		cmd = exec.Command("open", "-a", desktopAppName)
	case "windows":
		// Best-effort: ask the shell to start the app by its product name.
		cmd = exec.Command("cmd", "/c", "start", "", desktopAppName)
	case "linux":
		// Best-effort: run a binary named after the product from PATH.
		bin := "multica-desktop"
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf(
				"cannot launch the Multica desktop app automatically on linux — start it manually, then retry",
			)
		}
		cmd = exec.Command(bin)
	default:
		return fmt.Errorf(
			"cannot launch the Multica desktop app automatically on %s — start it manually, then retry",
			runtime.GOOS,
		)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launching the Multica desktop app: %w", err)
	}
	return nil
}

// runCerebroBrowserOpen launches the desktop app if needed and asks it to open
// & focus the Browser tab for the human (optionally pre-navigated to url).
func runCerebroBrowserOpen(cmd *cobra.Command, url string) error {
	// Same hard gate as every other action: an ungranted agent must not even
	// launch the app.
	if os.Getenv(personalBrowserGrantEnvCLI) == "" {
		return fmt.Errorf(
			"personal browser is not enabled for this workspace — ask an admin to turn on the Browser feature, then allow the 'tools:personal-browser' capability for you in Settings → Permissions",
		)
	}

	payload := map[string]any{}
	if url != "" {
		payload["url"] = url
	}

	// Already running → just open the tab.
	if sidecarReady() {
		return callCerebroBrowser(cmd, "open-tab", payload)
	}

	// Not running → launch and wait for the control server to come up.
	if err := launchDesktopApp(); err != nil {
		return err
	}

	const (
		pollInterval = 500 * time.Millisecond
		pollTimeout  = 25 * time.Second
	)
	deadline := time.Now().Add(pollTimeout)
	for !sidecarReady() {
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"the Multica desktop app was started but the personal browser did not come up — sign in to the app and make sure the Browser feature is enabled, then retry",
			)
		}
		time.Sleep(pollInterval)
	}

	return callCerebroBrowser(cmd, "open-tab", payload)
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

	openCmd := &cobra.Command{
		Use:   "open [url]",
		Short: "Launch the desktop app if needed and open & focus the Browser tab for the human",
		Long: "Bring up the in-app personal browser for the human to watch / log in / take over. " +
			"Launches the Multica desktop app if it is not already running, then opens and " +
			"focuses the Browser tab. Pass an optional URL to point the default session there " +
			"first. Run this before snapshot/click/fill.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := ""
			if len(args) == 1 {
				url = args[0]
			}
			return runCerebroBrowserOpen(cmd, url)
		},
	}

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

	cerebroBrowserCmd.AddCommand(openCmd, snapshotCmd, clickCmd, fillCmd, navigateCmd, sessionsCmd, logoutCmd, clearCookiesCmd)
}
