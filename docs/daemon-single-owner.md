# Single-daemon ownership and version compatibility

A machine may run at most **one** Multica agent daemon at a time. Two daemons on
one host — most commonly the CLI-spawned daemon and the Desktop-spawned daemon
running under different profiles — share the same local checkouts and the same
in-process local-directory mutex, so both can write the same repository at once
and corrupt it (VWO-364 / VWO-365).

This is enforced by a **machine-global advisory lock** and a **client/server
version gate**. This document is the operator reference for both.

## How ownership is enforced

- On startup, before it binds its health port or registers any runtime, a
  daemon takes an exclusive advisory lock on `~/.multica/daemon.lock`.
- That path lives in the **base** config directory, which every profile shares,
  so the lock is contended across profiles — unlike the per-profile health-port
  and PID-file guards, which never see a daemon under a different profile.
- The lock is held for the life of the process via an open file handle. The OS
  releases it automatically when the process exits — cleanly **or on a crash** —
  so there is no lease TTL to tune and no stale-lock window. A crashed owner's
  lock is gone the instant the process dies.
- A second daemon that tries to start while an owner is live fails immediately
  with an actionable error naming the incumbent (pid, version, profile, health
  port) instead of silently double-registering.
- The launcher also refuses to start while a daemon that does NOT hold the lock
  is alive on any known profile's health port — that can only be a daemon from a
  release predating single-daemon ownership (or one started under the
  break-glass env). This closes the rolling-upgrade window: when upgrading, stop
  every old daemon (`multica [--profile <name>] daemon stop`) before starting
  the new one; the sweep enforces it rather than silently running alongside.

## How version compatibility is enforced

- The server reports its build version in the daemon registration response
  (`server_version`). The daemon compares it against the minimum server version
  it requires (currently the first release carrying the task `prepare-lease`
  route) and, if the server is **older**, fails to start with a clear error
  rather than 404-looping on unsupported routes forever.
- A server that reports **no** version (older than this field, or built without
  a version stamp) is treated as "unknown": the daemon warns and proceeds. If it
  then hits a missing route (a bare `prepare-lease` 404), it logs one loud error
  and stops that retry loop — it never floods the log with repeated 404s.

## Operator verification

Confirm exactly one daemon owns the machine:

```bash
# 1. Exactly one daemon process.
pgrep -fl 'multica daemon start --foreground'

# 2. The lock records the live owner (pid, health port, version, profile).
cat ~/.multica/daemon.lock

# 3. The recorded pid is the running daemon.
ps -p "$(python3 -c 'import json,os;print(json.load(open(os.path.expanduser("~/.multica/daemon.lock")))["pid"])')"

# 4. Health endpoint agrees.
multica daemon status
```

Confirm the second daemon is refused (safe to run — it will not start):

```bash
# With a daemon already running, this must exit non-zero with an ownership error:
multica daemon start --profile some-other-profile
# => "another Multica daemon already owns this machine (pid …) … `multica daemon stop` … `--takeover`"
```

Confirm version compatibility on a self-hosted server:

```bash
# The server build the daemon negotiated against.
multica daemon logs | grep -Ei 'server version|incompatible|prepare-lease'
```

## Handing ownership over (supported takeover)

To replace the running daemon (e.g. switch the owner from the CLI daemon to the
Desktop daemon) without editing any file or database row:

```bash
# Ask the current owner to stop, wait for it to release, then start:
multica daemon start --takeover
# (or `multica daemon restart` for a same-profile stop+start)
```

`--takeover` reads the incumbent's health port from the lock file, requests a
graceful shutdown over HTTP (cross-platform; no OS signals), waits up to 45s for
the lock to free (sized to the incumbent's full graceful shutdown: a 30s
in-flight task drain plus deregistration), then acquires it. If the incumbent
does not release in time it fails loudly with a manual remedy — it never
force-kills, since that would skip the task drain and could orphan agent
subprocesses mid-write.

## Rollback / break-glass

The ownership guard can be disabled without redeploying:

```bash
# Restore the pre-change behavior (NO single-owner guard). Only for a deliberate
# multi-backend setup that accepts the shared-checkout risk, or emergency rollback.
export MULTICA_DAEMON_ALLOW_MULTIPLE=1
multica daemon start
```

With the variable set (any value other than empty / `0` / `false`), the daemon
skips lock acquisition and logs a loud warning on every start. Unset it and
restart to restore the guard. The version gate is independent and is not
affected by this variable.

## Recovery scenarios

| Situation | What happens | Operator action |
|---|---|---|
| Owner exits cleanly | Lock released on shutdown | Next daemon starts normally |
| Owner crashes / is killed | OS drops the lock on process death | Next daemon starts immediately; no cleanup |
| Leftover `daemon.lock` from a dead owner | File content is advisory only; no live lock | Next daemon acquires and overwrites it; no manual deletion |
| Second daemon attempted | Refused with an actionable error | Use `daemon stop`, or `daemon start --takeover` |
| Old (pre-lock) daemon still running during an upgrade | New daemon refuses to start alongside it | Stop the old daemon (`multica [--profile <name>] daemon stop`), then start |
| Daemon newer than server | Startup fails with a version error (or one loud `prepare-lease` error) | Upgrade the server, or run a matching daemon |
