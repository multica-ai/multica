import {
  parseReleaseAssets,
  type DownloadAssets,
  type GitHubAsset,
} from "./parse-release-assets";

/**
 * Lilith fork: instead of fetching the upstream GitHub Releases API,
 * read the electron-updater manifests (`latest-mac.yml`, `latest.yml`,
 * `latest-linux.yml`) the desktop release pipeline already writes to
 * our private OSS bucket, served via `/api/downloads/<file>`.
 *
 * The button hrefs come back as RELATIVE paths under `/api/downloads/`,
 * so the same code works for multica-test.lilithgames.com and
 * multica.lilithgames.com without env-specific wiring.
 *
 * On any failure (network, missing manifest, parse error) the page
 * degrades to a "version unavailable" view rather than 500ing.
 */

export interface LatestRelease {
  version: string | null;
  publishedAt: string | null;
  htmlUrl: string | null;
  assets: DownloadAssets;
}

const MANIFESTS = ["latest-mac.yml", "latest.yml", "latest-linux.yml"] as const;
const REVALIDATE_SECONDS = 300;

export async function fetchLatestRelease(): Promise<LatestRelease> {
  const base = downloadsBaseURL();
  try {
    const responses = await Promise.all(
      MANIFESTS.map(async (name) => {
        const res = await fetch(`${base}/api/downloads/${name}`, {
          next: { revalidate: REVALIDATE_SECONDS },
          headers: { Accept: "text/yaml" },
        });
        // Missing manifest = platform skipped on this release; the
        // matrix UI greys out unavailable formats automatically.
        if (!res.ok) return null;
        return res.text();
      }),
    );

    const filenames = new Set<string>();
    let version: string | null = null;
    for (const text of responses) {
      if (!text) continue;
      const manifest = parseManifest(text);
      if (manifest.version && !version) version = manifest.version;
      for (const f of manifest.files) filenames.add(f);
    }

    if (!version && filenames.size === 0) {
      return emptyRelease();
    }

    // Reuse parseReleaseAssets's filename → platform/arch/format
    // classifier by wrapping each filename in a GitHubAsset-shaped
    // object whose `browser_download_url` is our local proxy path.
    const fakeAssets: GitHubAsset[] = Array.from(filenames).map((name) => ({
      name,
      browser_download_url: `/api/downloads/${name}`,
    }));

    return {
      version: version ? prefixVersion(version) : null,
      publishedAt: null,
      htmlUrl: "/changelog",
      assets: parseReleaseAssets(fakeAssets),
    };
  } catch (err) {
    console.warn("[download] fetchLatestRelease failed:", err);
    return emptyRelease();
  }
}

function downloadsBaseURL(): string {
  // In the Next.js container REMOTE_API_URL points at the in-cluster
  // backend service (e.g. http://multica-server:8080). Local dev
  // (`pnpm dev:web`) falls through to the local backend.
  const base = process.env.REMOTE_API_URL || "http://localhost:8080";
  return base.replace(/\/$/, "");
}

// Minimal electron-updater YML parser. The files have a fixed shape:
//   version: 0.2.40
//   files:
//     - url: <filename>
//       sha512: ...
//       size: 123
//   path: <primary>
//   releaseDate: '...'
// Only `version:` at the top level and `url:` inside `files:` matter
// for our purposes; everything else (sha512, size, blockMapSize,
// releaseDate, path) is electron-updater plumbing the download page
// has no use for. Hand-rolled to avoid pulling in a YAML dependency.
interface ParsedManifest {
  version: string | null;
  files: string[];
}

function parseManifest(text: string): ParsedManifest {
  const out: ParsedManifest = { version: null, files: [] };
  for (const rawLine of text.split(/\r?\n/)) {
    const versionMatch = /^version:\s*(\S+)/.exec(rawLine);
    if (versionMatch && versionMatch[1]) {
      out.version = stripQuotes(versionMatch[1]);
      continue;
    }
    // Match `  - url: foo.zip` (the leading dash version) and
    // `    url: foo.zip` (continuation lines inside a file entry).
    const urlMatch = /^\s*(?:-\s*)?url:\s*(\S+)/.exec(rawLine);
    if (urlMatch && urlMatch[1]) {
      out.files.push(stripQuotes(urlMatch[1]));
    }
  }
  return out;
}

function stripQuotes(s: string): string {
  if (
    (s.startsWith("'") && s.endsWith("'")) ||
    (s.startsWith('"') && s.endsWith('"'))
  ) {
    return s.slice(1, -1);
  }
  return s;
}

function prefixVersion(v: string): string {
  return v.startsWith("v") ? v : `v${v}`;
}

function emptyRelease(): LatestRelease {
  return {
    version: null,
    publishedAt: null,
    htmlUrl: null,
    assets: {},
  };
}
