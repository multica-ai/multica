#!/usr/bin/env node
//
// Build the `download-multica.json` manifest that's served at
// `<apiUrl>/api/downloads` (see `download-multica.jsonc` in the repo
// root for the canonical contract).
//
// Run after `pnpm --filter @multica/desktop package` has produced
// installer artifacts in `apps/desktop/dist/`. The script scans that
// directory, picks the canonical installer per `<platform>/<arch>`
// key, and writes the JSON manifest.
//
// Why automate this instead of hand-editing:
//   - The manifest must reference filenames that actually exist on
//     OSS, byte-for-byte. A typo silently breaks auto-update with no
//     server-side error (the client just sees a 404 and quietly stays
//     idle).
//   - Multi-platform CI (one job per arch) gathers artifacts back into
//     one place; this script is the single point that asserts they
//     all agree on a version and that no expected platform is missing.
//
// Env overrides (no required positional args):
//   DIST_DIR    — where to scan        (default `apps/desktop/dist`)
//   OUT_FILE    — manifest output path (default `download-multica.json`)
//   URL_PREFIX  — joined onto filename (default `/api/downloads`)
//   STRICT      — `1` to fail if any expected platform key is missing
//                 (default off; partial-release behaviour is intentional
//                 in the contract, the client treats missing keys as
//                 "no update for this arch")

import { readdirSync, statSync, writeFileSync } from "node:fs";
import { basename, join } from "node:path";

const DIST_DIR = process.env.DIST_DIR ?? "apps/desktop/dist";
const OUT_FILE = process.env.OUT_FILE ?? "download-multica.json";
const URL_PREFIX = (process.env.URL_PREFIX ?? "/api/downloads").replace(
  /\/+$/,
  "",
);
const STRICT = process.env.STRICT === "1";

// electron-builder's `artifactName` (see apps/desktop/electron-builder.yml)
// emits `multica-desktop-${version}-${platform}-${arch}.${ext}`. Some platforms
// publish multiple installers per arch (mac → dmg + zip; linux → AppImage +
// deb + rpm); we prefer the format that gives the best `shell.openPath`
// experience on the client. See PLATFORM_PRIORITY below.
const FILE_RE =
  /^multica-desktop-(v?[\d.]+(?:-[\w.-]+)?)-(mac|windows|linux)-([\w]+)\.(dmg|zip|exe|AppImage|deb|rpm)$/;

// electron-builder uses `mac` in filenames but the manifest key uses
// `darwin` (matching the client's process.platform). Windows and Linux
// names already line up.
const PLATFORM_RENAME = {
  mac: "darwin",
  windows: "windows",
  linux: "linux",
};

// Some Linux extensions ship arch tokens like `amd64` / `aarch64`; the
// manifest key uses `x64` / `arm64` (matching Node's process.arch).
const ARCH_RENAME = {
  amd64: "x64",
  aarch64: "arm64",
  x64: "x64",
  arm64: "arm64",
};

// Per-platform installer preference, in descending order. The first
// extension we see wins for a given <platform>/<arch> key; later
// matches are ignored. Rationale:
//   - mac:   `dmg` is the "drag to Applications" flow users expect.
//            `zip` is the electron-updater differential-download blob,
//            useless for the OS-native installer hand-off we do.
//   - win:   only `exe` (NSIS); nothing else to pick.
//   - linux: `AppImage` works cross-distro with no privilege escalation,
//            `deb` and `rpm` need root + a matching package manager.
const PLATFORM_PRIORITY = {
  darwin: ["dmg", "zip"],
  windows: ["exe"],
  linux: ["AppImage", "deb", "rpm"],
};

// Optional: warn if a stable release is missing any of these. STRICT=1
// turns the warning into an error. Keep this conservative — the
// contract explicitly allows a partial release. Today's Lilith build
// matrix is `mac/arm64 + win/x64` (per download-multica.jsonc), so
// those are the only entries flagged.
const EXPECTED_KEYS = ["darwin/arm64", "windows/x64"];

function walk(dir, out = []) {
  let entries;
  try {
    entries = readdirSync(dir);
  } catch (err) {
    if (err?.code === "ENOENT") return out;
    throw err;
  }
  for (const entry of entries) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, out);
    else out.push(full);
  }
  return out;
}

function priorityOf(platform, ext) {
  const order = PLATFORM_PRIORITY[platform] ?? [];
  const idx = order.indexOf(ext);
  return idx === -1 ? Number.POSITIVE_INFINITY : idx;
}

const allFiles = walk(DIST_DIR);

const desktop = {};
const desktopExt = {}; // tracks selected ext per key so we can compare priority
let manifestVersion = null;

for (const filePath of allFiles) {
  const filename = basename(filePath);
  const match = FILE_RE.exec(filename);
  if (!match) continue;

  const [, version, rawPlatform, rawArch, ext] = match;
  const platform = PLATFORM_RENAME[rawPlatform];
  const arch = ARCH_RENAME[rawArch];
  if (!platform || !arch) {
    console.warn(`[manifest] skipping ${filename}: unmapped platform/arch`);
    continue;
  }

  // First version we see locks the run. Anything else → mixed-version
  // dist, which means a failed re-package or a stale local file. Fail
  // hard rather than ship a manifest pointing at half the wrong build.
  if (manifestVersion == null) {
    manifestVersion = version;
  } else if (manifestVersion !== version) {
    throw new Error(
      `mixed versions in ${DIST_DIR}: ${manifestVersion} vs ${version} (${filename}). ` +
        `Re-package from a clean dist or delete the stale artifacts.`,
    );
  }

  const key = `${platform}/${arch}`;
  const incomingPriority = priorityOf(platform, ext);

  // Skip if we've already picked a higher-priority installer for this
  // key. Equal priority is treated as "first wins" so two artifacts with
  // the same ext don't loop. (electron-builder shouldn't produce dupes
  // but we don't rely on that.)
  if (key in desktopExt) {
    const currentPriority = priorityOf(platform, desktopExt[key]);
    if (currentPriority <= incomingPriority) continue;
  }

  desktop[key] = `${URL_PREFIX}/${filename}`;
  desktopExt[key] = ext;
}

if (manifestVersion == null) {
  throw new Error(
    `no installer artifacts found in ${DIST_DIR}. ` +
      `Did the build matrix finish? Expected files matching ${FILE_RE}.`,
  );
}

const missing = EXPECTED_KEYS.filter((key) => !(key in desktop));
if (missing.length > 0) {
  const msg = `manifest missing expected keys: ${missing.join(", ")}`;
  if (STRICT) {
    throw new Error(msg);
  }
  console.warn(`[manifest] ${msg} — proceeding without them`);
}

const manifest = { version: manifestVersion, desktop };
writeFileSync(OUT_FILE, JSON.stringify(manifest, null, 2) + "\n");

console.log(`[manifest] wrote ${OUT_FILE}`);
console.log(`[manifest] version: ${manifestVersion}`);
console.log(`[manifest] keys:    ${Object.keys(desktop).join(", ")}`);
