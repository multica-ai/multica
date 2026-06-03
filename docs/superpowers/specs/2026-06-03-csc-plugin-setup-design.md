# CSC Plugin Setup Design

**Date:** 2026-06-03
**Branch:** `feat/csc-cloud-cli-dga-bridge`
**Status:** Draft

## Overview

Bridge the CSC Cloud ↔ CSC CLI task dispatch pipeline by adding a plugin
installation step to the Multica Daemon's task execution flow. When a CSC
agent picks up a task, the Daemon installs required CSC plugins into the
task's isolated working directory before spawning the agent process.

This is the first phase: hardcoded plugin names and marketplace URL.
Future phases will make these configurable and eventually server-driven.

## Requirements

1. When `provider == "csc"`, the Daemon must install CSC plugins into the
   task's working directory before the agent process starts.
2. Installation runs two CSC CLI commands:
   - `csc plugin marketplace add <marketplaceURL>`
   - `csc plugin install <pluginName>@<source> --dir <workdir>`
3. If either command fails, the task fails immediately and the error is
   reported back to the Multica Server via the existing `FailTask` mechanism.
4. The `Reuse()` path skips plugin installation (the working directory is
   already populated from a previous task).
5. Changes are confined to `execenv/` and a small wiring change in
   `daemon.go`. No Server-side or frontend changes required.

## Architecture

### Execution flow (before)

```
execenv.Prepare()
  ├── Create directory structure
  ├── writeContextFiles()        ← Skills, Issue context
  ├── [codex]    setupCodexHome()
  └── [openclaw] setupOpenclawConfig()
```

### Execution flow (after)

```
execenv.Prepare()
  ├── Create directory structure
  ├── writeContextFiles()        ← Skills, Issue context
  ├── [codex]    setupCodexHome()
  ├── [openclaw] setupOpenclawConfig()
  └── [csc]      setupCSCPlugins()   ← NEW
                     ├── exec("csc plugin marketplace add <url>")
                     └── exec("csc plugin install <name>@costrict-plugins --dir <workdir>")
```

### Error propagation

```
setupCSCPlugins() returns error
  → execenv.Prepare() returns error
    → daemon.runTask() returns (TaskResult{}, error)
      → daemon.handleTask() calls FailTask(taskID, "csc plugin setup: ...", "", "", "agent_error")
        → Server records failure, UI shows error to user
```

## Files changed

| File | Type | Description |
|---|---|---|
| `server/internal/daemon/execenv/execenv.go` | Modify | Add CSC branch in `Prepare()` and `ReuseParams` |
| `server/internal/daemon/execenv/csc_plugins.go` | **New** | `setupCSCPlugins()` implementation |
| `server/internal/daemon/execenv/csc_plugins_test.go` | **New** | Unit tests |
| `server/internal/daemon/execenv/execenv.go` | Modify | Add `CSCBin` field to `PrepareParams` |
| `server/internal/daemon/daemon.go` | Modify | Pass `CSCBin` from config to `PrepareParams` |

**No changes to:** `agent/csc.go`, `agent/agent.go`, Server handlers, frontend.

## Detailed design

### `execenv/csc_plugins.go`

```go
package execenv

import (
    "context"
    "fmt"
    "log/slog"
    "os/exec"
    "time"
)

const (
    cscMarketplaceURL = "https://github.com/costrict-plugins-repo/marketplace.git"
    cscPluginSource   = "costrict-plugins"
)

// Phase 1: fixed plugin list.
var cscDefaultPlugins = []string{
    "cospower",
}

// setupCSCPlugins installs CSC plugins into the task's working directory.
// It runs two CSC CLI commands sequentially:
//   1. csc plugin marketplace add <marketplaceURL>
//   2. csc plugin install <pluginName>@<source> --dir <workdir>
//
// Both commands must succeed. On failure, returns an error describing which
// step failed and why. The caller (Prepare) propagates this to runTask →
// handleTask → FailTask so the server records the failure.
func setupCSCPlugins(ctx context.Context, cscBin string, workDir string, logger *slog.Logger) error {
    // Step 1: marketplace add
    addCtx, addCancel := context.WithTimeout(ctx, 60*time.Second)
    defer addCancel()

    addCmd := exec.CommandContext(addCtx, cscBin, "plugin", "marketplace", "add", cscMarketplaceURL)
    // ... hideAgentWindow, capture stderr
    if err := addCmd.Run(); err != nil {
        return fmt.Errorf("csc plugin marketplace add %s: %w", cscMarketplaceURL, err)
    }
    logger.Info("execenv: csc plugin marketplace add ok", "url", cscMarketplaceURL)

    // Step 2: plugin install
    for _, name := range cscDefaultPlugins {
        installCtx, installCancel := context.WithTimeout(ctx, 120*time.Second)
        spec := fmt.Sprintf("%s@%s", name, cscPluginSource)
        installCmd := exec.CommandContext(installCtx, cscBin, "plugin", "install", spec, "--dir", workDir)
        // ... hideAgentWindow, capture stderr
        err := installCmd.Run()
        installCancel()
        if err != nil {
            return fmt.Errorf("csc plugin install %s: %w", spec, err)
        }
        logger.Info("execenv: csc plugin install ok", "plugin", spec)
    }

    return nil
}
```

