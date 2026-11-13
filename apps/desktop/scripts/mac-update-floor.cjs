// electron-builder `beforePack` hook: publish a macOS floor in the update
// metadata (latest-mac.yml / latest-x64-mac.yml).
//
// Why this exists: Electron 44+ cannot launch on macOS 12. Installed clients
// decide whether to download an update with the code they ALREADY run, so a
// runtime check inside the new app can never protect a macOS 12 user — only
// the published metadata can. electron-updater skips an update when
// `os.release()` (the Darwin kernel version, not the marketing version) is
// below `minimumSystemVersion`, but electron-builder 26 never writes that field
// and its config schema rejects it under `mac.releaseInfo`.
//
// electron-builder spreads `packager.platformSpecificBuildOptions.releaseInfo`
// into every update-info file it generates for that platform, and reads it
// after config validation. Setting it here, before packing, puts the floor
// into both macOS channels before anything is uploaded.
//
// Darwin 22 = macOS 13 Ventura. Do NOT write "13.0.0": it is compared against
// the kernel version, so Darwin 21 (macOS 12) would pass a 13.0.0 floor.
const MAC_UPDATE_MIN_DARWIN_VERSION = "22.0.0";

function applyMacUpdateFloor(context) {
  if (context.electronPlatformName !== "darwin") return;
  const options = context.packager && context.packager.platformSpecificBuildOptions;
  if (options == null || typeof options !== "object") {
    // Fail the build rather than silently publishing metadata without the floor.
    throw new Error(
      "[mac-update-floor] packager.platformSpecificBuildOptions is unavailable; " +
        "electron-builder internals changed — cannot publish minimumSystemVersion",
    );
  }
  options.releaseInfo = {
    ...(options.releaseInfo || {}),
    minimumSystemVersion: MAC_UPDATE_MIN_DARWIN_VERSION,
  };
}

module.exports = applyMacUpdateFloor;
module.exports.default = applyMacUpdateFloor;
module.exports.applyMacUpdateFloor = applyMacUpdateFloor;
module.exports.MAC_UPDATE_MIN_DARWIN_VERSION = MAC_UPDATE_MIN_DARWIN_VERSION;
