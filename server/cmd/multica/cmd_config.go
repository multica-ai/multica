package main

import (
	"fmt"
	"os"
	"strings"

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

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a CLI configuration value",
	Long:  "Supported keys: server_url, app_url, workspace_id, auto_publish, publish_remote",
	Args:  exactArgs(2),
	RunE:  runConfigSet,
}

var configLocalCmd = &cobra.Command{
	Use:   "local",
	Short: "Configure CLI for a local Docker Compose deployment",
	Long:  "Sets server_url and app_url to localhost defaults for a local self-hosted deployment.",
	RunE:  runConfigLocal,
}

func init() {
	configLocalCmd.Flags().Int("port", 8080, "Backend server port")
	configLocalCmd.Flags().Int("frontend-port", 3000, "Frontend port")

	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configLocalCmd)
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
	fmt.Fprintf(os.Stdout, "server_url:   %s\n", valueOrDefault(cfg.ServerURL, "(not set)"))
	fmt.Fprintf(os.Stdout, "app_url:      %s\n", valueOrDefault(cfg.AppURL, "(not set)"))
	fmt.Fprintf(os.Stdout, "workspace_id: %s\n", valueOrDefault(cfg.WorkspaceID, "(not set)"))
	fmt.Fprintf(os.Stdout, "auto_publish: %t\n", cfg.AutoPublish)
	fmt.Fprintf(os.Stdout, "publish_remote: %s\n", valueOrDefault(cfg.PublishRemote, "(default origin)"))
	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key, value := args[0], args[1]

	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return err
	}

	switch key {
	case "server_url":
		cfg.ServerURL = value
	case "app_url":
		cfg.AppURL = value
	case "workspace_id":
		cfg.WorkspaceID = value
	case "auto_publish":
		v, err := parseBoolString(value)
		if err != nil {
			return err
		}
		cfg.AutoPublish = v
	case "publish_remote":
		cfg.PublishRemote = value
	default:
		return fmt.Errorf("unknown config key %q (supported: server_url, app_url, workspace_id, auto_publish, publish_remote)", key)
	}

	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Set %s = %s\n", key, value)
	return nil
}

func parseBoolString(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value %q (use true/false, 1/0, yes/no, or on/off)", raw)
	}
}

func runConfigLocal(cmd *cobra.Command, _ []string) error {
	port, _ := cmd.Flags().GetInt("port")
	frontendPort, _ := cmd.Flags().GetInt("frontend-port")

	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return err
	}

	cfg.AppURL = fmt.Sprintf("http://localhost:%d", frontendPort)
	cfg.ServerURL = fmt.Sprintf("http://localhost:%d", port)

	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Configured for local deployment:\n")
	fmt.Fprintf(os.Stderr, "  app_url:    %s\n", cfg.AppURL)
	fmt.Fprintf(os.Stderr, "  server_url: %s\n", cfg.ServerURL)
	fmt.Fprintf(os.Stderr, "\nNext: run 'multica login' to authenticate.\n")
	return nil
}

func valueOrDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
