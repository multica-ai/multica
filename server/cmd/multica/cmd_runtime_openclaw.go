package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// ---------------------------------------------------------------------------
// `multica runtime openclaw ...` — per-machine OpenClaw instance overrides
//
// CLI mirror of the GUI Settings -> Runtimes -> "Choose OpenClaw instance..."
// dialog. Both routes ultimately write the same fields on the local CLI
// config (see PR #3896 / issue #3875):
//
//	cli.CLIConfig.Backends.OpenClaw.{BinaryPath,StateDir}
//
// These are PER-MACHINE overrides — they live only in this host's
// ~/.multica/profiles/<profile>/config.json and never leave the machine.
// They tell the daemon which OpenClaw installation to use locally:
//
//	state_dir   — the OpenClaw "instance" directory (agents, API keys, history)
//	binary_path — the openclaw CLI binary (advanced; the GUI does not expose
//	              this field. CLI exposes it for power users running an
//	              OpenClaw bundled inside another desktop app or built from
//	              source at a non-PATH location.)
//
// Resolution precedence is `env > config > default` and matches what
// daemon/config.go applies on LoadConfig:
//
//	BinaryPath: MULTICA_OPENCLAW_PATH (env) > backends.openclaw.binary_path > PATH lookup
//	StateDir:   OPENCLAW_STATE_DIR    (env) > backends.openclaw.state_dir   > ~/.openclaw
//
// Sibling command in the same tree: `multica runtime profile set-path` (added
// in MUL-3284 / PR #4177). Both write to local CLI config, both require a
// daemon restart to take effect.
// ---------------------------------------------------------------------------

var runtimeOpenclawCmd = &cobra.Command{
	Use:   "openclaw",
	Short: "Configure per-machine OpenClaw instance overrides",
	Long: `Configure which OpenClaw installation the daemon uses on this machine.

These overrides are written to ~/.multica/profiles/<profile>/config.json
under backends.openclaw and never leave the machine. They tell the daemon
which OpenClaw "instance" to use (an OpenClaw instance is a directory
containing its own agents, API keys, and conversation history — typically
~/.openclaw or another directory created with ` + "`openclaw --profile <name>`" + `).

Resolution precedence (env always wins over config):

  Instance directory: OPENCLAW_STATE_DIR    > backends.openclaw.state_dir   > ~/.openclaw
  Binary path:        MULTICA_OPENCLAW_PATH > backends.openclaw.binary_path > PATH lookup

Subcommands:
  show              Print current overrides and effective resolution
  set-instance      Pin the OpenClaw instance directory (state_dir)
  unset-instance    Remove the instance directory override
  set-binary        Pin the openclaw binary path (advanced; not in the GUI)
  unset-binary      Remove the binary path override

The daemon reads these values once at startup. Restart it for changes to
take effect:

  multica daemon restart
`,
}

var runtimeOpenclawShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current OpenClaw overrides and effective resolution",
	RunE:  runRuntimeOpenclawShow,
}

var runtimeOpenclawSetInstanceCmd = &cobra.Command{
	Use:   "set-instance <path>",
	Short: "Pin the OpenClaw instance directory on this machine (local only)",
	Args:  exactArgs(1),
	RunE:  runRuntimeOpenclawSetInstance,
}

var runtimeOpenclawUnsetInstanceCmd = &cobra.Command{
	Use:   "unset-instance",
	Short: "Remove the OpenClaw instance directory override",
	RunE:  runRuntimeOpenclawUnsetInstance,
}

var runtimeOpenclawSetBinaryCmd = &cobra.Command{
	Use:   "set-binary <path>",
	Short: "Pin the openclaw binary path on this machine (advanced; not in GUI)",
	Args:  exactArgs(1),
	RunE:  runRuntimeOpenclawSetBinary,
}

var runtimeOpenclawUnsetBinaryCmd = &cobra.Command{
	Use:   "unset-binary",
	Short: "Remove the openclaw binary path override",
	RunE:  runRuntimeOpenclawUnsetBinary,
}

