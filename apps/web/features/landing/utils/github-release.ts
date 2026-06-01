import {
  parseReleaseAssets,
  type DownloadAssets,
  DOWNLOAD_BASE_URL,
} from "./parse-release-assets";

/**
 * Server-side fetcher for the latest desktop release, designed to run
 * inside a Next.js server component. It reads the electron-updater
 * `latest-*.yml` manifests Lilith publishes to the OSS download proxy
 * — the same source the desktop auto-updater polls — NOT the upstream
 * GitHub releases. Upstream GitHub versions diverge from what Lilith
 * actually ships, so sourcing from there would surface installer URLs
 * that don't exist on our OSS bucket.
 *
 * Responses are cached by the Next.js fetch cache for 5 minutes, so
 * hitting /download costs at most one manifest fetch per region per
 * 5 minutes.
 *
 * Lilith ships mac-arm64, windows-x64 and linux-amd64 today, so only
 * three manifests exist. A missing manifest (404) simply drops that
 * platform; if every manifest fails (network, malformed payload) the
 * page degrades to a "version unavailable" view rather than 500ing.
 */

export interface LatestRelease {
  version: string | null;
  publishedAt: string | null;
  htmlUrl: string | null;
  assets: DownloadAssets;
}

// electron-updater manifests published to OSS, one per platform family.
const MANIFESTS = ["latest-mac.yml", "latest.yml", "latest-linux.yml"];

const REVALIDATE_SECONDS = 300;

export async function fetchLatestRelease(): Promise<LatestRelease> {
  const manifests = await Promise.all(MANIFESTS.map(fetchManifest));

  const filenames: string[] = [];
  let version: string | null = null;
  let publishedAt: string | null = null;
  for (const m of manifests) {
    if (!m) continue;
    version ??= m.version;
    publishedAt ??= m.releaseDate;
    filenames.push(...m.files);
  }

  if (filenames.length === 0) {
    return emptyRelease();
  }

  return {
    version,
    publishedAt,
    // No public release-notes page exists for Lilith builds; the UI
    // falls back to a generic link when this is null.
    htmlUrl: null,
    assets: parseReleaseAssets(filenames.map((name) => ({ name }))),
  };
}

interface Manifest {
  version: string | null;
  releaseDate: string | null;
  files: string[];
}

async function fetchManifest(name: string): Promise<Manifest | null> {
  try {
    const res = await fetch(`${DOWNLOAD_BASE_URL}/${name}`, {
      next: { revalidate: REVALIDATE_SECONDS },
    });
    // A 404 just means that platform isn't published — not an error.
    if (!res.ok) return null;
    return parseManifest(await res.text());
  } catch (err) {
    console.warn(`[download] fetch ${name} failed:`, err);
    return null;
  }
}

/**
 * Minimal scanner for electron-updater's YAML manifests. We only need
 * `version`, `releaseDate` and the `files[].url` installer names, so a
 * line scan is enough and avoids pulling in a YAML dependency.
 */
function parseManifest(text: string): Manifest {
  let version: string | null = null;
  let releaseDate: string | null = null;
  const files: string[] = [];
  for (const line of text.split("\n")) {
    const v = /^version:\s*(.+?)\s*$/.exec(line);
    if (v) {
      version = stripQuotes(v[1]);
      continue;
    }
    const d = /^releaseDate:\s*(.+?)\s*$/.exec(line);
    if (d) {
      releaseDate = stripQuotes(d[1]);
      continue;
    }
    // `url:` only appears under `files:` (the top-level primary file is
    // keyed `path:`), so this collects exactly the installer filenames.
    const f = /^\s*-?\s*url:\s*(.+?)\s*$/.exec(line);
    if (f) files.push(stripQuotes(f[1]));
  }
  return { version, releaseDate, files };
}

function stripQuotes(s: string | undefined): string {
  return (s ?? "").replace(/^['"]|['"]$/g, "");
}

function emptyRelease(): LatestRelease {
  return {
    version: null,
    publishedAt: null,
    htmlUrl: null,
    assets: {},
  };
}
