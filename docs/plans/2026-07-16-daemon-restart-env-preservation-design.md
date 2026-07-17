# Daemon Restart Environment Preservation Design

## Problem

A plain `multica daemon restart` stops the running daemon and launches a new
daemon from the caller process. That changes ownership of restart configuration:
the new process inherits the caller's executable, flags, and environment instead
of the daemon's. In WS-2062 this removed `MULTICA_SKILL_TRACE_ENABLED`,
`MULTICA_SKILL_TRACE_PATH`, and `MULTICA_DAEMON_AUTO_UPDATE=false`, which stopped
skill telemetry while leaving the daemon otherwise healthy.

The Loop-1 assertion runner also writes its issue description under the machine
temporary directory. Multica now rejects `--description-file` paths outside the
task workdir, so automatic triage creation fails even when the assertion is
correct.

## Options

1. Copy a fixed list of environment variables in the CLI caller. This is brittle:
   each new daemon setting can regress independently, and the caller still may
   use a different executable or start flags.
2. Add launchd-specific restart logic. This preserves the configured environment
   on macOS but does not solve the same ownership bug on Linux or Windows.
3. Let the running daemon hand off to its successor for plain restarts. This
   reuses the existing self-update restart path, preserving the daemon's own
   executable, start flags, profile, and complete environment. This is the
   selected approach.

## Design

The daemon exposes a localhost-only `POST /restart` endpoint beside `/shutdown`.
The handler schedules the existing graceful restart path and responds before
cancelling the daemon context. A plain background `multica daemon restart` calls
this endpoint and waits until the health endpoint reports a different ready PID.

If the user supplies restart flags or requests foreground mode, the CLI retains
the existing stop-then-start behavior because those overrides intentionally come
from the caller. This keeps explicit flag semantics while making the common
restart path environment-safe.

The assertion runner creates its temporary description directory beneath the
configured blackboard repository, passes that in-workdir path to Multica, and
removes the directory after the command finishes.

## Verification

- Go handler tests prove `POST /restart` schedules a restart and non-POST methods
  are rejected.
- CLI tests prove plain restart selects self-handoff while explicit overrides do
  not.
- Bun tests prove triage descriptions are inside the runner workdir and cleaned
  up.
- Focused Go and Bun suites must pass before operational verification.

