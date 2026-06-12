package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/pkg/detsteps"
	"github.com/multica-ai/multica/server/pkg/dettools"
)

var mcpToolsCmd = &cobra.Command{
	Use:   "mcp-tools",
	Short: "Deterministic tool plane (MCP server)",
	Long: "Multica's deterministic tool plane. These tools are normally launched " +
		"automatically by an agent runtime via the daemon-injected mcp_config; the " +
		"command is exposed here so it can be run and inspected directly.",
}

var mcpToolsServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the deterministic tools MCP server over stdio",
	Long: "Speak MCP (Model Context Protocol) over stdio, exposing Multica's " +
		"read-only deterministic tool catalog. Configuration is " +
		"read from MULTICA_DETTOOLS_* environment variables set by the daemon.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		// stdout is the JSON-RPC channel — every log line MUST go to stderr or it
		// will corrupt the protocol stream.
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

		opts := dettools.OptionsFromEnv()
		reg := dettools.NewRegistry(opts.AllowedTools)

		// Register workspace-authored steps the daemon delivered for this task,
		// alongside the built-in catalog. Fail-open: a bad steps file logs and the
		// server still serves the built-ins.
		if steps, err := detsteps.LoadStepsFile(opts.StepsFile); err != nil {
			logger.Warn("dettools: failed to load workspace steps; serving built-ins only", "error", err)
		} else {
			for _, t := range detsteps.Tools(detsteps.SelfBin(), steps) {
				if !reg.Add(t) {
					logger.Warn("dettools: workspace step shadows a built-in tool; skipping", "name", t.Name)
				}
			}
			if len(steps) > 0 {
				logger.Info("dettools: registered workspace steps", "count", len(steps))
			}
		}

		env := dettools.ToolEnv{
			WorkDir:      opts.WorkDir,
			AllowNetwork: opts.AllowNetwork,
			Timeout:      opts.Timeout,
			ArtifactDir:  opts.ArtifactDir,
			Logger:       logger,
		}
		info := dettools.ServerInfo{Name: "multica-tools", Version: version}
		logger.Info("dettools: serving", "work_dir", opts.WorkDir, "tools", opts.AllowedTools)
		return dettools.Serve(cmd.Context(), os.Stdin, os.Stdout, reg, info, env, logger)
	},
}

func init() {
	mcpToolsCmd.AddCommand(mcpToolsServeCmd)
}
