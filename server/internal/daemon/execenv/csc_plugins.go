package execenv

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

const (
	// cscMarketplaceURL is the hardcoded CSC plugin marketplace repository URL.
	// Phase 1: hardcoded. Phase 2: configurable. Phase 3: server-driven.
	cscMarketplaceURL = "https://github.com/costrict-plugins-repo/marketplace.git"
	// cscPluginSource is the CSC plugin source identifier used in install specs.
	cscPluginSource = "costrict-plugins"
)

// cscDefaultPlugins is the Phase 1 hardcoded list of plugins to install.
var cscDefaultPlugins = []string{
	"cospower",
}

// setupCSCPlugins installs CSC plugins into the task's working directory.
// It runs CSC CLI commands sequentially:
//
//	1. csc plugin marketplace add <marketplaceURL>
//	2. csc plugin install <pluginName>@<source> --dir <workdir>
//
// Both commands must succeed. On failure, returns an error describing which
// step failed and why. The caller (Prepare) propagates this to runTask ->
// handleTask -> FailTask so the server records the failure.
//
// When cscBin is empty, the function returns nil immediately without
// executing any commands (the CSC binary is not available on this host).
func setupCSCPlugins(ctx context.Context, cscBin string, workDir string, logger *slog.Logger) error {
	if cscBin == "" {
		return nil
	}

	// Step 1: marketplace add
	addCtx, addCancel := context.WithTimeout(ctx, 60*time.Second)
	defer addCancel()

	addCmd := exec.CommandContext(addCtx, cscBin, "plugin", "marketplace", "add", cscMarketplaceURL)
	var addStderr strings.Builder
	addCmd.Stderr = &addStderr
	if err := addCmd.Run(); err != nil {
		stderrMsg := strings.TrimSpace(addStderr.String())
		if stderrMsg != "" {
			return fmt.Errorf("csc plugin marketplace add %s: %w (stderr: %s)", cscMarketplaceURL, err, stderrMsg)
		}
		return fmt.Errorf("csc plugin marketplace add %s: %w", cscMarketplaceURL, err)
	}
	logger.Info("execenv: csc plugin marketplace add ok", "url", cscMarketplaceURL)

	// Step 2: plugin install
	for _, name := range cscDefaultPlugins {
		installCtx, installCancel := context.WithTimeout(ctx, 120*time.Second)
		spec := fmt.Sprintf("%s@%s", name, cscPluginSource)
		installCmd := exec.CommandContext(installCtx, cscBin, "plugin", "install", spec, "--dir", workDir)
		var installStderr strings.Builder
		installCmd.Stderr = &installStderr
		err := installCmd.Run()
		installCancel()
		if err != nil {
			stderrMsg := strings.TrimSpace(installStderr.String())
			if stderrMsg != "" {
				return fmt.Errorf("csc plugin install %s: %w (stderr: %s)", spec, err, stderrMsg)
			}
			return fmt.Errorf("csc plugin install %s: %w", spec, err)
		}
		logger.Info("execenv: csc plugin install ok", "plugin", spec, "dir", workDir)
	}

	return nil
}
