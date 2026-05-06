import path from "path";

import type { LocalDataPaths } from "./local-data-paths";
import type { ResetResult } from "../shared/local-reset-types";

export type { ResetResult };

/**
 * Side-effect surface the reset orchestrator needs. Tests inject fakes;
 * production code wires `removeDir` to `fs.promises.rm({ recursive: true,
 * force: true })`, `stopStack` to the supervisor, and `clearDaemonToken`
 * to whatever daemon-manager exposes.
 */
export type ResetRunner = {
  /** Stop everything that holds files open before deletion. */
  stopStack: () => Promise<void>;
  /** rm -rf the supplied path. Used only for paths in RESETTABLE_KEYS. */
  removeDir: (target: string) => Promise<void>;
  /** Best-effort daemon token clear. Failures are logged, not fatal. */
  clearDaemonToken: () => Promise<void>;
};

export type ResetOptions = {
  paths: LocalDataPaths;
  runner: ResetRunner;
  /** Optional logger so tests can assert log output. */
  logger?: {
    info: (msg: string) => void;
    error: (msg: string, err?: unknown) => void;
  };
};

/**
 * Whitelist of LocalDataPaths keys the reset is allowed to wipe. Defense in
 * depth: even if `paths` is mutated to point one of these keys at /etc, the
 * descendant check below skips it. `appConfig` and `root` are intentionally
 * NOT in this list — deleting Electron's persisted state would nuke user
 * preferences without aiding a debug reset. The plan: "never deletes
 * configured user repo checkouts."
 */
const RESETTABLE_KEYS: readonly (keyof LocalDataPaths)[] = [
  "postgresData",
  "postgresLogs",
  "daemonLogs",
  "appLogs",
];

/**
 * Returns true iff `target` is the same path as `root` or nested under it.
 * Uses `path.relative` so platform-specific separators are handled correctly
 * (the result is "" for the same path, starts with ".." for an outside path).
 */
function isUnderRoot(target: string, root: string): boolean {
  const rel = path.relative(root, target);
  if (rel === "") return false; // same as root — never delete the root itself
  if (rel.startsWith("..")) return false;
  if (path.isAbsolute(rel)) return false; // different drive on Windows
  return true;
}

export async function performLocalReset(
  opts: ResetOptions,
): Promise<ResetResult> {
  const { paths, runner, logger } = opts;
  const removed: string[] = [];
  const skipped: string[] = [];
  const errors: { path: string; error: string }[] = [];

  // 1. Stop the stack first so processes don't keep file handles open.
  //    Best-effort: a failure here is recorded, not fatal — the OS will
  //    forcibly close handles when the files are removed.
  try {
    await runner.stopStack();
    logger?.info("[local-reset] stack stopped");
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    errors.push({ path: "stopStack", error: msg });
    logger?.error("[local-reset] stopStack failed", err);
  }

  // 2. Best-effort daemon token clear. Failures are recorded but never abort.
  try {
    await runner.clearDaemonToken();
    logger?.info("[local-reset] daemon token cleared");
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    errors.push({ path: "clearDaemonToken", error: msg });
    logger?.error("[local-reset] clearDaemonToken failed", err);
  }

  // 3. Walk the resettable keys in declaration order. Skip any path that
  //    isn't a strict descendant of `paths.root` — defense against caller
  //    mutating the paths object to point at an arbitrary directory.
  for (const key of RESETTABLE_KEYS) {
    const target = paths[key];
    if (!target) {
      skipped.push(String(key));
      logger?.info(`[local-reset] skipping ${key} — empty path`);
      continue;
    }

    if (!isUnderRoot(target, paths.root)) {
      skipped.push(target);
      logger?.info(
        `[local-reset] skipping ${key} (${target}) — outside app-owned root`,
      );
      continue;
    }

    try {
      await runner.removeDir(target);
      removed.push(target);
      logger?.info(`[local-reset] removed ${target}`);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      errors.push({ path: target, error: msg });
      logger?.error(`[local-reset] removeDir(${target}) failed`, err);
    }
  }

  return {
    ok: errors.length === 0,
    removed,
    skipped,
    errors,
  };
}
