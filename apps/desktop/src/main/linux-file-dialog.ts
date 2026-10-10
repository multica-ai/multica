import type { CommandLine } from "electron";

/** Run before app readiness, when Electron probes and caches portal support. */
export function configureLinuxFileDialogs(
  commandLine: Pick<CommandLine, "hasSwitch" | "appendSwitch">,
  platform: NodeJS.Platform = process.platform,
): void {
  if (platform !== "linux") return;

  // Directory selection requires FileChooser portal v3. Electron 39 parses a
  // missing switch into zero, admitting older portals that only select files.
  // An explicit minimum lets Electron use its native dialog on older systems.
  // https://github.com/electron/electron/issues/49116
  const switchName = "xdg-portal-required-version";
  if (!commandLine.hasSwitch(switchName)) {
    commandLine.appendSwitch(switchName, "3");
  }
}
