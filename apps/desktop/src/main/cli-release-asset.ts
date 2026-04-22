const RELEASE_ARCHIVE_PREFIX = "multica-cli-";

function platformArchiveDescriptor(
  platform: NodeJS.Platform = process.platform,
  arch: string = process.arch,
): { os: string; arch: string; ext: string } {
  const osMap: Record<string, string> = {
    darwin: "darwin",
    linux: "linux",
    win32: "windows",
  };
  const archMap: Record<string, string> = {
    x64: "amd64",
    arm64: "arm64",
  };
  const os = osMap[platform];
  const mappedArch = archMap[arch];
  if (!os || !mappedArch) {
    throw new Error(
      `unsupported platform for CLI auto-install: ${platform}/${arch}`,
    );
  }
  const ext = platform === "win32" ? "zip" : "tar.gz";
  return { os, arch: mappedArch, ext };
}

export function selectPlatformReleaseAssetName(
  assetNames: Iterable<string>,
  platform: NodeJS.Platform = process.platform,
  arch: string = process.arch,
): string {
  const { os, arch: mappedArch, ext } = platformArchiveDescriptor(
    platform,
    arch,
  );
  const names = [...assetNames];

  // Prefer the versioned `multica-cli-<v>-<os>-<arch>.<ext>` name; fall
  // back to the legacy `multica_<os>_<arch>.<ext>` so older releases that
  // only ship the legacy archive keep working.
  const suffix = `-${os}-${mappedArch}.${ext}`;
  const matches = names.filter(
    (name) =>
      name.startsWith(RELEASE_ARCHIVE_PREFIX) && name.endsWith(suffix),
  );

  if (matches.length === 1) {
    return matches[0];
  }
  if (matches.length > 1) {
    throw new Error(
      `multiple release assets matched current platform ${suffix}: ${matches.join(", ")}`,
    );
  }

  const legacyName = `multica_${os}_${mappedArch}.${ext}`;
  if (names.includes(legacyName)) {
    return legacyName;
  }

  throw new Error(`no release asset found for current platform: ${suffix}`);
}
