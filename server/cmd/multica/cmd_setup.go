package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configure the CLI, authenticate, and start the daemon",
	Long: `Configures the CLI to connect to Multica Cloud (multica.ai), then
authenticates via browser and starts the agent daemon.

If a configuration already exists, you will be prompted before overwriting.

Use 'multica setup self-host' to connect to a self-hosted server instead.

If you run this command over SSH on a remote machine, keep the localhost
callback and follow the SSH tunnel hint printed during browser login. If your
browser can reach this CLI directly on a private network address, pass
--callback-host <host-or-ip>.

Use --profile to create an isolated configuration for a separate environment:
  multica setup self-host --profile staging --server-url https://api-staging.co`,
	RunE: runSetupCloud,
}

var setupCloudCmd = &cobra.Command{
	Use:   "cloud",
	Short: "Configure the CLI for Multica Cloud (multica.ai)",
	Long: `Explicitly configures the CLI to connect to Multica Cloud (multica.ai).

If you run this command over SSH on a remote machine, keep the localhost
callback and follow the SSH tunnel hint printed during browser login. If your
browser can reach this CLI directly on a private network address, pass
--callback-host <host-or-ip>.

This is equivalent to running 'multica setup' without a subcommand.`,
	RunE: runSetupCloud,
}

var setupSelfHostCmd = &cobra.Command{
	Use:   "self-host",
	Short: "Configure the CLI for a self-hosted Multica server",
	Long: `Configures the CLI to connect to a self-hosted Multica server.

By default, connects to http://localhost:8080 (backend) and http://localhost:3000 (frontend).
Use --server-url and --app-url to specify a custom server (e.g. an on-premise deployment).

If you run this command from a different machine than the server, also pass
--callback-host <host-or-ip-the-browser-can-reach-back-to-this-machine-on> so
the OAuth login flow can return the token to the CLI.

Examples:
  multica setup self-host
  multica setup self-host --server-url https://api.internal.co --app-url https://app.internal.co
  multica setup self-host --port 9090 --frontend-port 4000`,
	RunE: runSetupSelfHost,
}

func init() {
	setupCmd.Flags().String(callbackHostFlag, "", callbackHostFlagHelp)
	setupCloudCmd.Flags().String(callbackHostFlag, "", callbackHostFlagHelp)

	setupSelfHostCmd.Flags().String("server-url", "", "Backend server URL (e.g. https://api.internal.co) (env: MULTICA_SERVER_URL)")
	setupSelfHostCmd.Flags().String("app-url", "", "Frontend app URL (e.g. https://app.internal.co) (env: MULTICA_APP_URL)")
	setupSelfHostCmd.Flags().Int("port", 8080, "Backend server port (used when --server-url is not set)")
	setupSelfHostCmd.Flags().Int("frontend-port", 3000, "Frontend port (used when --app-url is not set)")
	setupSelfHostCmd.Flags().String(callbackHostFlag, "", callbackHostFlagHelp)

	setupCmd.AddCommand(setupCloudCmd)
	setupCmd.AddCommand(setupSelfHostCmd)
}

// printConfigLocation prints the config file path and profile name.
func printConfigLocation(profile string) {
	path, err := cli.CLIConfigPathForProfile(profile)
	if err != nil {
		return
	}
	if profile != "" {
		fmt.Fprintf(os.Stderr, "  profile:    %s\n", profile)
	}
	fmt.Fprintf(os.Stderr, "  config:     %s\n", path)
}

// confirmOverwrite checks for an existing config and prompts the user before
// overwriting it. newServerURL/newAppURL are the values setup is about to
// write; they are shown as "old -> new" when they differ from the current
// config so the user can see the passed flags/env were received rather than
// silently ignored. Returns true if we should proceed, false if the user
// declined.
func confirmOverwrite(profile, newServerURL, newAppURL string) (bool, error) {
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return true, nil // can't load → treat as no config
	}
	if cfg.ServerURL == "" {
		return true, nil // no server configured → fresh config
	}

	fmt.Fprintln(os.Stderr, "Current configuration:")
	fmt.Fprintf(os.Stderr, "  server_url: %s\n", formatURLChange(cfg.ServerURL, newServerURL))
	fmt.Fprintf(os.Stderr, "  app_url:    %s\n", formatURLChange(cfg.AppURL, newAppURL))
	if cfg.WorkspaceID != "" {
		fmt.Fprintf(os.Stderr, "  workspace:  %s\n", cfg.WorkspaceID)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprint(os.Stderr, "This will reset your configuration. Continue? [y/N] ")

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Fprintln(os.Stderr, "Aborted.")
		return false, nil
	}
	return true, nil
}