func init() {
	runtimeCmd.AddCommand(runtimeOpenclawCmd)
	runtimeOpenclawCmd.AddCommand(runtimeOpenclawShowCmd)
	runtimeOpenclawCmd.AddCommand(runtimeOpenclawSetInstanceCmd)
	runtimeOpenclawCmd.AddCommand(runtimeOpenclawUnsetInstanceCmd)
	runtimeOpenclawCmd.AddCommand(runtimeOpenclawSetBinaryCmd)
	runtimeOpenclawCmd.AddCommand(runtimeOpenclawUnsetBinaryCmd)

	runtimeOpenclawShowCmd.Flags().String("output", "table", "Output format: table or json")
}

// loadOpenclawOverride reads the CLI config for the resolved profile and
// returns its OpenClaw override (or a fresh zero struct + the loaded cfg if
// no override exists). The caller can mutate the returned override and pass
// cfg back to cli.SaveCLIConfigForProfile to persist.
//
// Pulled out into a helper so set/unset/show share the same load+navigate
// path — keeping the nullable-pointer chain (Backends -> OpenClaw) accessed
// in one place reduces the chance of a future field add forgetting to handle
// nil at one of the call sites.
func loadOpenclawOverride(profile string) (cli.CLIConfig, *cli.OpenClawOverride, error) {
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return cli.CLIConfig{}, nil, fmt.Errorf("load CLI config: %w", err)
	}
	if cfg.Backends == nil {
		cfg.Backends = &cli.BackendOverrides{}
	}
	if cfg.Backends.OpenClaw == nil {
		cfg.Backends.OpenClaw = &cli.OpenClawOverride{}
	}
	return cfg, cfg.Backends.OpenClaw, nil
}

// pruneOpenclawOverride removes empty leaves so saved configs stay minimal:
// if both BinaryPath and StateDir are empty we strip the OpenClaw pointer;
// if BackendOverrides ends up with no live backends we strip that too. This
// keeps round-tripped configs byte-identical for users who set then cleared
// an override (the omitempty contract documented on the struct).
func pruneOpenclawOverride(cfg *cli.CLIConfig) {
	if cfg.Backends == nil {
		return
	}
	oc := cfg.Backends.OpenClaw
	if oc != nil && oc.BinaryPath == "" && oc.StateDir == "" {
		cfg.Backends.OpenClaw = nil
	}
	if cfg.Backends.OpenClaw == nil {
		// No other backend overrides exist today; an additive future field
		// would add more checks here before clearing the parent pointer.
		cfg.Backends = nil
	}
}

func runRuntimeOpenclawShow(cmd *cobra.Command, _ []string) error {
	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return fmt.Errorf("load CLI config: %w", err)
	}

	var oc cli.OpenClawOverride
	if cfg.Backends != nil && cfg.Backends.OpenClaw != nil {
		oc = *cfg.Backends.OpenClaw
	}

	envBinary := os.Getenv("MULTICA_OPENCLAW_PATH")
	envState := os.Getenv("OPENCLAW_STATE_DIR")

	effectiveBinary, binarySource := resolveOpenclawBinary(envBinary, oc.BinaryPath)
	effectiveState, stateSource := resolveOpenclawState(envState, oc.StateDir)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		payload := map[string]any{
			"profile": profile,
			"override": map[string]string{
				"binary_path": oc.BinaryPath,
				"state_dir":   oc.StateDir,
			},
			"env": map[string]string{
				"MULTICA_OPENCLAW_PATH": envBinary,
				"OPENCLAW_STATE_DIR":    envState,
			},
			"effective": map[string]string{
				"binary":        effectiveBinary,
				"binary_source": binarySource,
				"state":         effectiveState,
				"state_source":  stateSource,
			},
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	// Table mode: print a compact precedence-aware report. We deliberately
	// show three rows per field (env / config / effective) so power users
	// debugging an unexpected resolution can spot which layer won at a glance.
	profileLabel := profile
	if profileLabel == "" {
		profileLabel = "(default)"
	}
	fmt.Printf("OpenClaw runtime overrides for profile %q:\n\n", profileLabel)

	fmt.Println("  Instance directory (state_dir):")
	fmt.Printf("    config.json:           %s\n", notSetOr(oc.StateDir))
	fmt.Printf("    OPENCLAW_STATE_DIR:    %s\n", notSetOr(envState))
	fmt.Printf("    effective:             %s  (%s)\n", effectiveState, stateSource)
	fmt.Println()
	fmt.Println("  Binary path:")
	fmt.Printf("    config.json:           %s\n", notSetOr(oc.BinaryPath))
	fmt.Printf("    MULTICA_OPENCLAW_PATH: %s\n", notSetOr(envBinary))
	fmt.Printf("    effective:             %s  (%s)\n", effectiveBinary, binarySource)
	return nil
}

