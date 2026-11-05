import { readFile } from "node:fs/promises";
import { homedir } from "os";
import { join } from "path";

// Keep the Go impl in sync: server/cmd/multica/cmd_daemon.go healthPortForProfile.
export const DEFAULT_HEALTH_PORT = 19514;

/**
 * Desktop uses `~/.multica/profiles/desktop-runtime/` and a separate runtime
 * registration directory. The default profile
 * at `~/.multica/` — config, daemon log, and health port 19514 — belongs to the
 * user's terminal CLI and must never be read, written, probed, or passed to the
 * bundled CLI.
 *
 * Callers signal "target API URL not known yet" with `null`, never with an
 * empty name, so there is no value that can quietly resolve to the default
 * profile. This guard is the backstop for that. See #6399.
 */
export function assertResolvedProfile(profile: string): void {
  if (!profile) {
    throw new Error(
      "daemon profile is unresolved — refusing to fall back to the default CLI profile"
    );
  }
}

// Legacy profile naming retained for standalone discovery and existing tests.
export function deriveProfileName(targetUrl: string): string {
  try {
    const url = new URL(targetUrl);
    const host = url.host.replace(/:/g, "-").toLowerCase();
    return `desktop-${host}`;
  } catch {
    return "desktop";
  }
}

/**
 * Port 19514 itself is the default profile's, so an unresolved profile must
 * never produce one: probing it would report the user's own CLI daemon as
 * Desktop's, and lifecycle commands would act on it.
 */
export function healthPortForProfile(profile: string): number {
  assertResolvedProfile(profile);
  if (process.env.MULTICA_DEV_ISOLATED === "1") {
    const port = Number(process.env.MULTICA_DEV_HEALTH_PORT);
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      throw new Error(
        "Isolated development requires a valid MULTICA_DEV_HEALTH_PORT"
      );
    }
    return port;
  }
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
  return join(profileStateDir(profile), "daemon.log");
}

/** The daemon keeps local conversation data under the profile directory, not its state directory. */
export function profileDaemonTimingPath(profile: string): string {
  return join(
    profileDir(profile),
    "local-conversations",
    "diagnostics",
    "daemon-timing.jsonl"
  );
}

export function profilePidPath(profile: string): string {
  return join(profileStateDir(profile), "daemon.pid");
}

export function profilePortPath(profile: string): string {
  const registered = profile === "desktop-runtime";
  return join(
    profileStateDir(profile),
    registered ? "runtime.json" : "daemon.port"
  );
}

function profileStateDir(profile: string): string {
  assertResolvedProfile(profile);
  return profile === "desktop-runtime"
    ? join(homedir(), ".multica", "desktop-runtime")
    : profileDir(profile);
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

/** Credentials Desktop synced into its named profile; throws when absent. */
export async function readDesktopProfileCredentials(
  profile: string
): Promise<{ userId: string; token: string; serverUrl: string }> {
  assertResolvedProfile(profile);
  const userId = (await readFile(profileUserIdPath(profile), "utf8")).trim();
  const config = JSON.parse(await readFile(profileConfigPath(profile), "utf8"));
  return { userId, token: config.token, serverUrl: config.server_url };
}
