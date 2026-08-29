import { app } from "electron";
import { readFile } from "fs/promises";
import { join } from "path";
import {
  DEFAULT_RUNTIME_CONFIG,
  LIFEOS_RUNTIME_CONFIG,
  parseRuntimeConfig,
  runtimeConfigFromDevEnv,
  type RuntimeConfig,
  type RuntimeConfigEnv,
  type RuntimeConfigResult,
} from "../shared/runtime-config";
import type { DesktopFlavor } from "../shared/desktop-flavor";

export async function loadRuntimeConfig(options: {
  isDev: boolean;
  env: RuntimeConfigEnv;
  configPath?: string;
  flavor?: DesktopFlavor;
}): Promise<RuntimeConfigResult> {
  if (options.isDev) {
    try {
      return { ok: true, config: runtimeConfigFromDevEnv(options.env) };
    } catch (err) {
      return { ok: false, error: { message: errorMessage(err) } };
    }
  }

  const flavor = options.flavor ?? "multica";
  const configPath = options.configPath ?? desktopConfigPath(flavor);
  try {
    const raw = await readFile(configPath, "utf-8");
    return { ok: true, config: parseRuntimeConfig(raw) };
  } catch (err) {
    if (isMissingFileError(err)) {
      return {
        ok: true,
        config: {
          ...(flavor === "lifeos"
            ? LIFEOS_RUNTIME_CONFIG
            : DEFAULT_RUNTIME_CONFIG),
        },
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

export function desktopConfigPath(flavor: DesktopFlavor = "multica"): string {
  if (flavor === "lifeos") {
    return join(app.getPath("userData"), "desktop.json");
  }
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
