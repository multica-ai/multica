package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// ---------------------------------------------------------------------------
// `multica runtime profile ...` — custom runtime profiles (MUL-3284)
//
// A runtime profile lets a workspace declare a custom agent runtime built on
// top of a supported protocol family (the routing backend) but launched via a
// site-specific command_name (e.g. a wrapper that injects credentials). The
// profile lives server-side and is workspace-scoped; the daemon resolves the
// command_name on each host's PATH at registration time.
//
// `set-path` / `unset-path` are the per-machine escape hatch: they record a
// profile_id -> absolute executable path mapping in this machine's local CLI
// config so the daemon can launch a profile whose command isn't on PATH (or
// pick a specific install among several). That mapping never leaves the
// machine — it is not sent to the server.
// ---------------------------------------------------------------------------

var runtimeProfileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage custom runtime profiles",
}

var runtimeProfileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List custom runtime profiles in the workspace",
	RunE:  runRuntimeProfileList,
}

var runtimeProfileCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a custom runtime profile",
	RunE:  runRuntimeProfileCreate,
}

var runtimeProfileUpdateCmd = &cobra.Command{
	Use:   "update <profile-id>",
	Short: "Update a custom runtime profile (protocol family is immutable)",
	Args:  exactArgs(1),
	RunE:  runRuntimeProfileUpdate,
}

var runtimeProfileDeleteCmd = &cobra.Command{
	Use:   "delete <profile-id>",
	Short: "Delete a custom runtime profile",
	Args:  exactArgs(1),
	RunE:  runRuntimeProfileDelete,
}

var runtimeProfileSetPathCmd = &cobra.Command{
	Use:   "set-path <profile-id>",
	Short: "Pin a per-machine executable path for a runtime profile (local only)",
	Args:  exactArgs(1),
	RunE:  runRuntimeProfileSetPath,
}

var runtimeProfileUnsetPathCmd = &cobra.Command{
	Use:   "unset-path <profile-id>",
	Short: "Remove a per-machine executable path override for a runtime profile",
	Args:  exactArgs(1),
	RunE:  runRuntimeProfileUnsetPath,
}

func init() {
	runtimeCmd.AddCommand(runtimeProfileCmd)
	runtimeProfileCmd.AddCommand(runtimeProfileListCmd)
	runtimeProfileCmd.AddCommand(runtimeProfileCreateCmd)
	runtimeProfileCmd.AddCommand(runtimeProfileUpdateCmd)
	runtimeProfileCmd.AddCommand(runtimeProfileDeleteCmd)
	runtimeProfileCmd.AddCommand(runtimeProfileSetPathCmd)
	runtimeProfileCmd.AddCommand(runtimeProfileUnsetPathCmd)

	// list
	runtimeProfileListCmd.Flags().String("output", "table", "Output format: table or json")

	// create
	runtimeProfileCreateCmd.Flags().String("protocol-family", "", "Supported backend the profile routes to (required)")
	runtimeProfileCreateCmd.Flags().String("command-name", "", "Executable the daemon resolves on PATH (required)")
	runtimeProfileCreateCmd.Flags().String("display-name", "", "Human-readable profile name (required)")
	runtimeProfileCreateCmd.Flags().String("description", "", "Optional description")
	runtimeProfileCreateCmd.Flags().String("visibility", "", "Visibility: workspace or private")
	runtimeProfileCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// update
	runtimeProfileUpdateCmd.Flags().String("display-name", "", "New display name")
	runtimeProfileUpdateCmd.Flags().String("command-name", "", "New command name")
	runtimeProfileUpdateCmd.Flags().String("description", "", "New description")
	// NOTE: a --fixed-arg flag is intentionally NOT exposed in v1. The server
	// carries the fixed_args column, but the daemon does not yet pass these
	// args to the agent launch command, so a CLI flag would promise admins a
	// no-op. Re-add once it's wired end-to-end (TODO(MUL-3284), see
	// server/internal/daemon/daemon.go).
	runtimeProfileUpdateCmd.Flags().String("visibility", "", "New visibility: workspace or private")
	runtimeProfileUpdateCmd.Flags().Bool("enabled", true, "Enable or disable the profile")
	runtimeProfileUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// set-path
	runtimeProfileSetPathCmd.Flags().String("path", "", "Absolute path to the executable on this machine (required)")
}

