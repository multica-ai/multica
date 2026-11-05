import { verificationDev } from "#verification-dev/main";
import { app } from "electron";
import { homedir } from "node:os";
import { join } from "node:path";
import { bundledCliPath } from "./bundled-cli";
import { DESKTOP_RUNTIME_PROFILE, RuntimeInstanceRegistry } from "./runtime-instance";

let registry: RuntimeInstanceRegistry | undefined;

export function desktopRuntimeRegistry(): RuntimeInstanceRegistry {
  if (registry) return registry;
  const appPath = app.getAppPath();
  registry = new RuntimeInstanceRegistry((verificationDev?.runtimeHome ?? homedir()), {
    kind: app.isPackaged ? "packaged" : "development", appPath,
    cliPath: bundledCliPath(), electronPath: process.execPath,
    claudeEntryPath: app.isPackaged
      ? join(appPath.replace("app.asar", "app.asar.unpacked"), "resources/local-conversation-host/claude-host.cjs")
      : join(appPath, "out/main/claude-host.js"),
  });
  return registry;
}

export function desktopRuntimeProfile(): string { return DESKTOP_RUNTIME_PROFILE; }
