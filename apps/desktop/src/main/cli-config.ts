import { ipcMain } from "electron";
import { readFile, writeFile, mkdir, rename } from "fs/promises";
import { existsSync } from "fs";
import { dirname, join } from "path";
import { homedir } from "os";
import { randomBytes } from "crypto";

// IPC handlers for the local CLI config under ~/.multica/profiles/<profile>/.
//
// Layer 2 of #3875 (Settings → Runtimes → "Choose OpenClaw instance..."
// dialog) needs the desktop renderer to read/write
//   cfg.backends.openclaw.{binary_path, state_dir}
// on the same file the daemon reads at startup. There is no server-side API
// for this — the override is intentionally per-machine (see PR #4177's
// ProfileCommandOverrides comment), so the only way to mutate it from the
// renderer is via main-process fs IPC.
//
// Directory existence/perm checks are delegated to the existing
// `local-directory:validate` handler in main/local-directory.ts — we
// deliberately don't duplicate that logic here.

// ---- Types shared with renderer ------------------------------------------

export interface OpenClawOverridePayload {
  /** `backends.openclaw.binary_path` from config.json; "" when unset. */
  binaryPath: string;
  /** `backends.openclaw.state_dir` from config.json; "" when unset. */
  stateDir: string;
  /** Live env var as observed by the daemon process (the precedence winner if non-empty). */
  envBinaryPath: string;
  envStateDir: string;
  /** Absolute path of the config.json file we read/wrote, for diagnostics. */
  configPath: string;
}

export interface SaveOpenClawResult {
  ok: boolean;
  /** When ok=false, identifies the failure mode without forcing the renderer to parse free-form text. */
  reason?: "io_error" | "parse_error" | "invalid_input";
  error?: string;
}

// ---- File-system helpers --------------------------------------------------

/**
 * Resolve the config.json path for a given multica profile. Empty profile
 * (the default) resolves to `~/.multica/config.json` per the contract on
 * server/internal/cli/config.go::CLIConfigPathForProfile.
 */
function configPathForProfile(profile: string): string {
  const home = homedir();
  if (!profile) return join(home, ".multica", "config.json");
  return join(home, ".multica", "profiles", profile, "config.json");
}

/**
 * Read JSON from disk and parse to a generic record. Returns {} when the file
 * doesn't exist yet (a fresh install has no config.json until first login).
 * Throws on parse failure so the caller can surface a meaningful error rather
 * than silently overwriting a malformed file.
 */
async function readConfigJson(
  path: string,
): Promise<Record<string, unknown>> {
  if (!existsSync(path)) return {};
  const raw = await readFile(path, "utf8");
  if (!raw.trim()) return {};
  const parsed = JSON.parse(raw);
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("config.json root must be a JSON object");
  }
  return parsed as Record<string, unknown>;
}

/**
 * Atomically write JSON to disk: write to a sibling temp file, fsync via
 * Node's default rename semantics, then replace the target. Matches the
 * pattern in server/internal/cli/config.go::SaveCLIConfigForProfile so the
 * write contract is consistent across Go and Electron code paths.
 *
 * Mode 0o600 keeps the file readable only by the user — same as the Go side.
 */
async function writeConfigJsonAtomic(
  path: string,
  body: Record<string, unknown>,
): Promise<void> {
  const dir = dirname(path);
  await mkdir(dir, { recursive: true });
  const data = JSON.stringify(body, null, 2) + "\n";
  // Random suffix so two concurrent writes can't collide on the same tmp
  // filename. The Go side uses os.CreateTemp which does this implicitly;
  // we mirror that explicitly here.
  const tmp = `${path}.tmp.${randomBytes(6).toString("hex")}`;
  try {
    await writeFile(tmp, data, { mode: 0o600 });
    await rename(tmp, path);
  } catch (err) {
    // Best-effort cleanup; if the write itself failed the tmp might not
    // exist, and we don't want a cleanup failure to mask the real error.
    try {
      const { unlink } = await import("fs/promises");
      await unlink(tmp);
    } catch {
      // ignore
    }
    throw err;
  }
}

/**
 * Pluck `backends.openclaw` out of a parsed config.json, navigating the
 * nullable-pointer chain that PR #3896 introduced. Missing branches yield
 * the empty-string defaults — same semantics the daemon applies.
 */
