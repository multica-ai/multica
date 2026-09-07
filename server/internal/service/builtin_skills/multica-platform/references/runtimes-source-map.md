# Runtime setup source map

- Explicit Desktop install, progress, recovery coordination, and startup:
  `apps/desktop/src/main/daemon-manager.ts`.
- Human/local install command and task-context guard:
  `server/cmd/multica/cmd_daemon.go`.
- Pinned download, checksum verification, extraction, and activation:
  `server/internal/managedruntime/descriptor.go` and `installer.go`.
- Local-machine matching and model connection form:
  `packages/views/runtimes/components/built-in-runtime-setup.ts`,
  `built-in-runtime-offer.tsx`, and `built-in-runtime-connect-form.tsx`.
- Model connection validation, permissions, persistence, and non-secret reads:
  `server/internal/handler/runtime_model_connection.go`,
  `runtime_model_connection_validate.go`, and `server/internal/piagent/probe.go`.
- Agent override precedence and task-scoped configuration:
  `server/internal/handler/daemon_pi_connection.go`,
  `server/internal/daemon/pi_runtime_config.go`, and
  `server/internal/daemon/execenv/pi_home.go`.
