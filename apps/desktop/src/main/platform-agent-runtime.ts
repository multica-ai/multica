import { join } from "path";

export const PLATFORM_AGENT_CLI_PATH_ENV = "MULTICA_PLATFORM_AGENT_CLI_PATH";
export const PLATFORM_AGENT_CLI_DESKTOP_BUNDLED_ONLY_ENV =
  "MULTICA_PLATFORM_AGENT_CLI_DESKTOP_BUNDLED_ONLY";
export const PLATFORM_AGENT_MODE_ENV = "PLATFORM_AGENT_MODE";

function deleteEnvKeyCaseInsensitive(
  env: NodeJS.ProcessEnv,
  keyToDelete: string,
): void {
  const normalizedKey = keyToDelete.toUpperCase();
  for (const key of Object.keys(env)) {
    if (key.toUpperCase() === normalizedKey) {
      delete env[key];
    }
  }
}

function explicitPlatformAgentMode(
  env: NodeJS.ProcessEnv,
  platform: NodeJS.Platform,
): { configured: boolean; value: string } {
  const canonical = env[PLATFORM_AGENT_MODE_ENV];
  if (canonical !== undefined) {
    return { configured: true, value: canonical.trim() };
  }
  if (platform !== "win32") return { configured: false, value: "" };
  const normalizedKey = PLATFORM_AGENT_MODE_ENV.toUpperCase();
  for (const [key, value] of Object.entries(env)) {
    if (key.toUpperCase() === normalizedKey && value !== undefined) {
      return { configured: true, value: value.trim() };
    }
  }
  return { configured: false, value: "" };
}

export function bundledPlatformAgentPath(
  appPath: string,
  platform: NodeJS.Platform,
): string {
  const binaryName =
    platform === "win32" ? "platform-agent-cli.exe" : "platform-agent-cli";
  return join(appPath, "resources", "bin", binaryName).replace(
    "app.asar",
    "app.asar.unpacked",
  );
}

export function withBundledPlatformAgentPath(
  sourceEnv: NodeJS.ProcessEnv,
  appPath: string,
  platform: NodeJS.Platform,
  exists: (path: string) => boolean,
): NodeJS.ProcessEnv {
  const childEnv = { ...sourceEnv };
  const configuredMode = explicitPlatformAgentMode(childEnv, platform);
  if (platform === "win32") {
    deleteEnvKeyCaseInsensitive(childEnv, PLATFORM_AGENT_MODE_ENV);
  }
  childEnv[PLATFORM_AGENT_MODE_ENV] = configuredMode.configured
    ? configuredMode.value
    : "mock";
  deleteEnvKeyCaseInsensitive(childEnv, PLATFORM_AGENT_CLI_PATH_ENV);
  deleteEnvKeyCaseInsensitive(
    childEnv,
    PLATFORM_AGENT_CLI_DESKTOP_BUNDLED_ONLY_ENV,
  );
  childEnv[PLATFORM_AGENT_CLI_DESKTOP_BUNDLED_ONLY_ENV] = "1";

  const bundledPath = bundledPlatformAgentPath(appPath, platform);
  if (exists(bundledPath)) {
    childEnv[PLATFORM_AGENT_CLI_PATH_ENV] = bundledPath;
  }

  return childEnv;
}
