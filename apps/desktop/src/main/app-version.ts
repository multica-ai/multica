import { app } from "electron";
import { execSync } from "node:child_process";

/**
 * Resolve the running app version. In packaged builds this is the value
 * `electron-builder` baked into package.json via `extraMetadata.version`
 * (driven by `git describe` — see `apps/desktop/scripts/package.mjs`), so
 * `app.getVersion()` matches the GitHub Release tag exactly.
 *
 * In dev (`pnpm dev:desktop`) `app.getVersion()` only sees the static
 * `apps/desktop/package.json` value, which is "0.1.0" and never bumped —
 * the Settings → Updates panel and any other UI surfacing the version
 * would mislead developers into thinking they're running ancient builds.
 * Fall back to `git describe --tags --always --dirty` (same source the
 * packager uses) so dev shows e.g. `0.2.19-14-gabcdef-dirty`. If git is
 * unavailable for whatever reason, we just return the package.json value.
 */
export function getAppVersion(): string {
  if (app.isPackaged) {
    return app.getVersion();
  }
  try {
    const raw = execSync("git describe --tags --always --dirty", {
      cwd: app.getAppPath(),
      encoding: "utf-8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
    if (!raw) return app.getVersion();
    return raw.replace(/^v/, "");
  } catch {
    return app.getVersion();
  }
}
