import { join } from "path";

export const PLATFORM_AGENT_CLI_PATH_ENV = "MULTICA_PLATFORM_AGENT_CLI_PATH";

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
  delete childEnv[PLATFORM_AGENT_CLI_PATH_ENV];

  const bundledPath = bundledPlatformAgentPath(appPath, platform);
  if (exists(bundledPath)) {
    childEnv[PLATFORM_AGENT_CLI_PATH_ENV] = bundledPath;
  }

  return childEnv;
}
