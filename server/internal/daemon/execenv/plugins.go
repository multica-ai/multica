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
// Each entry is self-contained — one plugin, one marketplace URL and name.
type PluginSource struct {
	MarketplaceURL  string `json:"marketplace_url"`
	MarketplaceName string `json:"marketplace_name"`
	Plugin          string `json:"plugin"`
}

// setupPlugins is a provider-aware plugin installer dispatcher.
// It routes to the correct implementation based on the provider string.
// Returns nil immediately when bin is empty.
func setupPlugins(ctx context.Context, provider, bin, workDir string, plugins []PluginSource, logger *slog.Logger) error {
	if bin == "" {
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
//  1. csc plugin marketplace add <marketplaceURL>        (non-fatal)
//  2. csc plugin marketplace update <marketplaceName>
//  3. csc plugin install <plugin>@<marketplaceName> -s local
//  4. csc plugin update <plugin>@<marketplaceName> -s local
//
// All commands run with cmd.Dir set to workDir (CSC uses cwd + scope, not --dir).
// marketplace add failure is non-fatal: the marketplace may already be registered.
// When plugins is empty, hardcoded defaults are used (remove in Phase 2).
func setupCSCPlugins(ctx context.Context, cscBin string, workDir string, plugins []PluginSource, logger *slog.Logger) error {
	if cscBin == "" {
		return nil
	}
	// TODO(Phase 2): remove this fallback once the server populates Task.Plugins.
	if len(plugins) == 0 {
		plugins = []PluginSource{
			{
				MarketplaceURL:  "https://github.com/costrict-plugins-repo/marketplace.git",
				MarketplaceName: "costrict-plugins",
				Plugin:          "cospowers-requirements",
			},
		}
		logger.Info("execenv: using hardcoded CSC plugin defaults (server did not provide plugins)")
	}
	for _, p := range plugins {
		// Step 1: marketplace add (non-fatal — may already be registered)
		if err := runCSCCmd(ctx, cscBin, workDir, "plugin", "marketplace", "add", p.MarketplaceURL); err != nil {
			logger.Error("execenv: csc plugin marketplace add failed", "url", p.MarketplaceURL, "error", err)
		}

		// Step 2: marketplace update
		if err := runCSCCmd(ctx, cscBin, workDir, "plugin", "marketplace", "update", p.MarketplaceName); err != nil {
			return fmt.Errorf("csc plugin marketplace update %s: %w", p.MarketplaceName, err)
		}

		// Step 3: install with local scope
		spec := p.Plugin
		if p.MarketplaceName != "" {
			spec = p.Plugin + "@" + p.MarketplaceName
		}
		if err := runCSCCmd(ctx, cscBin, workDir, "plugin", "install", spec, "-s", "local"); err != nil {
			return fmt.Errorf("csc plugin install %s: %w", spec, err)
		}

		// Step 4: update installed plugin
		if err := runCSCCmd(ctx, cscBin, workDir, "plugin", "update", spec, "-s", "local"); err != nil {
			return fmt.Errorf("csc plugin update %s: %w", spec, err)
		}
	}

	return nil
}

// runCSCCmd executes a csc CLI command with the given arguments.
func runCSCCmd(ctx context.Context, cscBin, workDir string, args ...string) error {
	cmdCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, cscBin, args...)
	cmd.Dir = workDir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}
