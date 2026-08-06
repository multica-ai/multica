import { homedir } from "os";
import { join } from "path";

// Keep the Go impl in sync: server/cmd/multica/cmd_daemon.go healthPortForProfile.
export const DEFAULT_HEALTH_PORT = 19514;

/**
 * Empty string is the "target API URL not known yet" state, which exists
 * between main-process startup and the renderer reporting its `apiUrl`.
 *
 * It is NOT a profile name. Desktop owns only `~/.multica/profiles/desktop-<host>/`;
 * the default profile at `~/.multica/` belongs to the user's terminal CLI and
 * must never be read, written, or passed to the bundled CLI. Resolving an empty
 * name to that directory silently rewrote the user's own `server_url` and
 * `token`, so every path that could reach the filesystem fails loudly instead.
 * See #6399.
 */
export function assertResolvedProfile(profile: string): void {
  if (!profile) {
    throw new Error(
      "daemon profile is unresolved — refusing to fall back to the default CLI profile",
    );
  }
}

export function isResolvedProfile(profile: string): boolean {
  return Boolean(profile);
}

// Desktop owns a dedicated CLI profile named after the target API host, so it
// never reads or writes the user's hand-configured profiles. Profile dir:
//   ~/.multica/profiles/desktop-<host>/
export function deriveProfileName(targetUrl: string): string {
  try {
    const url = new URL(targetUrl);
    const host = url.host.replace(/:/g, "-").toLowerCase();
    return `desktop-${host}`;
  } catch {
    return "desktop";
  }
}

export function healthPortForProfile(profile: string): number {
  if (!profile) return DEFAULT_HEALTH_PORT;
  let sum = 0;
  for (const b of Buffer.from(profile, "utf-8")) sum += b;
  return DEFAULT_HEALTH_PORT + 1 + (sum % 1000);
}

export function profileDir(profile: string): string {
  assertResolvedProfile(profile);
  return join(homedir(), ".multica", "profiles", profile);
}

export function profileConfigPath(profile: string): string {
  return join(profileDir(profile), "config.json");
}

export function profileLogPath(profile: string): string {
  return join(profileDir(profile), "daemon.log");
}

// Sidecar file that records which Multica user the cached PAT in config.json
// was minted for. The Go CLI/daemon never read or write this file, so it
// survives Go-side config rewrites. Used to detect user switches and mint a
// fresh PAT instead of reusing a token that belongs to a previous user.
export function profileUserIdPath(profile: string): string {
  return join(profileDir(profile), ".desktop-user-id");
}

/**
 * CLI args selecting the Desktop-owned profile. An unresolved profile must
 * never produce an empty arg list: the bundled CLI would then act on the
 * user's default profile at `~/.multica/config.json`.
 */
export function profileArgs(profile: string): string[] {
  assertResolvedProfile(profile);
  return ["--profile", profile];
}
