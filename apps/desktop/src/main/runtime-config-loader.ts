import { app } from "electron";
import { mkdir, readFile, writeFile } from "fs/promises";
import { dirname, join } from "path";
import {
  DEFAULT_RUNTIME_CONFIG,
  parseDesktopConfigFile,
  resolveActiveConfig,
  runtimeConfigFromDevEnv,
  serializeDesktopConfigFile,
  serversStateFromConfig,
  type DesktopServersState,
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
      return {
        ok: true,
        config,
        servers: serversStateFromConfig(config, { editable: false }),
      };
    } catch (err) {
      return { ok: false, error: { message: errorMessage(err) } };
    }
  }

  const configPath = options.configPath ?? desktopConfigPath();
  try {
    const raw = await readFile(configPath, "utf-8");
    const { config, servers } = parseDesktopConfigFile(raw);
    return { ok: true, config, servers: { ...servers, editable: true } };
  } catch (err) {
    if (isMissingFileError(err)) {
      const config = { ...DEFAULT_RUNTIME_CONFIG };
      return {
        ok: true,
        config,
        servers: serversStateFromConfig(config, { editable: true }),
      };
    }
    return {
      ok: false,
      error: {
        message: `Invalid ${configPath}: ${errorMessage(err)}`,
      },
    };
  }
}

/**
 * Persist the multi-server list and active selection to desktop.json.
 * Returns the active RuntimeConfig derived from the new state.
 */
export async function saveDesktopServersState(options: {
  servers: DesktopServersState;
  configPath?: string;
}): Promise<{ config: RuntimeConfig; servers: DesktopServersState }> {
  if (!options.servers.editable) {
    throw new Error("Server list is not editable in this build (dev uses VITE_* env)");
  }
  if (options.servers.servers.length === 0) {
    throw new Error("Server list cannot be empty");
  }

  const configPath = options.configPath ?? desktopConfigPath();
  await mkdir(dirname(configPath), { recursive: true });
  const normalized: DesktopServersState = {
    ...options.servers,
    editable: true,
  };
  await writeFile(configPath, serializeDesktopConfigFile(normalized), "utf-8");
  return {
    config: resolveActiveConfig(normalized),
    servers: normalized,
  };
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

export type { RuntimeConfig, RuntimeConfigResult, DesktopServersState };