function extractOpenclawOverride(
  cfg: Record<string, unknown>,
): { binaryPath: string; stateDir: string } {
  const backends = cfg["backends"];
  if (!backends || typeof backends !== "object") {
    return { binaryPath: "", stateDir: "" };
  }
  const openclaw = (backends as Record<string, unknown>)["openclaw"];
  if (!openclaw || typeof openclaw !== "object") {
    return { binaryPath: "", stateDir: "" };
  }
  const oc = openclaw as Record<string, unknown>;
  const binaryPath = typeof oc["binary_path"] === "string" ? (oc["binary_path"] as string) : "";
  const stateDir = typeof oc["state_dir"] === "string" ? (oc["state_dir"] as string) : "";
  return { binaryPath, stateDir };
}

/**
 * Patch `cfg.backends.openclaw.{binary_path, state_dir}` and prune empty
 * branches so the saved file stays minimal. The pruning contract mirrors the
 * Go-side pruneOpenclawOverride() helper added in cmd_runtime_openclaw.go.
 *
 * The function MUTATES `cfg` in place — callers immediately persist it.
 */
function applyOpenclawOverride(
  cfg: Record<string, unknown>,
  binaryPath: string,
  stateDir: string,
): void {
  const backends =
    (cfg["backends"] as Record<string, unknown> | undefined) ?? {};

  if (binaryPath === "" && stateDir === "") {
    // Cleared: drop the openclaw branch entirely.
    delete backends["openclaw"];
  } else {
    const existing =
      (backends["openclaw"] as Record<string, unknown> | undefined) ?? {};
    if (binaryPath !== "") existing["binary_path"] = binaryPath;
    else delete existing["binary_path"];
    if (stateDir !== "") existing["state_dir"] = stateDir;
    else delete existing["state_dir"];
    backends["openclaw"] = existing;
  }

  // If backends now has no live keys, strip it too. Otherwise keep it (a
  // future field — e.g. backends.codex — would render this branch live).
  if (Object.keys(backends).length === 0) {
    delete cfg["backends"];
  } else {
    cfg["backends"] = backends;
  }
}

// ---- IPC registration -----------------------------------------------------

/**
 * Register the cli-config IPC handlers. Called once during main-process
 * startup from index.ts — same lifecycle as setupLocalDirectory().
 *
 * The handlers are intentionally minimal: get/save for the OpenClaw block,
 * plus an existence check for the config file. Anything more (validation,
 * file picker) is provided by local-directory.ts, which the renderer can
 * call alongside these.
 */
export function setupCliConfig(): void {
  ipcMain.handle(
    "cli-config:get-openclaw",
    async (_event, profile: string): Promise<OpenClawOverridePayload> => {
      const path = configPathForProfile(profile ?? "");
      const cfg = await readConfigJson(path);
      const { binaryPath, stateDir } = extractOpenclawOverride(cfg);
      return {
        binaryPath,
        stateDir,
        envBinaryPath: process.env["MULTICA_OPENCLAW_PATH"] ?? "",
        envStateDir: process.env["OPENCLAW_STATE_DIR"] ?? "",
        configPath: path,
      };
    },
  );

  ipcMain.handle(
    "cli-config:save-openclaw",
    async (
      _event,
      args: { profile: string; binaryPath: string; stateDir: string },
    ): Promise<SaveOpenClawResult> => {
      // Defensive input checks: a malformed renderer call (e.g. a non-string
      // path) should not silently write `null` into the JSON file. The Go
      // side also rejects non-string values via its struct tags, but we
      // catch them here too because the renderer ↔ main bridge is JSON-RPC
      // and would happily marshal arbitrary types.
      if (typeof args?.binaryPath !== "string" || typeof args?.stateDir !== "string") {
        return { ok: false, reason: "invalid_input", error: "binary_path and state_dir must be strings" };
      }

      const path = configPathForProfile(args.profile ?? "");
      let cfg: Record<string, unknown>;
      try {
        cfg = await readConfigJson(path);
      } catch (err) {
        return {
          ok: false,
          reason: "parse_error",
          error: errorMessage(err),
        };
      }
      applyOpenclawOverride(cfg, args.binaryPath.trim(), args.stateDir.trim());
      try {
        await writeConfigJsonAtomic(path, cfg);
        return { ok: true };
      } catch (err) {
        return {
          ok: false,
          reason: "io_error",
          error: errorMessage(err),
        };
      }
    },
  );
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

// ---- Exports for unit testing --------------------------------------------
// Keeping the pure helpers exported lets the Vitest suite exercise them
// without spinning up Electron's IPC layer. The handlers themselves are not
// directly unit-testable in jsdom (they capture ipcMain), so test coverage
// rides on these helpers + an integration test from the renderer side.
export const __testing = {
  configPathForProfile,
  extractOpenclawOverride,
  applyOpenclawOverride,
};
