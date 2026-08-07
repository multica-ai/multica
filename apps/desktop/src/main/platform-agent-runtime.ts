import { join } from "path";

export const PLATFORM_AGENT_CLI_PATH_ENV = "MULTICA_PLATFORM_AGENT_CLI_PATH";
export const PLATFORM_AGENT_CLI_DESKTOP_BUNDLED_ONLY_ENV =
  "MULTICA_PLATFORM_AGENT_CLI_DESKTOP_BUNDLED_ONLY";

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
