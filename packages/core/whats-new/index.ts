import { api } from "../api";
import { useAuthStore } from "../auth";
import type { User } from "../types";

export interface ReleaseStamp {
  /** MAJOR.MINOR.PATCH, matching the changelog and the server's semver check. */
  version: string;
  /** Release day, YYYY-MM-DD. */
  date: string;
}

type Triple = [number, number, number];

/**
 * Parses MAJOR.MINOR.PATCH, tolerating a leading "v" and any prerelease or
 * build suffix (server builds report e.g. "v0.7.2-12-gabc"). Returns null for
 * anything else, including the empty string dev builds report.
 */
export function parseVersion(raw: string | null | undefined): Triple | null {
  const match = /^v?(\d+)\.(\d+)\.(\d+)/.exec(raw?.trim() ?? "");
  if (!match) return null;
  return [Number(match[1]), Number(match[2]), Number(match[3])];
}

export function compareVersions(a: Triple, b: Triple): number {
  return a[0] - b[0] || a[1] - b[1] || a[2] - b[2];
}

/**
 * Releases the client can announce: those not newer than the connected
 * server, since a self-hosted server can lag the client bundle and must not
 * advertise features it doesn't run. An unknown server version (managed
 * cloud, dev builds) applies no cap. Result is newest first.
 */
export function visibleReleases<R extends ReleaseStamp>(
  releases: readonly R[],
  serverVersion: string,
): R[] {
  const cap = parseVersion(serverVersion);
  return releases
    .filter((r) => {
      const v = parseVersion(r.version);
      return v !== null && (cap === null || compareVersions(v, cap) <= 0);
    })
    .sort((a, b) => compareVersions(parseVersion(b.version)!, parseVersion(a.version)!));
}

/**
 * Releases the user hasn't seen. Before the first dismissal, only releases
 * after the account's creation day count, so a new user isn't greeted with
 * the whole backlog. Returns [] when the server can't persist the marker
 * (`whats_new_seen_version` undefined): an auto-opening card that can never
 * be dismissed would reappear on every launch.
 */
export function unreadReleases<R extends ReleaseStamp>(
  visible: readonly R[],
  user: Pick<User, "whats_new_seen_version" | "created_at"> | null,
): R[] {
  if (!user || user.whats_new_seen_version === undefined) return [];
  const seen = parseVersion(user.whats_new_seen_version);
  if (seen) {
    return visible.filter((r) => compareVersions(parseVersion(r.version)!, seen) > 0);
  }
  const joined = user.created_at.slice(0, 10);
  if (!joined) return [];
  return visible.filter((r) => r.date > joined);
}

/**
 * Persists the dismissal and syncs the auth store. Applied optimistically so
 * the rail dot clears at once; a failed write resurfaces it on next launch.
 * The server only advances the marker, so the returned user may carry a
 * newer version than `version`.
 */
export async function markWhatsNewSeen(version: string): Promise<void> {
  const current = useAuthStore.getState().user;
  if (current) useAuthStore.getState().setUser({ ...current, whats_new_seen_version: version });
  const user = await api.markWhatsNewSeen(version);
  if (user.id) useAuthStore.getState().setUser(user);
}
