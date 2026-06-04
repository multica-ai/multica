package execenv

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// AgentPlugin describes the plugin bound to an agent.
// Single plugin per agent — nil means no plugin is configured.
type AgentPlugin struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Install *PluginInstall `json:"install,omitempty"`
}

// PluginInstall describes how to install a plugin from a marketplace.
type PluginInstall struct {
	Method              string `json:"method"`                // e.g. "plugin_marketplace"
	Marketplace         string `json:"marketplace"`           // e.g. "anthropics/claude-plugins-official"
	PluginName          string `json:"plugin_name"`           // e.g. "superpowers"
	MarketplaceName     string `json:"marketplace_name"`      // e.g. "claude-plugins-official"
	MarketplaceRepo     string `json:"marketplace_repo"`      // e.g. "anthropics/claude-plugins-official"
	MarketplaceVerified bool   `json:"marketplace_verified"`  // e.g. true
}

// setupPlugins is a provider-aware plugin installer dispatcher.
// It routes to the correct implementation based on the provider string.
// Returns nil immediately when bin is empty or plugin is nil.
func setupPlugins(ctx context.Context, provider, bin, workDir string, plugin *AgentPlugin, logger *slog.Logger) error {
	if bin == "" || plugin == nil || plugin.Install == nil {
		return nil
	}
	switch provider {
	case "csc":
		return setupCSCPlugins(ctx, bin, workDir, plugin, logger)
	default:
		return nil
	}
}

// setupCSCPlugins installs a CSC plugin into the task's working directory.
// It runs the following commands:
//
//  1. csc plugin marketplace add <marketplaceRepo>        (non-fatal)
//  2. csc plugin marketplace update <marketplaceName>
//  3. csc plugin install <pluginName>@<marketplaceName> -s local
//  4. csc plugin update <pluginName>@<marketplaceName> -s local
//
// All commands run with cmd.Dir set to workDir (CSC uses cwd + scope, not --dir).
// marketplace add failure is non-fatal: the marketplace may already be registered.
func setupCSCPlugins(ctx context.Context, cscBin string, workDir string, plugin *AgentPlugin, logger *slog.Logger) error {
	if cscBin == "" || plugin == nil || plugin.Install == nil {
		return nil
	}
	install := plugin.Install

	// Step 1: marketplace add (non-fatal — may already be registered)
	if err := runCSCCmd(ctx, cscBin, workDir, "plugin", "marketplace", "add", install.MarketplaceRepo); err != nil {
		logger.Error("execenv: csc plugin marketplace add failed", "repo", install.MarketplaceRepo, "error", err)
	}

	// Step 2: marketplace update
	if err := runCSCCmd(ctx, cscBin, workDir, "plugin", "marketplace", "update", install.MarketplaceName); err != nil {
		return fmt.Errorf("csc plugin marketplace update %s: %w", install.MarketplaceName, err)
	}

	// Step 3: install with local scope
	spec := install.PluginName
	if install.MarketplaceName != "" {
		spec = install.PluginName + "@" + install.MarketplaceName
	}
	if err := runCSCCmd(ctx, cscBin, workDir, "plugin", "install", spec, "-s", "local"); err != nil {
		return fmt.Errorf("csc plugin install %s: %w", spec, err)
	}

	// Step 4: update installed plugin
	if err := runCSCCmd(ctx, cscBin, workDir, "plugin", "update", spec, "-s", "local"); err != nil {
		return fmt.Errorf("csc plugin update %s: %w", spec, err)
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
