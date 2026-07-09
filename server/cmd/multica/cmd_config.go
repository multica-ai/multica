package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration for multica",
	RunE:  runConfigShow,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current CLI configuration",
	RunE:  runConfigShow,
}

// configSetSupportedKeys is the whitelist consumed by both `config set`'s
// switch and its --help output, so a new key gets validation, error text,
// and documentation in one place. Order matches configShow output.
var configSetSupportedKeys = []string{
	"server_url",
	"app_url",
	"workspace_id",
	"device_name",
	"runtime_name",
	"max_concurrent_tasks",
	"poll_interval",
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a CLI configuration value",
	Long: "Supported keys: " +
		"server_url, app_url, workspace_id, " +
		"device_name, runtime_name, max_concurrent_tasks, poll_interval.\n\n" +
		"The four daemon keys (device_name, runtime_name, max_concurrent_tasks, " +
		"poll_interval) mirror their --flag / env counterparts and are read by " +
		"`daemon start` when neither the flag nor the env var is set. " +
		"Precedence: --flag > MULTICA_… env > config.json > built-in default. " +
		"Set poll_interval as a Go duration string, e.g. '10s' or '500ms'.",
	Args: exactArgs(2),
	RunE: runConfigSet,
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configSetCmd)
}

func runConfigShow(cmd *cobra.Command, _ []string) error {
	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return err
	}

	path, _ := cli.CLIConfigPathForProfile(profile)
	fmt.Fprintf(os.Stdout, "Config file: %s\n", path)
	if profile != "" {
		fmt.Fprintf(os.Stdout, "Profile:      %s\n", profile)
	}
	fmt.Fprintf(os.Stdout, "%-22s %s\n", "server_url:", valueOrDefault(cfg.ServerURL, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-22s %s\n", "app_url:", valueOrDefault(cfg.AppURL, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-22s %s\n", "workspace_id:", valueOrDefault(cfg.WorkspaceID, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-22s %s\n", "device_name:", valueOrDefault(cfg.DeviceName, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-22s %s\n", "runtime_name:", valueOrDefault(cfg.RuntimeName, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-22s %s\n", "max_concurrent_tasks:", intOrDefault(cfg.MaxConcurrentTasks, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-22s %s\n", "poll_interval:", valueOrDefault(cfg.PollInterval, "(not set)"))
	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key, value := args[0], args[1]

	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return err
	}

	if err := applyConfigSet(&cfg, key, value); err != nil {
		return err
	}

	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Set %s = %s\n", key, value)
	return nil
}

// applyConfigSet mutates cfg in place per (key, value). Split out from
// runConfigSet so tests can exercise the validation branches without
// touching disk. Unknown keys and bad values return errors; the caller
// only saves when this returns nil.
//
// Validation rules keep the on-disk config sane at write time so the
// daemon doesn't have to re-check on every start: an empty duration
// string is treated as "clear the field" (parity with `config set
// server_url ""` clearing a URL), a non-parseable duration is rejected
// up front rather than being persisted and re-erroring later.
func applyConfigSet(cfg *cli.CLIConfig, key, value string) error {
	switch key {
	case "server_url":
		cfg.ServerURL = value
	case "app_url":
		cfg.AppURL = value
	case "workspace_id":
		cfg.WorkspaceID = value
	case "device_name":
		cfg.DeviceName = value
	case "runtime_name":
		cfg.RuntimeName = value
	case "max_concurrent_tasks":
		if value == "" {
			cfg.MaxConcurrentTasks = 0
			return nil
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("max_concurrent_tasks must be an integer: %w", err)
		}
		if n < 0 {
			return fmt.Errorf("max_concurrent_tasks must be >= 0 (got %d)", n)
		}
		cfg.MaxConcurrentTasks = n
	case "poll_interval":
		if value == "" {
			cfg.PollInterval = ""
			return nil
		}
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("poll_interval must be a Go duration (e.g. 10s, 500ms): %w", err)
		}
		if d < 0 {
			return fmt.Errorf("poll_interval must be non-negative (got %s)", d)
		}
		cfg.PollInterval = value
	default:
		return fmt.Errorf("unknown config key %q (supported: %s)", key, joinKeys(configSetSupportedKeys))
	}
	return nil
}

func valueOrDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func intOrDefault(v int, fallback string) string {
	if v == 0 {
		return fallback
	}
	return strconv.Itoa(v)
}

// joinKeys renders the supported-keys list for the error message. Cheap
// to keep tiny rather than pulling in strings.Join through Sprintf tricks.
func joinKeys(keys []string) string {
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += ", "
		}
		out += k
	}
	return out
}