// notSetOr returns "(not set)" for empty strings and s otherwise. Used only
// by the `show` table-mode renderer; we do not reuse the package-level
// emptyDash helper because its "-" placeholder reads as a literal dash for
// users who don't know it's a sentinel.
func notSetOr(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}

func runRuntimeOpenclawSetInstance(cmd *cobra.Command, args []string) error {
	path := strings.TrimSpace(args[0])
	if path == "" {
		return fmt.Errorf("instance directory path is required")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute, got %q", path)
	}
	profile := resolveProfile(cmd)
	cfg, oc, err := loadOpenclawOverride(profile)
	if err != nil {
		return err
	}
	oc.StateDir = path
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return fmt.Errorf("save CLI config: %w", err)
	}
	fmt.Printf("Pinned OpenClaw instance directory to %s on this machine.\n", path)
	fmt.Println("Restart the daemon for the change to take effect: multica daemon restart")
	return nil
}

func runRuntimeOpenclawUnsetInstance(cmd *cobra.Command, _ []string) error {
	profile := resolveProfile(cmd)
	cfg, oc, err := loadOpenclawOverride(profile)
	if err != nil {
		return err
	}
	if oc.StateDir == "" {
		fmt.Println("No OpenClaw instance directory override is set; nothing to remove.")
		return nil
	}
	oc.StateDir = ""
	pruneOpenclawOverride(&cfg)
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return fmt.Errorf("save CLI config: %w", err)
	}
	fmt.Println("Removed OpenClaw instance directory override.")
	fmt.Println("Restart the daemon for the change to take effect: multica daemon restart")
	return nil
}

func runRuntimeOpenclawSetBinary(cmd *cobra.Command, args []string) error {
	path := strings.TrimSpace(args[0])
	if path == "" {
		return fmt.Errorf("binary path is required")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute, got %q", path)
	}
	profile := resolveProfile(cmd)
	cfg, oc, err := loadOpenclawOverride(profile)
	if err != nil {
		return err
	}
	oc.BinaryPath = path
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return fmt.Errorf("save CLI config: %w", err)
	}
	fmt.Printf("Pinned openclaw binary to %s on this machine.\n", path)
	fmt.Println("Restart the daemon for the change to take effect: multica daemon restart")
	return nil
}

func runRuntimeOpenclawUnsetBinary(cmd *cobra.Command, _ []string) error {
	profile := resolveProfile(cmd)
	cfg, oc, err := loadOpenclawOverride(profile)
	if err != nil {
		return err
	}
	if oc.BinaryPath == "" {
		fmt.Println("No openclaw binary path override is set; nothing to remove.")
		return nil
	}
	oc.BinaryPath = ""
	pruneOpenclawOverride(&cfg)
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return fmt.Errorf("save CLI config: %w", err)
	}
	fmt.Println("Removed openclaw binary path override.")
	fmt.Println("Restart the daemon for the change to take effect: multica daemon restart")
	return nil
}

// resolveOpenclawBinary mirrors the precedence applied by daemon/config.go's
// applyOpenclawOverride helper. Returned source is one of:
//
//	"env (MULTICA_OPENCLAW_PATH)"  -- env wins
//	"config.json"                 -- file override applies
//	"PATH lookup"                 -- daemon will resolve "openclaw" on PATH
func resolveOpenclawBinary(envVal, configVal string) (effective, source string) {
	if envVal != "" {
		return envVal, "env (MULTICA_OPENCLAW_PATH)"
	}
	if configVal != "" {
		return configVal, "config.json"
	}
	return "openclaw", "PATH lookup"
}

// resolveOpenclawState mirrors the same precedence for the state directory.
func resolveOpenclawState(envVal, configVal string) (effective, source string) {
	if envVal != "" {
		return envVal, "env (OPENCLAW_STATE_DIR)"
	}
	if configVal != "" {
		return configVal, "config.json"
	}
	return "~/.openclaw  (OpenClaw default)", "default"
}
