package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
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
	"heartbeat_interval",
	"agent_timeout",
	"codex_semantic_inactivity_timeout",
	"codex_handshake_timeout",
	"disable_auto_update",
	"auto_update_check_interval",
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a CLI configuration value",
	Long: "Supported keys: " +
		"server_url, app_url, workspace_id, " +
		"device_name, runtime_name, max_concurrent_tasks, poll_interval, " +
		"heartbeat_interval, agent_timeout, " +
		"codex_semantic_inactivity_timeout, codex_handshake_timeout, " +
		"disable_auto_update, auto_update_check_interval.\n\n" +
		"The daemon keys (device_name, runtime_name, max_concurrent_tasks, " +
		"poll_interval, heartbeat_interval, agent_timeout, " +
		"codex_semantic_inactivity_timeout, codex_handshake_timeout, " +
		"disable_auto_update, auto_update_check_interval) mirror their " +
		"--flag / env counterparts and are read by `daemon start` when " +
		"neither the flag nor the env var is set. " +
		"Precedence: --flag > MULTICA_… env > config.json > built-in default. " +
		"Duration keys take a positive Go duration (e.g. '10s', '500ms', '1m30s'); " +
		"'0s' and negative values are rejected — except agent_timeout, where " +
		"'0s' is meaningful and explicitly disables the wall-clock cap. " +
		"disable_auto_update takes 'true' or 'false' (single-direction: setting " +
		"it to 'true' turns auto-update off, 'false' clears the override so " +
		"env/default decides). Pass an empty string to clear a persisted " +
		"value (e.g. `config set poll_interval \"\"`).",
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
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "server_url:", valueOrDefault(cfg.ServerURL, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "app_url:", valueOrDefault(cfg.AppURL, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "workspace_id:", valueOrDefault(cfg.WorkspaceID, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "device_name:", valueOrDefault(cfg.DeviceName, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "runtime_name:", valueOrDefault(cfg.RuntimeName, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "max_concurrent_tasks:", intOrDefault(cfg.MaxConcurrentTasks, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "poll_interval:", valueOrDefault(cfg.PollInterval, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "heartbeat_interval:", valueOrDefault(cfg.HeartbeatInterval, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "agent_timeout:", agentTimeoutDisplay(cfg.AgentTimeout))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "codex_semantic_inactivity_timeout:", valueOrDefault(cfg.CodexSemanticInactivityTimeout, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "codex_handshake_timeout:", valueOrDefault(cfg.CodexHandshakeTimeout, "(not set)"))
	fmt.Fprintf(os.Stdout, "%-34s %t\n", "disable_auto_update:", cfg.DisableAutoUpdate)
	fmt.Fprintf(os.Stdout, "%-34s %s\n", "auto_update_check_interval:", valueOrDefault(cfg.AutoUpdateCheckInterval, "(not set)"))
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
		// Reject zero and negative durations. Persisting "0s" would look
		// configured in `config show` but be silently ignored at daemon
		// start (the resolver only substitutes strictly positive values),
		// which is exactly the trap reported in #3824's review. Empty
		// string is the one and only way to clear a previously persisted
		// value.
		if d <= 0 {
			return fmt.Errorf("poll_interval must be positive (got %s); use `config set poll_interval \"\"` to clear it", d)
		}
		cfg.PollInterval = value
	case "heartbeat_interval":
		if err := assignPositiveDuration(&cfg.HeartbeatInterval, key, value); err != nil {
			return err
		}
	case "agent_timeout":
		// agent_timeout is the one duration knob where "0s" is a
		// meaningful persisted value (it explicitly disables the
		// wall-clock cap; see cli.CLIConfig.AgentTimeout). Store the raw
		// string via a pointer so we can distinguish "not set" (nil)
		// from "disabled" (non-nil, "0s") and any positive value.
		if value == "" {
			cfg.AgentTimeout = nil
			return nil
		}
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("agent_timeout must be a Go duration (e.g. 10m, 0s to disable): %w", err)
		}
		if d < 0 {
			return fmt.Errorf("agent_timeout must be >= 0 (got %s); use 0s to disable the cap or \"\" to clear the persisted value", d)
		}
		s := value
		cfg.AgentTimeout = &s
	case "codex_semantic_inactivity_timeout":
		if err := assignPositiveDuration(&cfg.CodexSemanticInactivityTimeout, key, value); err != nil {
			return err
		}
	case "codex_handshake_timeout":
		if err := assignPositiveDuration(&cfg.CodexHandshakeTimeout, key, value); err != nil {
			return err
		}
	case "disable_auto_update":
		if value == "" {
			cfg.DisableAutoUpdate = false
			return nil
		}
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("disable_auto_update must be 'true' or 'false' (got %q)", value)
		}
		cfg.DisableAutoUpdate = b
	case "auto_update_check_interval":
		if err := assignPositiveDuration(&cfg.AutoUpdateCheckInterval, key, value); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown config key %q (supported: %s)", key, joinKeys(configSetSupportedKeys))
	}
	return nil
}

// assignPositiveDuration parses value as a strictly-positive Go duration
// and writes the raw string into dst. Shared by every persisted daemon
// duration knob except agent_timeout, whose zero value is meaningful.
// Empty string clears the field.
func assignPositiveDuration(dst *string, key, value string) error {
	if value == "" {
		*dst = ""
		return nil
	}
	normalized := strings.TrimSpace(value)
	d, err := time.ParseDuration(normalized)
	if err != nil {
		return fmt.Errorf("%s must be a Go duration (e.g. 10s, 500ms): %w", key, err)
	}
	if d <= 0 {
		return fmt.Errorf("%s must be positive (got %s); use `config set %s \"\"` to clear it", key, d, key)
	}
	*dst = normalized
	return nil
}

// agentTimeoutDisplay renders the tri-state agent_timeout value for
// `config show`. nil = not persisted (fall through to env/default);
// non-nil "0s" = explicitly disabled; any other non-nil = the persisted
// duration string.
func agentTimeoutDisplay(v *string) string {
	if v == nil {
		return "(not set)"
	}
	if *v == "" {
		return "(not set)"
	}
	if d, err := time.ParseDuration(*v); err == nil && d == 0 {
		return *v + " (disabled)"
	}
	return *v
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
