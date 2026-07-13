#!/usr/bin/env node
// Wrapper around `electron-builder` that keeps the Desktop version in
// lockstep with the CLI. Both are derived from `git describe --tags
// --match 'v[0-9]*' --always --dirty` — the same source GoReleaser reads
// for the CLI
// binary via the `main.version` ldflag — so a single `vX.Y.Z` tag push
// produces matching CLI and Desktop versions.
//
// Builds the Electron bundles once, then for each requested target
// (platform + arch) compiles the matching Go CLI into resources/bin/ and
// invokes electron-builder with `-c.extraMetadata.version=<derived>` so
// the override applies at build time without mutating the tracked
// package.json.
//
// The electron-vite step is important: electron-builder only packages
// whatever is already in out/, so skipping it (or relying on stale
// artifacts from a prior partial build) ships an app with missing
// renderer code and white-screens on launch.
//
// Extra CLI args after `pnpm package --` are forwarded to electron-builder
// unchanged (e.g. `--mac --arm64`). For an unsigned local smoke-test
// build, set `CSC_IDENTITY_AUTO_DISCOVERY=false` so electron-builder falls
// back to an ad-hoc signature instead of requiring a Developer ID cert.
//
// The `normalizeGitVersion`, `deriveVersion`, and `DESCRIBE_ARGS` exports let
// tests cover version derivation both as a pure string transform and as the
// real `git describe` invocation against a throwaway repo.

