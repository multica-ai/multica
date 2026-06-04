package execenv

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// PluginSource describes a single plugin to install from a specific marketplace.
// Each entry is self-contained — one plugin, one marketplace URL.
type PluginSource struct {
	MarketplaceURL string `json:"marketplace_url"`
	Plugin         string `json:"plugin"`
}

// setupPlugins is a provider-aware plugin installer dispatcher.
// It routes to the correct implementation based on the provider string.
// Returns nil immediately when plugins is empty or bin is empty.
func setupPlugins(ctx context.Context, provider, bin, workDir string, plugins []PluginSource, logger *slog.Logger) error {
	if len(plugins) == 0 || bin == "" {
		return nil
	}
	switch provider {
	case "csc":
		return setupCSCPlugins(ctx, bin, workDir, plugins, logger)
	default:
		return nil
	}
}

// setupCSCPlugins installs CSC plugins into the task's working directory.
// For each PluginSource it runs:
//
//	1. csc plugin marketplace add <marketplaceURL>
//	2. csc plugin update <plugin>
//	3. csc plugin install <plugin> -s project
//
// All commands run with cmd.Dir set to workDir (CSC uses cwd + scope, not --dir).
func setupCSCPlugins(ctx context.Context, cscBin string, workDir string, plugins []PluginSource, logger *slog.Logger) error {
	if cscBin == "" || len(plugins) == 0 {
		return nil
	}
	for _, p := range plugins {
		// Step 1: marketplace add
		addCtx, addCancel := context.WithTimeout(ctx, 60*time.Second)
		addCmd := exec.CommandContext(addCtx, cscBin, "plugin", "marketplace", "add", p.MarketplaceURL)
		addCmd.Dir = workDir
		var addStderr strings.Builder
		addCmd.Stderr = &addStderr
		if err := addCmd.Run(); err != nil {
			addCancel()
			stderrMsg := strings.TrimSpace(addStderr.String())
			if stderrMsg != "" {
				return fmt.Errorf("csc plugin marketplace add %s: %w (stderr: %s)", p.MarketplaceURL, err, stderrMsg)
			}
			return fmt.Errorf("csc plugin marketplace add %s: %w", p.MarketplaceURL, err)
		}
		addCancel()
		logger.Info("execenv: csc plugin marketplace add ok", "url", p.MarketplaceURL)

		// Step 2: update
		updateCtx, updateCancel := context.WithTimeout(ctx, 60*time.Second)
		updateCmd := exec.CommandContext(updateCtx, cscBin, "plugin", "update", p.Plugin)
		updateCmd.Dir = workDir
		var updateStderr strings.Builder
		updateCmd.Stderr = &updateStderr
		if err := updateCmd.Run(); err != nil {
			updateCancel()
			stderrMsg := strings.TrimSpace(updateStderr.String())
			if stderrMsg != "" {
				return fmt.Errorf("csc plugin update %s: %w (stderr: %s)", p.Plugin, err, stderrMsg)
			}
			return fmt.Errorf("csc plugin update %s: %w", p.Plugin, err)
		}
		updateCancel()
		logger.Info("execenv: csc plugin update ok", "plugin", p.Plugin)

		// Step 3: install with project scope
		installCtx, installCancel := context.WithTimeout(ctx, 120*time.Second)
		installCmd := exec.CommandContext(installCtx, cscBin, "plugin", "install", p.Plugin, "-s", "project")
		installCmd.Dir = workDir
		var installStderr strings.Builder
		installCmd.Stderr = &installStderr
		if err := installCmd.Run(); err != nil {
			installCancel()
			stderrMsg := strings.TrimSpace(installStderr.String())
			if stderrMsg != "" {
				return fmt.Errorf("csc plugin install %s: %w (stderr: %s)", p.Plugin, err, stderrMsg)
			}
			return fmt.Errorf("csc plugin install %s: %w", p.Plugin, err)
		}
		installCancel()
		logger.Info("execenv: csc plugin install ok", "plugin", p.Plugin, "scope", "project")
	}

	return nil
}
