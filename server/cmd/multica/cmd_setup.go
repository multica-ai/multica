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
	Long: `Configures the CLI to connect to a self-hosted Multica server,
authenticates via browser, and starts the agent daemon.

By default, connects to http://localhost:8080 (backend) and
http://localhost:3000 (frontend). Use --server-url and --app-url to
specify a custom server (e.g. an on-premise deployment).

If a configuration already exists, you will be prompted before overwriting.

If you run this command from a different machine than the server, also
pass --callback-host <FQDN-or-IP-the-browser-can-reach-back-to-this-machine-on>
so the OAuth login flow can return the token to the CLI.

Use --profile to create an isolated configuration for a separate environment:
  multica setup --profile staging --server-url https://api-staging.co`,
	RunE: runSetupSelfHost,
}

var setupSelfHostCmd = &cobra.Command{
	Use:   "self-host",
	Short: "Alias for 'multica setup' (kept for backwards compatibility)",
	Long: `Equivalent to running 'multica setup' without a subcommand. This
fork is self-host-only, so the bare 'setup' command already configures a
self-hosted server.`,
	RunE: runSetupSelfHost,
}

func init() {
	registerSelfHostFlags(setupCmd)
	registerSelfHostFlags(setupSelfHostCmd)

	setupCmd.AddCommand(setupSelfHostCmd)
}

func registerSelfHostFlags(cmd *cobra.Command) {
	cmd.Flags().String("server-url", "", "Backend server URL (e.g. https://api.internal.co)")
	cmd.Flags().String("app-url", "", "Frontend app URL (e.g. https://app.internal.co)")
	cmd.Flags().Int("port", 8080, "Backend server port (used when --server-url is not set)")
	cmd.Flags().Int("frontend-port", 3000, "Frontend port (used when --app-url is not set)")
	cmd.Flags().String(callbackHostFlag, "", "Host the OAuth callback URL points at (auto-detected when empty). Use this for reverse-proxy / FQDN setups.")
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

// confirmOverwrite checks for an existing config and prompts the user.
// Returns true if we should proceed, false if the user declined.
func confirmOverwrite(profile string) (bool, error) {
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return true, nil // can't load → treat as no config
	}
	if cfg.ServerURL == "" {
		return true, nil // no server configured → fresh config
	}

	fmt.Fprintln(os.Stderr, "Current configuration:")
	fmt.Fprintf(os.Stderr, "  server_url: %s\n", cfg.ServerURL)
	fmt.Fprintf(os.Stderr, "  app_url:    %s\n", cfg.AppURL)
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

func runSetupSelfHost(cmd *cobra.Command, args []string) error {
	profile := resolveProfile(cmd)

	ok, err := confirmOverwrite(profile)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	serverURL, _ := cmd.Flags().GetString("server-url")
	appURL, _ := cmd.Flags().GetString("app-url")
	port, _ := cmd.Flags().GetInt("port")
	frontendPort, _ := cmd.Flags().GetInt("frontend-port")
	userProvidedServerURL := serverURL != ""

	// If custom URLs provided, use them; otherwise default to localhost with ports.
	if serverURL == "" {
		serverURL = fmt.Sprintf("http://localhost:%d", port)
	}
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

	cfg := cli.CLIConfig{
		ServerURL: serverURL,
		AppURL:    appURL,
	}
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Configured for self-hosted server.")
	fmt.Fprintf(os.Stderr, "  server_url: %s\n", cfg.ServerURL)
	fmt.Fprintf(os.Stderr, "  app_url:    %s\n", cfg.AppURL)
	printConfigLocation(profile)

	// Check if the server is reachable.
	if !probeServer(serverURL) {
		fmt.Fprintf(os.Stderr, "\n⚠ Server at %s is not reachable.\n", serverURL)
		fmt.Fprintln(os.Stderr, "  Make sure the server is running, then run 'multica login'.")
		return nil
	}

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
