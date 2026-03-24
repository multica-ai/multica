package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current CLI configuration",
	RunE:  runConfigShow,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a CLI configuration value",
	Long:  "Supported keys: server_url, workspace_id",
	Args:  cobra.ExactArgs(2),
	RunE:  runConfigSet,
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configSetCmd)
}

func runConfigShow(_ *cobra.Command, _ []string) error {
	cfg, err := cli.LoadCLIConfig()
	if err != nil {
		return err
	}

	path, _ := cli.CLIConfigPath()
	fmt.Fprintf(os.Stdout, "Config file: %s\n", path)
	fmt.Fprintf(os.Stdout, "server_url:   %s\n", valueOrDefault(cfg.ServerURL, "(not set)"))
	fmt.Fprintf(os.Stdout, "workspace_id: %s\n", valueOrDefault(cfg.WorkspaceID, "(not set)"))
	return nil
}

func runConfigSet(_ *cobra.Command, args []string) error {
	key, value := args[0], args[1]

	cfg, err := cli.LoadCLIConfig()
	if err != nil {
		return err
	}

	switch key {
	case "server_url":
		cfg.ServerURL = value
	case "workspace_id":
		cfg.WorkspaceID = value
	default:
		return fmt.Errorf("unknown config key %q (supported: server_url, workspace_id)", key)
	}

	if err := cli.SaveCLIConfig(cfg); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Set %s = %s\n", key, value)
	return nil
}

func valueOrDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
