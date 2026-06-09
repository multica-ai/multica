/**
 * Detecting a daemon the desktop app can't manage.
 *
 * The app reads daemon liveness over HTTP at 127.0.0.1:{port}/health, but it
 * starts/stops the daemon by shelling out to the bundled native CLI, which acts
 * on the *host* OS process namespace. On Windows with the daemon running inside
 * WSL2, /health is reachable via localhost forwarding (so status looks fine) but
 * the daemon's process lives in a separate Linux namespace the Windows CLI can't
 * touch — so auto-start / auto-stop silently do nothing (#3916).
 *
 * The reliable, low-false-positive signal is the daemon's own OS (reported as
 * `os` on /health, = runtime.GOOS) vs the desktop host OS. They only disagree
 * when the daemon runs in a foreign environment we can't drive. This module is
 * the single source of truth for that comparison so it stays unit-tested — the
 * cost of a false positive is hiding a working toggle from a native user, so the
 * logic must fail safe (treat unknown / matching as manageable).
 */

/**
 * Normalize a Node `process.platform` value to the daemon's `runtime.GOOS`
 * vocabulary so the two are directly comparable. Only `win32` -> `windows`
 * actually differs across the platforms we ship (darwin/linux already match);
 * any other value passes through unchanged.
 */
export function normalizeHostOS(platform: NodeJS.Platform): string {
  return platform === "win32" ? "windows" : platform;
}

/**
 * Whether a running daemon is in an environment the desktop app can't control.
 *
 * Returns true ONLY when the daemon reports a concrete OS that differs from the
 * host's. Fails safe to false when:
 *   - `daemonOS` is missing/empty (older daemon that predates the `os` field, or
 *     a malformed response) — we can't prove it's foreign, so keep toggles live.
 *   - the OSes match — a normally-managed native daemon.
 *
 * Callers must only invoke this for a daemon that is actually running; a stopped
 * daemon has no OS to compare and its toggles must stay enabled.
 */
export function isDaemonExternallyManaged(
  daemonOS: string | undefined,
  hostOS: string,
): boolean {
  if (typeof daemonOS !== "string" || daemonOS.length === 0) return false;
  return daemonOS !== hostOS;
}