### `execenv/execenv.go` — Prepare() changes

Add `CSCBin` to `PrepareParams`:

```go
type PrepareParams struct {
    // ... existing fields ...
    OpenclawBin string  // resolved openclaw CLI path
    CSCBin      string  // resolved csc CLI path (empty = skip plugin setup)
    // ...
}
```

Add CSC branch at the end of `Prepare()`, alongside existing provider branches:

```go
if provider == "csc" && params.CSCBin != "" {
    if err := setupCSCPlugins(ctx, params.CSCBin, env.WorkDir, logger); err != nil {
        return nil, fmt.Errorf("csc plugin setup: %w", err)
    }
}
```

### `daemon/daemon.go` — runTask() changes

Extract CSC binary path (same pattern as `openclawBin`):

```go
cscBin := ""
if provider == "csc" {
    cscBin = entry.Path
}
```

Pass to `PrepareParams`:

```go
env, err = execenv.Prepare(execenv.PrepareParams{
    // ... existing fields ...
    OpenclawBin:  openclawBin,
    CSCBin:       cscBin,        // NEW
    // ...
}, d.logger)
```

Also pass `CSCBin` through `ReuseParams` when the `Reuse()` path is used
(skip plugin setup on reuse — the directory is already populated):

```go
env = execenv.Reuse(execenv.ReuseParams{
    // ... existing fields ...
    // No CSCBin needed — reuse skips plugin installation
}, d.logger)
```

## Error handling

| Scenario | Handling | Reason |
|---|---|---|
| `cscBin == ""` | Skip setup entirely | CSC binary not available, no point trying |
| marketplace add fails | Return error → FailTask → Server | Without marketplace, install cannot proceed |
| plugin install fails | Return error → FailTask → Server | Without plugin, task execution is meaningless |
| Command timeout (60s/120s) | Context cancel → return error → FailTask | Unresponsive CLI should not block indefinitely |

Error messages sent to the server include the failing step and stderr output,
e.g.:
- `csc plugin marketplace add https://github.com/.../marketplace.git: exit status 1 (stderr: ...)`
- `csc plugin install cospower@costrict-plugins: timeout after 120s`

## Testing

### Unit tests (`csc_plugins_test.go`)

| Test | Verifies |
|---|---|
| `TestSetupCSCPlugins_Success` | Both commands execute with correct arguments, returns nil |
| `TestSetupCSCPlugins_MarketplaceAddFails` | Returns error containing "marketplace add" |
| `TestSetupCSCPlugins_InstallFails` | Returns error containing "plugin install" |
| `TestSetupCSCPlugins_CSCBinEmpty` | Returns immediately, no commands executed |
| `TestSetupCSCPlugins_Timeout` | Context cancellation returns error |

Tests use a fake executable (script that succeeds/fails on demand) via
`ExecOptions.ExecutablePath`, following the pattern in `daemon_test.go`.

### Integration test (manual)

1. Ensure CSC CLI is on PATH with `csc plugin` subcommands available.
2. Start Daemon with a CSC agent configured.
3. Trigger a task assignment from the Multica UI.
4. Verify:
   - Plugin installed in task workdir.
   - Task executes via CSC CLI with plugin available.
   - On forced failure (e.g. bad marketplace URL), task shows failure in UI.

## Future evolution

```
Phase 1 (this PR)       Phase 2                Phase 3
──────────────────      ──────────────────     ──────────────────
Hardcoded constants     Config-driven params    Server-driven
┌────────────────┐     ┌────────────────┐     ┌────────────────┐
│ marketplaceURL │     │ marketplaceURL │     │ Task.PluginReqs
│ pluginName    │ ──→ │ plugins []     │ ──→ │   {name, source
│ (hardcoded)   │     │ (config file)  │     │    version}    │
└────────────────┘     └────────────────┘     │ (Server sends) │
                                              └────────────────┘
Files: csc_plugins.go   Files: config.go       Files: handler/daemon.go
No external changes     + PrepareParams        + Task type
                                               + execenv reads
```

The function signature naturally evolves — hardcoded constants become
parameters without structural changes:

```go
// Phase 1
setupCSCPlugins(ctx, cscBin, workDir, logger)

// Phase 2 (add params from config)
setupCSCPlugins(ctx, cscBin, workDir, marketplaceURL, plugins, logger)

// Phase 3 (params from Task, passed via PrepareParams)
setupCSCPlugins(ctx, cscBin, workDir, params.PluginReqs, logger)
```