// formatURLChange renders "old -> new" when setup is about to change the value,
// or just the current value when it stays the same.
func formatURLChange(oldVal, newVal string) string {
	if newVal != "" && newVal != oldVal {
		return fmt.Sprintf("%s  ->  %s", oldVal, newVal)
	}
	return oldVal
}

func runSetupCloud(cmd *cobra.Command, args []string) error {
	profile := resolveProfile(cmd)

	cfg := cli.CLIConfig{
		ServerURL: "https://api.multica.ai",
		AppURL:    "https://multica.ai",
	}

	ok, err := confirmOverwrite(profile, cfg.ServerURL, cfg.AppURL)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Configured for Multica Cloud (https://multica.ai).")
	fmt.Fprintf(os.Stderr, "  server_url: %s\n", cfg.ServerURL)
	fmt.Fprintf(os.Stderr, "  app_url:    %s\n", cfg.AppURL)
	printConfigLocation(profile)

	// Authenticate.
	fmt.Fprintln(os.Stderr, "")
	if err := runLogin(cmd, args); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "\nStarting daemon...")
	if err := runDaemonBackground(cmd); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	fmt.Fprintln(os.Stderr, "\n✓ Setup complete! Your machine is now connected to Multica.")

	return nil
}

func runSetupSelfHost(cmd *cobra.Command, args []string) error {
	profile := resolveProfile(cmd)

	// Resolve the target URLs before confirming the overwrite so the prompt can
	// show the incoming values ("old -> new"), making it clear the passed flags
	// were received.
	//
	// Honor MULTICA_SERVER_URL / MULTICA_APP_URL when the matching flag is not
	// set — consistent with the rest of the CLI (resolveServerURL) and with the
	// env vars documented on the root --server-url flag and in `multica --help`.
	// Before this, setup self-host read only the flags, so a self-hoster who set
	// MULTICA_SERVER_URL still got the localhost default and an "unreachable"
	// error (GitHub #3912).
	existing, _ := cli.LoadCLIConfigForProfile(profile)
	serverURL, userProvidedServerURL := resolveSelfHostServerURL(cmd, existing)
	appURL := resolveSelfHostAppURL(cmd, existing)
	frontendPort, _ := cmd.Flags().GetInt("frontend-port")

	if appURL == "" {
		if userProvidedServerURL && !serverHostIsLocal(serverURL) {
			// We can't guess the frontend URL for a remote server: api.x.co
			// and app.x.co, or an https-fronted deployment, would silently
			// produce a broken login URL. Ask the user instead.
			entered, err := promptAppURL(serverURL)
			if err != nil {
				return err
			}
			if entered == "" {
				return fmt.Errorf("--app-url is required when --server-url points at a remote host (e.g. --app-url https://app.internal.co)")
			}
			appURL = entered
		} else {
			appURL = fmt.Sprintf("http://localhost:%d", frontendPort)
		}
	}

	ok, err := confirmOverwrite(profile, serverURL, appURL)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	// Probe before persisting anything. A failed setup must never overwrite a
	// working config or wipe the saved token: persistSelfHostConfigIfReachable
	// writes only when the server answers, so an unreachable host leaves the
	// existing config untouched and the user stays logged in.
	reachable, err := persistSelfHostConfigIfReachable(serverURL, appURL, profile, probeServer)
	if err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if !reachable {
		fmt.Fprintf(os.Stderr, "\n⚠ Server at %s is not reachable.\n", serverURL)
		fmt.Fprintln(os.Stderr, "  Your existing configuration was left unchanged.")
		fmt.Fprintln(os.Stderr, "  Verify the URL, then re-run 'multica setup self-host' once it's reachable.")
		return nil
	}

	fmt.Fprintln(os.Stderr, "Configured for self-hosted server.")
	fmt.Fprintf(os.Stderr, "  server_url: %s\n", serverURL)
	fmt.Fprintf(os.Stderr, "  app_url:    %s\n", appURL)
	printConfigLocation(profile)

	// Authenticate.
	fmt.Fprintln(os.Stderr, "")
	if err := runLogin(cmd, args); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "\nStarting daemon...")
	if err := runDaemonBackground(cmd); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	fmt.Fprintln(os.Stderr, "\n✓ Setup complete! Your machine is now connected to Multica.")

	return nil
}

