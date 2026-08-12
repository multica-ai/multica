/**
 * Semver comparison for the desktop/web ↔ server version-mismatch warning
 * (#5848). Pure and read-only: it never blocks a feature, it only classifies
 * the relationship so the UI can decide whether to surface a hint.
 *
 * Only major/minor are compared — patch drift between an installed client
 * and its server is normal during rolling deploys and must not warn.
 */

export type VersionRelation =
  /** App is newer than the server (server may lack endpoints the app calls). */
  | "app_newer"
  /** Server is newer than the app (client update flow owns this case). */
  | "server_newer"
  | "equal"
  /** Either side is missing or unparseable — never warn on unknown. */
  | "unknown";

/** "0.1.0" is the electron-builder dev placeholder, not a real release. */
const DEV_PLACEHOLDER_VERSIONS = new Set(["0.1.0"]);

function parseMajorMinor(
  version: string | undefined | null,
): { major: number; minor: number } | null {
  if (!version) return null;
  const trimmed = version.trim();
  if (DEV_PLACEHOLDER_VERSIONS.has(trimmed)) return null;
  // Loose semver: optional leading "v", optional patch / prerelease / build.
  // Anything else ("dev", "unknown", a bare commit sha) is unparseable.
  const match = /^v?(\d+)\.(\d+)(?:\.\d+)?(?:[-+].*)?$/.exec(trimmed);
  if (!match) return null;
  return { major: Number(match[1]), minor: Number(match[2]) };
}

export function compareVersions(
  appVersion: string | undefined | null,
  serverVersion: string | undefined | null,
): VersionRelation {
  const app = parseMajorMinor(appVersion);
  const server = parseMajorMinor(serverVersion);
  if (!app || !server) return "unknown";
  if (app.major !== server.major) {
    return app.major > server.major ? "app_newer" : "server_newer";
  }
  if (app.minor !== server.minor) {
    return app.minor > server.minor ? "app_newer" : "server_newer";
  }
  return "equal";
}
