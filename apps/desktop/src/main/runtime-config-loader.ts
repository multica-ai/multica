import { app } from "electron";
import { readFile, writeFile } from "fs/promises";
import { join } from "path";
import {
  DEFAULT_RUNTIME_CONFIG,
  parseRuntimeConfig,
  runtimeConfigFromDevEnv,
  type RuntimeConfig,
  type RuntimeConfigEnv,
  type RuntimeConfigResult,
} from "../shared/runtime-config";

export async function loadRuntimeConfig(options: {
  isDev: boolean;
  env: RuntimeConfigEnv;
  configPath?: string;
}): Promise<RuntimeConfigResult> {
  if (options.isDev) {
    try {
      return { ok: true, config: runtimeConfigFromDevEnv(options.env) };
    } catch (err) {
      return { ok: false, error: { message: errorMessage(err) } };
    }
  }

  const configPath = options.configPath ?? desktopConfigPath();
  try {
    const raw = await readFile(configPath, "utf-8");
    return { ok: true, config: parseRuntimeConfig(raw) };
  } catch (err) {
    if (isMissingFileError(err)) {
      return { ok: true, config: { ...DEFAULT_RUNTIME_CONFIG } };
    }
    return {
      ok: false,
      error: {
        message: `Invalid ${configPath}: ${errorMessage(err)}`,
      },
    };
  }
}

export function desktopConfigPath(): string {
  return join(app.getPath("home"), ".multica", "desktop.json");
}

function isMissingFileError(err: unknown): boolean {
  return Boolean(
    err &&
      typeof err === "object" &&
      "code" in err &&
      (err as NodeJS.ErrnoException).code === "ENOENT",
  );
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export type { RuntimeConfig, RuntimeConfigResult };

/**
 * Merge a partial update into the existing desktop.json and write it back.
 * Returns the updated config on success, or an error result.
 */
export async function updateDesktopConfig(
  patch: Partial<RuntimeConfig>,
  configPath?: string,
): Promise<RuntimeConfigResult> {
  const path = configPath ?? desktopConfigPath();
  let current: RuntimeConfig;
  try {
    const raw = await readFile(path, "utf-8");
    current = parseRuntimeConfig(raw);
  } catch (err) {
    if (isMissingFileError(err)) {
      current = { ...DEFAULT_RUNTIME_CONFIG };
    } else {
      return { ok: false, error: { message: errorMessage(err) } };
    }
  }
  const merged: RuntimeConfig = { ...current, ...patch };
  try {
    await writeFile(path, JSON.stringify(merged, null, 2) + "\n", "utf-8");
  } catch (err) {
    return { ok: false, error: { message: errorMessage(err) } };
  }
  return { ok: true, config: merged };
}