// persistSelfHostConfigIfReachable probes serverURL and, only when it answers,
// overwrites the profile config with the given self-host URLs. When the server
// is unreachable it leaves any existing config — and its auth token — untouched
// and returns false, so a failed `setup self-host` never logs the user out or
// clobbers a working config (the original ordering saved first, then probed,
// then bailed — wiping the token on every failed probe). The prober is injected
// so tests can exercise both branches without real network I/O.
func persistSelfHostConfigIfReachable(serverURL, appURL, profile string, probe func(string) bool) (bool, error) {
	if !probe(serverURL) {
		return false, nil
	}
	cfg := cli.CLIConfig{
		ServerURL: serverURL,
		AppURL:    appURL,
	}
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return false, err
	}
	return true, nil
}

// resolveSelfHostServerURL picks the backend URL for `setup self-host`: the
// --server-url flag wins, then the MULTICA_SERVER_URL env var (consistent with
// the rest of the CLI and the env var documented on the root flag), then an
// already-configured server_url from the existing config, then the localhost
// default built from --port. userProvided is true when the URL came from the
// user (flag, env, or an existing config) rather than the localhost fallback —
// the caller uses it to decide whether a remote host needs an explicit app_url.
//
// Falling back to existing.ServerURL means re-running setup self-host (e.g. to
// re-login or restart the daemon) keeps a configured remote deployment instead
// of silently resetting it to http://localhost:8080. An explicit --port opts
// back into the localhost path for the local-dev case.
//
// A user-supplied URL is run through normalizeAPIBaseURL, the same path
// resolveServerURL uses: MULTICA_SERVER_URL is documented as a ws:// daemon
// address (e.g. ws://localhost:8080/ws), so the ws/wss form and a trailing /ws
// are accepted and converted to the http(s) base that the reachability probe
// and the stored server_url expect.
func resolveSelfHostServerURL(cmd *cobra.Command, existing cli.CLIConfig) (serverURL string, userProvided bool) {
	if v := cli.FlagOrEnv(cmd, "server-url", "MULTICA_SERVER_URL", ""); v != "" {
		return normalizeAPIBaseURL(v), true
	}
	if !cmd.Flags().Changed("port") && existing.ServerURL != "" {
		// `config set server_url` stores the value as-is, so it may be the
		// documented ws:// daemon form; normalize it to the http(s) base the
		// probe and stored server_url expect, like resolveServerURL does.
		return normalizeAPIBaseURL(existing.ServerURL), true
	}
	port, _ := cmd.Flags().GetInt("port")
	return fmt.Sprintf("http://localhost:%d", port), false
}

// resolveSelfHostAppURL resolves the frontend URL for `setup self-host`: the
// --app-url flag wins, then MULTICA_APP_URL, then an already-configured app_url
// from the existing config (unless --frontend-port was passed). Returns "" when
// none of those is set, leaving the caller to infer it — prompt for a remote
// host, or fall back to localhost:<frontend-port>.
//
// Mirrors resolveSelfHostServerURL so re-running setup self-host keeps a
// configured remote frontend instead of resetting it to localhost. Unlike
// server_url, app_url is a plain frontend URL rather than a ws:// daemon
// address, so it is used as-is without normalizeAPIBaseURL.
func resolveSelfHostAppURL(cmd *cobra.Command, existing cli.CLIConfig) string {
	if v := cli.FlagOrEnv(cmd, "app-url", "MULTICA_APP_URL", ""); v != "" {
		return v
	}
	if !cmd.Flags().Changed("frontend-port") && existing.AppURL != "" {
		return existing.AppURL
	}
	return ""
}

// serverHostIsLocal reports whether serverURL points at the same machine as
// the CLI (loopback literal or "localhost"). Used to decide whether to infer
// app_url from server_url or fall back to the local-dev default.
func serverHostIsLocal(serverURL string) bool {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return false
	}
	h := parsed.Hostname()
	if h == "localhost" {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// promptAppURL asks the user for the frontend URL interactively. We can't
// derive it from a remote server_url — api.example.com ≠ app.example.com in
// most production setups — so guessing would just defer the failure to the
// browser login step. Returns an empty string if the user hits enter.
func promptAppURL(serverURL string) (string, error) {
	fmt.Fprintf(os.Stderr, "No --app-url provided, and --server-url (%s) is remote.\n", serverURL)
	fmt.Fprint(os.Stderr, "Enter the frontend app URL (e.g. https://app.internal.co): ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", nil
	}
	return strings.TrimRight(strings.TrimSpace(line), "/"), nil
}

// probeServer checks whether a Multica backend is reachable at the given URL.
func probeServer(baseURL string) bool {
	url := strings.TrimRight(baseURL, "/") + "/health"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
