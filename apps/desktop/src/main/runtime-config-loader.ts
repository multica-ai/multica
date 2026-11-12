import { app } from "electron";
import { mkdir, open, readFile, rename, rm } from "fs/promises";
import { dirname, join } from "path";
import { randomUUID } from "crypto";
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
      const config = runtimeConfigFromDevEnv(options.env);
      try {
        const stored = JSON.parse(await readFile(options.configPath ?? desktopConfigPath(), "utf-8"));
        const parsed = parseStoredRuntimeConfig({ proxyUrl: stored.proxyUrl, noProxy: stored.noProxy });
        config.proxyUrl = parsed.proxyUrl;
        config.noProxy = parsed.noProxy;
      } catch (err) {
        if (!isMissingFileError(err)) throw err;
      }
      return { ok: true, config };
    } catch (err) {
      return { ok: false, error: { message: errorMessage(err) } };
    }
  }

  const configPath = options.configPath ?? desktopConfigPath();
  try {
    const raw = await readFile(configPath, "utf-8");
    return { ok: true, config: parseStoredRuntimeConfig(JSON.parse(raw)) };
  } catch (err) {
    if (isMissingFileError(err)) {
      return { ok: true, config: { ...DEFAULT_RUNTIME_CONFIG } };
    }
    return {
      ok: false,
      error: {
        message: `Invalid ${configPath}: Invalid desktop runtime config JSON: ${errorMessage(err)}`,
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

function parseStoredRuntimeConfig(value: unknown): RuntimeConfig {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("Invalid desktop runtime config JSON: expected an object");
  }

  const entries = value as Record<string, unknown>;
  const keys = Object.keys(entries);
  const proxyOnly = keys.length === 0 || keys.every((key) => key === "proxyUrl" || key === "noProxy");

  return parseRuntimeConfig(JSON.stringify({
    schemaVersion: DEFAULT_RUNTIME_CONFIG.schemaVersion,
    ...(proxyOnly ? { apiUrl: DEFAULT_RUNTIME_CONFIG.apiUrl } : {}),
    ...entries,
  }));
}

/**
 * Merge a partial update into the existing desktop.json and write it back.
 * Returns the updated config on success, or an error result.
 */
export async function updateDesktopConfig(
  patch: Pick<RuntimeConfig, "proxyUrl" | "noProxy">,
  configPath?: string,
): Promise<RuntimeConfigResult> {
  const path = configPath ?? desktopConfigPath();
  let current: Record<string, unknown>;
  try {
    const raw = await readFile(path, "utf-8");
    current = JSON.parse(raw);
    if (!current || typeof current !== "object" || Array.isArray(current)) {
      throw new Error("Expected a JSON object");
    }
  } catch (err) {
    if (isMissingFileError(err)) {
      current = {};
    } else {
      return { ok: false, error: { message: errorMessage(err) } };
    }
  }
  const merged = { ...current, proxyUrl: patch.proxyUrl, noProxy: patch.noProxy };
  const temporaryPath = `${path}.${randomUUID()}.tmp`;
  try {
    const config = parseStoredRuntimeConfig(merged);
    await mkdir(dirname(path), { recursive: true });
    const file = await open(temporaryPath, "wx", 0o600);
    try {
      await file.writeFile(JSON.stringify(merged, null, 2) + "\n", "utf-8");
      await file.sync();
    } finally {
      await file.close();
    }
    await rename(temporaryPath, path);
    return { ok: true, config };
  } catch (err) {
    return { ok: false, error: { message: errorMessage(err) } };
  } finally {
    await rm(temporaryPath, { force: true });
  }
}
