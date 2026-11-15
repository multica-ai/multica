import type { CommandLine } from "electron";

/**
 * Restores Electron's intended minimum xdg-desktop-portal FileChooser version
 * on Linux, so `openDirectory` dialogs are not handed to a portal that can only
 * pick files.
 *
 * Why this is needed: Chromium gates the portal correctly on its own
 * (`kXdgPortalRequiredVersion = 3` in
 * `ui/shell_dialogs/select_file_dialog_linux_portal.cc`), but Electron's
 * `feat_add_support_for_missing_dialog_features_to_shell_dialogs.patch`
 * disables that check and routes `IsPortalAvailable()` through its own copy in
 * `shell/browser/ui/file_dialog_linux_portal.cc`. That copy declares
 * `g_required_portal_version = 3` and then overwrites it:
 *
 *   base::StringToUint(cmd->GetSwitchValueASCII("xdg-portal-required-version"),
 *                      &g_required_portal_version);
 *
 * With the switch absent the value is the empty string, and Chromium's
 * `StringToIntImpl` assigns the parse result (0) to the out-param even on
 * failure. The minimum silently drops from 3 to 0, every portal version passes,
 * and the `directory: true` option — only defined from FileChooser v3 — is
 * ignored by v1/v2 portals, which answer with a file chooser instead.
 *
 * Setting the switch ourselves keeps the gate at 3, so hosts with an older
 * portal fall back to Electron's native dialog, where folder selection works.
 *
 * Scope: reproduced in the Electron sources for v39 and still present in v42
 * and v43, so an Electron upgrade alone will not retire this. Before removing
 * it, confirm upstream actually fixed the overwrite.
 * https://github.com/electron/electron/issues/49116
 *
 * Must run at main-script top level: the probe happens later, when
 * `ToolkitInitialized()` reaches `shell_dialog_linux::Initialize()`, and the
 * result is cached for the process lifetime.
 */
export function configureLinuxFileDialogs(
  commandLine: Pick<CommandLine, "hasSwitch" | "appendSwitch">,
  platform: NodeJS.Platform = process.platform,
): void {
  if (platform !== "linux") return;

  // An operator who passes the switch explicitly — including to opt into an
  // older portal — keeps their value.
  const switchName = "xdg-portal-required-version";
  if (!commandLine.hasSwitch(switchName)) {
    commandLine.appendSwitch(switchName, "3");
  }
}