// runtimeProfilesPath builds the workspace-scoped collection path.
func runtimeProfilesPath(workspaceID string) string {
	return fmt.Sprintf("/api/workspaces/%s/runtime-profiles", workspaceID)
}

// validateProtocolFamily checks a protocol family against the canonical agent
// whitelist client-side so an obvious typo fails fast with a helpful list
// instead of an opaque server 400.
func validateProtocolFamily(family string) error {
	if !agent.IsSupportedType(family) {
		return fmt.Errorf("invalid --protocol-family %q: must be one of %s",
			family, strings.Join(agent.SupportedTypes, ", "))
	}
	return nil
}

// validateVisibility rejects anything other than the two server-accepted
// visibility values. An empty string is allowed (means "let the server pick
// its default" on create, "leave unchanged" on update).
func validateVisibility(visibility string) error {
	switch visibility {
	case "", "workspace", "private":
		return nil
	default:
		return fmt.Errorf("invalid --visibility %q: must be workspace or private", visibility)
	}
}

func runRuntimeProfileList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	workspaceID, err := requireWorkspaceID(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var resp struct {
		RuntimeProfiles []map[string]any `json:"runtime_profiles"`
	}
	if err := client.GetJSON(ctx, runtimeProfilesPath(workspaceID), &resp); err != nil {
		return fmt.Errorf("list runtime profiles: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp.RuntimeProfiles)
	}
	printRuntimeProfileTable(resp.RuntimeProfiles)
	return nil
}

func runRuntimeProfileCreate(cmd *cobra.Command, _ []string) error {
	family, _ := cmd.Flags().GetString("protocol-family")
	commandName, _ := cmd.Flags().GetString("command-name")
	displayName, _ := cmd.Flags().GetString("display-name")
	description, _ := cmd.Flags().GetString("description")
	visibility, _ := cmd.Flags().GetString("visibility")

	if strings.TrimSpace(family) == "" {
		return fmt.Errorf("--protocol-family is required")
	}
	if strings.TrimSpace(commandName) == "" {
		return fmt.Errorf("--command-name is required")
	}
	if strings.TrimSpace(displayName) == "" {
		return fmt.Errorf("--display-name is required")
	}
	if err := validateProtocolFamily(family); err != nil {
		return err
	}
	if err := validateVisibility(visibility); err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	workspaceID, err := requireWorkspaceID(cmd)
	if err != nil {
		return err
	}

	body := map[string]any{
		"display_name":    displayName,
		"protocol_family": family,
		"command_name":    commandName,
	}
	if description != "" {
		body["description"] = description
	}
	if visibility != "" {
		body["visibility"] = visibility
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var profile map[string]any
	if err := client.PostJSON(ctx, runtimeProfilesPath(workspaceID), body, &profile); err != nil {
		return fmt.Errorf("create runtime profile: %w", err)
	}
	return outputRuntimeProfile(cmd, profile)
}

func runRuntimeProfileUpdate(cmd *cobra.Command, args []string) error {
	profileID := args[0]

	body := map[string]any{}
	if cmd.Flags().Changed("display-name") {
		v, _ := cmd.Flags().GetString("display-name")
		body["display_name"] = v
	}
	if cmd.Flags().Changed("command-name") {
		v, _ := cmd.Flags().GetString("command-name")
		body["command_name"] = v
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	if cmd.Flags().Changed("visibility") {
		v, _ := cmd.Flags().GetString("visibility")
		if err := validateVisibility(v); err != nil {
			return err
		}
		body["visibility"] = v
	}
	if cmd.Flags().Changed("enabled") {
		v, _ := cmd.Flags().GetBool("enabled")
		body["enabled"] = v
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update: pass at least one of --display-name, --command-name, --description, --visibility, --enabled")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	workspaceID, err := requireWorkspaceID(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path := runtimeProfilesPath(workspaceID) + "/" + profileID
	var profile map[string]any
	if err := client.PatchJSON(ctx, path, body, &profile); err != nil {
		return fmt.Errorf("update runtime profile: %w", err)
	}
	return outputRuntimeProfile(cmd, profile)
}

func runRuntimeProfileDelete(cmd *cobra.Command, args []string) error {
	profileID := args[0]

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	workspaceID, err := requireWorkspaceID(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path := runtimeProfilesPath(workspaceID) + "/" + profileID
	if err := client.DeleteJSON(ctx, path); err != nil {
		// 409 means the server refused because active agents are still bound
		// to this profile. Surface the server's explanation verbatim rather
		// than the generic HTTP wrapper so the user sees what to unbind.
		var httpErr *cli.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusConflict {
			msg := strings.TrimSpace(httpErr.Body)
			if msg == "" {
				msg = "profile still has active agents bound to it"
			}
			return fmt.Errorf("cannot delete runtime profile %s: %s", profileID, msg)
		}
		return fmt.Errorf("delete runtime profile: %w", err)
	}
	fmt.Printf("Deleted runtime profile %s\n", profileID)
	return nil
}

func runRuntimeProfileSetPath(cmd *cobra.Command, args []string) error {
	profileID := args[0]
	path, _ := cmd.Flags().GetString("path")
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("--path is required")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("--path must be an absolute path, got %q", path)
	}

	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return fmt.Errorf("load CLI config: %w", err)
	}
	if cfg.ProfileCommandOverrides == nil {
		cfg.ProfileCommandOverrides = map[string]string{}
	}
	cfg.ProfileCommandOverrides[profileID] = path
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return fmt.Errorf("save CLI config: %w", err)
	}
	fmt.Printf("Pinned runtime profile %s to %s on this machine.\n", profileID, path)
	fmt.Println("Restart the daemon for the change to take effect.")
	return nil
}

func runRuntimeProfileUnsetPath(cmd *cobra.Command, args []string) error {
	profileID := args[0]

	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return fmt.Errorf("load CLI config: %w", err)
	}
	if _, ok := cfg.ProfileCommandOverrides[profileID]; !ok {
		fmt.Printf("No per-machine path override set for runtime profile %s.\n", profileID)
		return nil
	}
	delete(cfg.ProfileCommandOverrides, profileID)
	if len(cfg.ProfileCommandOverrides) == 0 {
		// Normalize back to nil so the key drops out of the saved JSON.
		cfg.ProfileCommandOverrides = nil
	}
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return fmt.Errorf("save CLI config: %w", err)
	}
	fmt.Printf("Removed per-machine path override for runtime profile %s.\n", profileID)
	fmt.Println("Restart the daemon for the change to take effect.")
	return nil
}

// outputRuntimeProfile renders a single profile honoring --output.
func outputRuntimeProfile(cmd *cobra.Command, profile map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, profile)
	}
	printRuntimeProfileTable([]map[string]any{profile})
	return nil
}

// printRuntimeProfileTable renders profiles as a stable, sorted table.
func printRuntimeProfileTable(profiles []map[string]any) {
	headers := []string{"ID", "DISPLAY_NAME", "PROTOCOL_FAMILY", "COMMAND_NAME", "VISIBILITY", "ENABLED"}
	rows := make([][]string, 0, len(profiles))
	for _, p := range profiles {
		rows = append(rows, []string{
			strVal(p, "id"),
			strVal(p, "display_name"),
			strVal(p, "protocol_family"),
			strVal(p, "command_name"),
			strVal(p, "visibility"),
			strVal(p, "enabled"),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][1] < rows[j][1] })
	cli.PrintTable(os.Stdout, headers, rows)
}