import { execFileSync, spawnSync } from "node:child_process";
import { delimiter, dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const desktopRoot = resolve(here, "..");
const bundleCliScript = resolve(here, "bundle-cli.mjs");

const PLATFORM_CONFIG = {
  mac: {
    aliases: new Set(["--mac", "--macos", "-m"]),
    builderFlag: "--mac",
    runtimePlatform: "darwin",
    label: "macOS",
  },
  win: {
    aliases: new Set(["--win", "--windows", "-w"]),
    builderFlag: "--win",
    runtimePlatform: "win32",
    label: "Windows",
  },
  linux: {
    aliases: new Set(["--linux", "-l"]),
    builderFlag: "--linux",
    runtimePlatform: "linux",
    label: "Linux",
  },
};

const ARCH_FLAGS = new Map([
  ["--x64", "x64"],
  ["--arm64", "arm64"],
  ["--ia32", "ia32"],
  ["--armv7l", "armv7l"],
  ["--universal", "universal"],
]);

const SUPPORTED_CLI_ARCHS = new Set(["x64", "arm64"]);
const MAC_ALL_PLATFORM_TARGETS = [
  { platform: "mac", arch: "arm64" },
  { platform: "win", arch: "x64" },
  { platform: "win", arch: "arm64" },
  { platform: "linux", arch: "x64" },
  { platform: "linux", arch: "arm64" },
];

// Run a git subcommand with its arguments handed straight to the binary,
// never through a shell. A match pattern like `v[0-9]*` must reach git as a
// single literal argument on every platform. Passing the whole command as a
// shell string (execSync) is unsafe on Windows: cmd.exe does not strip the
// POSIX single quotes around 'v[0-9]*', so git receives the quotes verbatim,
// matches no tag, and the version silently degrades to the 0.0.0-g<hash>
// fallback — which is exactly how a `0.0.0-…` Windows Desktop build once
// escaped to a GitHub Release.
function git(args, cwd) {
  try {
    return execFileSync("git", args, { encoding: "utf-8", cwd }).trim();
  } catch {
    return "";
  }
}

/**
 * Strip the leading `--` that npm/pnpm insert to separate their own
 * flags from the ones meant for the underlying script.  Without this,
 * `pnpm package -- --mac --arm64 --publish always` forwards the bare
 * `--` into electron-builder's argv, which terminates option parsing
 * and turns `--publish always` into ignored positional arguments.
 */
export function stripLeadingSeparator(argv) {
  if (argv.length > 0 && argv[0] === "--") return argv.slice(1);
  return argv;
}

/**
 * Pure transformation from the `git describe --tags --always --dirty`
 * output to the value we feed into electron-builder's extraMetadata.version.
 *
 *   - empty input              → null   (caller should fall back)
 *   - "v0.1.36"                → "0.1.36"
 *   - "v0.1.35-14-gf1415e96"   → "0.1.35-14-gf1415e96"  (semver prerelease)
 *   - "v0.1.35-…-dirty"        → same, dirty suffix preserved
 *   - "f1415e96" (no tag)      → "0.0.0-gf1415e96"       (fallback)
 *   - "2f24057b" (no tag, hash begins with a digit) → "0.0.0-g2f24057b"
 *   - "0123456"  (no tag, all-digit hash w/ leading zero) → "0.0.0-g0123456"
 *
 * Leading `v` is stripped so the result is valid semver for package.json.
 * The fallback matters because a bare commit hash is never valid semver —
 * even one that happens to start with a digit (e.g. "2f24057b") — and
 * electron-updater throws on launch if package.json carries such a version.
 * The hash is prefixed with `g` so the pre-release identifier is always
 * alphanumeric; a bare all-digit hash with a leading zero (e.g. "0123456")
 * would otherwise form `0.0.0-0123456`, which is invalid semver.
 */
export function normalizeGitVersion(raw) {
  if (!raw) return null;
  const stripped = raw.replace(/^v/, "");
  // A real version begins with major.minor.patch. The bare commit hash
  // that `git describe --always` falls back to (no reachable tag) does not,
  // so coerce it to a 0.0.0 prerelease rather than passing it through.
  // Prefix the hash with `g` (mirroring `git describe`'s own `g<hash>`
  // shorthand) so a hash like "0123456" yields "0.0.0-g0123456" — a single
  // alphanumeric identifier — instead of the invalid "0.0.0-0123456".
  if (!/^\d+\.\d+\.\d+/.test(stripped)) {
    return `0.0.0-g${stripped}`;
  }
  return stripped;
}

// The exact argv handed to `git describe` for version derivation. Kept as a
// standalone array — never a shell command string — so the `v[0-9]*` match
// pattern reaches git as one literal argument regardless of platform (see the
// git() note above for why a shell string breaks on Windows).
export const DESCRIBE_ARGS = [
  "describe",
  "--tags",
  "--match",
  "v[0-9]*",
  "--always",
  "--dirty",
];

// Exported (with an optional cwd) so tests can exercise the real describe
// invocation against a throwaway repo, not just normalizeGitVersion in
// isolation — the gap that let the Windows quoting regression through CI.
export function deriveVersion(cwd) {
  return normalizeGitVersion(git(DESCRIBE_ARGS, cwd));
}

function uniqueOrdered(values) {
  return [...new Set(values)];
}

export function envWithLocalBins(env = process.env, root = desktopRoot) {
  const pathKey =
    Object.keys(env).find((key) => key.toUpperCase() === "PATH") ?? "PATH";
  const existingPath = env[pathKey] ?? "";
  const localBins = uniqueOrdered([
    resolve(root, "node_modules", ".bin"),
    resolve(root, "..", "..", "node_modules", ".bin"),
  ]);
  const mergedPath = uniqueOrdered([
    ...localBins,
    ...String(existingPath)
      .split(delimiter)
      .filter(Boolean),
  ]).join(delimiter);
  return { ...env, [pathKey]: mergedPath };
}

function hostPlatformKey(platform = process.platform) {
  if (platform === "darwin") return "mac";
  if (platform === "win32") return "win";
  if (platform === "linux") return "linux";
  throw new Error(`[package] unsupported host platform: ${platform}`);
}

function hostArchKey(arch = process.arch) {
  if (SUPPORTED_CLI_ARCHS.has(arch)) return arch;
  throw new Error(
    `[package] unsupported host architecture for Desktop CLI bundling: ${arch}`,
  );
}

function expandPlatformShorthand(token) {
  if (!/^-[mwl]{2,}$/.test(token)) return null;
  const expanded = [];
  for (const char of token.slice(1)) {
    if (char === "m") expanded.push("mac");
    if (char === "w") expanded.push("win");
    if (char === "l") expanded.push("linux");
  }
  return uniqueOrdered(expanded);
}

function platformKeyForToken(token) {
  for (const [platform, config] of Object.entries(PLATFORM_CONFIG)) {
    if (config.aliases.has(token)) return platform;
  }
  return null;
}

function platformTargetsTemplate() {
  return { mac: [], win: [], linux: [] };
}

export function parsePackageArgs(argv) {
  const sharedArgs = [];
  const platformTargets = platformTargetsTemplate();
  const requestedPlatforms = [];
  const requestedArchs = [];
  let allPlatforms = false;

  for (let i = 0; i < argv.length; i += 1) {
    const token = argv[i];
    if (token === "--all-platforms") {
      allPlatforms = true;
      continue;
    }

    const expandedPlatforms = expandPlatformShorthand(token);
    if (expandedPlatforms) {
      requestedPlatforms.push(...expandedPlatforms);
      continue;
    }

    const platform = platformKeyForToken(token);
    if (platform) {
      requestedPlatforms.push(platform);
      while (i + 1 < argv.length && !argv[i + 1].startsWith("-")) {
        platformTargets[platform].push(argv[i + 1]);
        i += 1;
      }
      continue;
    }

    const arch = ARCH_FLAGS.get(token);
    if (arch) {
      requestedArchs.push(arch);
      continue;
    }

    sharedArgs.push(token);
  }

  return {
    allPlatforms,
    sharedArgs,
    platformTargets,
    requestedPlatforms: uniqueOrdered(requestedPlatforms),
    requestedArchs: uniqueOrdered(requestedArchs),
  };
}

export function resolveBuildMatrix(parsed, platform = process.platform, arch = process.arch) {
  if (parsed.allPlatforms) {
    if (parsed.requestedPlatforms.length > 0 || parsed.requestedArchs.length > 0) {
      throw new Error(
        "[package] --all-platforms cannot be combined with explicit platform or arch flags",
      );
    }
    if (platform !== "darwin") {
      throw new Error(
        `[package] --all-platforms is only supported on macOS hosts (current: ${platform})`,
      );
    }
    return MAC_ALL_PLATFORM_TARGETS.map((target) => ({ ...target }));
  }

  const platforms =
    parsed.requestedPlatforms.length > 0
      ? parsed.requestedPlatforms
      : [hostPlatformKey(platform)];
  const archs =
    parsed.requestedArchs.length > 0
      ? parsed.requestedArchs
      : [hostArchKey(arch)];

  const unsupported = archs.filter((value) => !SUPPORTED_CLI_ARCHS.has(value));
  if (unsupported.length > 0) {
    throw new Error(
      `[package] unsupported Desktop CLI architecture(s): ${unsupported.join(", ")}. ` +
        "Use --x64 or --arm64.",
    );
  }

  return platforms.flatMap((targetPlatform) =>
    archs.map((targetArch) => ({
      platform: targetPlatform,
      arch: targetArch,
    })),
  );
}

function formatTarget(target) {
  return `${PLATFORM_CONFIG[target.platform].label} ${target.arch}`;
}

export function builderArgsForTarget(
  target,
  parsed,
  version,
  {
    disableMacNotarize = false,
    hostPlatform = process.platform,
    useScopedOutputDir = false,
  } = {},
) {
  const builderArgs = [];
  if (version) builderArgs.push(`-c.extraMetadata.version=${version}`);
  if (disableMacNotarize) builderArgs.push("-c.mac.notarize=false");
  builderArgs.push(PLATFORM_CONFIG[target.platform].builderFlag);
  const requestedTargets = parsed.platformTargets[target.platform];
  if (
    target.platform === "linux" &&
    hostPlatform !== "linux" &&
    requestedTargets.length === 0
  ) {
    // electron-builder only guarantees AppImage/Snap when cross-building
    // Linux from macOS/Windows. Keep `package:all` portable by defaulting
    // to AppImage unless the caller explicitly requests Linux targets.
    builderArgs.push("AppImage");
  } else {
    builderArgs.push(...requestedTargets);
  }
  builderArgs.push(`--${target.arch}`);
  builderArgs.push(...parsed.sharedArgs);
  if (useScopedOutputDir) {
    builderArgs.push(
      `-c.directories.output=dist/${target.platform}-${target.arch}`,
    );
  }
  // electron-builder's update metadata file is `latest.yml` for Windows
  // regardless of arch (only Linux gets an arch suffix automatically — see
  // app-builder-lib's getArchPrefixForUpdateFile). Without an explicit
  // channel override, building Windows x64 and arm64 in two invocations
  // makes both publish `latest.yml` to the same GitHub Release, so the
  // second upload overwrites the first and one of the two architectures
  // ends up with no auto-update metadata. Route Windows arm64 to its own
  // channel so x64 keeps `latest.yml` and arm64 ships `latest-arm64.yml`;
  // the renderer-side updater pins the matching channel per arch.
  if (target.platform === "win" && target.arch === "arm64") {
    builderArgs.push("-c.publish.channel=latest-arm64");
  }
  return builderArgs;
}

function main() {
  const passthrough = stripLeadingSeparator(process.argv.slice(2));
  const parsed = parsePackageArgs(passthrough);
  const buildMatrix = resolveBuildMatrix(parsed);
  console.log(
    `[package] build matrix → ${buildMatrix.map(formatTarget).join(", ")}`,
  );

  // Step 1: build the Electron main/preload/renderer bundles. Without
  // this step electron-builder silently packages whatever is already in
  // out/, which on a fresh checkout (or after a partial build) ships an
  // app that white-screens because the renderer bundle is missing.
  //
  // CI invokes this script via `node scripts/package.mjs`, so we cannot
  // rely on pnpm/npm to inject package-local binaries into PATH.
  //
  // `shell: true` is required on Windows: `node_modules/.bin/electron-vite`
  // ships as a `.cmd` shim there, and Node's `spawnSync` does not honour
  // PATHEXT when spawning a bare command without a shell — it would fail
  // with `ENOENT`. On POSIX hosts the shim is a real executable so going
  // through the shell is harmless. See
  // https://nodejs.org/api/child_process.html#spawning-bat-and-cmd-files-on-windows
  const viteResult = spawnSync("electron-vite", ["build"], {
    stdio: "inherit",
    cwd: desktopRoot,
    env: envWithLocalBins(),
    shell: true,
  });
  if (viteResult.error) {
    console.error(
      "[package] failed to spawn electron-vite:",
      viteResult.error.message,
    );
    process.exit(1);
  }
  if (viteResult.status !== 0) {
    process.exit(viteResult.status ?? 1);
  }

  // Step 2: derive the version that should be written into the app.
  const version = deriveVersion();
  if (version) {
    console.log(`[package] Desktop version → ${version} (from git describe)`);
  } else {
    console.warn(
      "[package] could not derive version from git; falling back to package.json",
    );
  }

  const disableMacNotarize = !process.env.APPLE_TEAM_ID;
  if (disableMacNotarize) {
    console.warn(
      "[package] APPLE_TEAM_ID not set — skipping notarization (local dev build). " +
        "Set APPLE_ID + APPLE_APP_SPECIFIC_PASSWORD + APPLE_TEAM_ID for a release build.",
    );
  }

  const useScopedOutputDir = buildMatrix.length > 1;

  // Step 3: for each requested target, build the matching CLI into
  // resources/bin/ and package that target in isolation.
  for (const target of buildMatrix) {
    console.log(`[package] bundling CLI → ${formatTarget(target)}`);
    execFileSync(
      "node",
      [
        bundleCliScript,
        "--target-platform",
        PLATFORM_CONFIG[target.platform].runtimePlatform,
        "--target-arch",
        target.arch,
      ],
      {
        stdio: "inherit",
        cwd: desktopRoot,
      },
    );

    const builderArgs = builderArgsForTarget(target, parsed, version, {
      disableMacNotarize,
      hostPlatform: process.platform,
      useScopedOutputDir,
    });

    // Step 4: invoke electron-builder for the current target only.
    // `shell: true` for the same Windows `.cmd` shim reason as the
    // electron-vite invocation above.
    const result = spawnSync("electron-builder", builderArgs, {
      stdio: "inherit",
      cwd: desktopRoot,
      env: envWithLocalBins(),
      shell: true,
    });

    if (result.error) {
      console.error(
        "[package] failed to spawn electron-builder:",
        result.error.message,
      );
      process.exit(1);
    }
    if (result.status !== 0) {
      process.exit(result.status ?? 1);
    }
  }
}

// Only run when invoked as a CLI, not when imported by a test file.
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  main();
}
