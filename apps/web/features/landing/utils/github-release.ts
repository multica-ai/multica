import {
  parseReleaseAssets,
  type DownloadAssets,
} from "./parse-release-assets";

/**
 * Server-side fetcher for the latest Multica release, designed to
 * run inside a Next.js server component. Response is cached by the
 * Next.js fetch cache for 5 minutes (Vercel ISR) so hitting /download
 * costs at most one GitHub API call per region per 5 minutes.
 *
 * Desktop assets don't all land at the same time: CI uploads Linux
 * and Windows within a minute of each other, but macOS is packaged
 * manually (notarization credentials aren't wired into CI yet) and
 * lands tens of minutes later. To avoid showing the half-filled
 * mid-flight state on /download, the fetcher pulls the two most
 * recent releases and falls back to the previous one for the first
 * hour after publish. Empirically full desktop uploads complete in
 * ~20 min; 1 h gives 3x buffer for commonly-variable manual steps.
 *
 * On any failure (network, rate limit, malformed payload) returns a
 * `null`-shaped result and logs — the page degrades to a "version
 * unavailable" view rather than 500ing.
 */

export interface LatestRelease {
  version: string | null;
  publishedAt: string | null;
  htmlUrl: string | null;
  allReleasesUrl: string;
  assets: DownloadAssets;
}

const DEFAULT_RELEASE_REPOSITORY = "kanfashidoufu/multica";

const REVALIDATE_SECONDS = 300;

const FRESH_RELEASE_WINDOW_MS = 60 * 60 * 1000;

interface GitHubReleasePayload {
  tag_name?: string;
  published_at?: string;
  html_url?: string;
  prerelease?: boolean;
  draft?: boolean;
  assets?: Array<{ name: string; browser_download_url: string }>;
}

export async function fetchLatestRelease(): Promise<LatestRelease> {
  const repository = resolveReleaseRepository();
  const releasesApiUrl = `https://api.github.com/repos/${repository}/releases?per_page=2`;
  const allReleasesUrl = `https://github.com/${repository}/releases`;
  const headers: Record<string, string> = {
    Accept: "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28",
  };
  // Optional PAT for local development and self-hosted deploys where
  // the shared outbound IP keeps hitting the 60-requests/hour
  // unauthenticated limit. Vercel's fetch cache is shared across all
  // regions so production rarely needs this — but the env var lets
  // anyone running the site locally avoid the rate-limit dance. Never
  // prefix this with `NEXT_PUBLIC_`; the token must stay server-side.
  const token = process.env.GITHUB_TOKEN;
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  try {
    const res = await fetch(releasesApiUrl, {
      next: { revalidate: REVALIDATE_SECONDS },
      headers,
    });
    if (!res.ok) {
      throw new Error(`GitHub API responded ${res.status}`);
    }
    const data = (await res.json()) as GitHubReleasePayload[];

    // Defensive filter — Multica doesn't publish prereleases or drafts
    // today, but the endpoint returns them if that ever changes. A
    // prerelease shadowing a stable version on /download would be a
    // regression.
    const stable = data.filter((r) => !r.prerelease && !r.draft);
    const latest = stable[0];
    if (!latest) {
      return emptyRelease();
    }
    const previous = stable[1];
    const chosen =
      previous && isWithinFreshWindow(latest) ? previous : latest;

    return {
      version: chosen.tag_name ?? null,
      publishedAt: chosen.published_at ?? null,
      htmlUrl: chosen.html_url ?? null,
      allReleasesUrl,
      assets: parseReleaseAssets(chosen.assets ?? []),
    };
  } catch (err) {
    console.warn("[download] fetchLatestRelease failed:", err);
    return emptyRelease(allReleasesUrl);
  }
}

export function resolveReleaseRepository(): string {
  const raw =
    process.env.MULTICA_RELEASE_REPOSITORY ||
    process.env.GITHUB_REPOSITORY ||
    DEFAULT_RELEASE_REPOSITORY;
  const normalized = raw.trim().replace(/^https:\/\/github\.com\//, "");
  return /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(normalized)
    ? normalized
    : DEFAULT_RELEASE_REPOSITORY;
}

function isWithinFreshWindow(release: GitHubReleasePayload): boolean {
  if (!release.published_at) return false;
  const publishedAt = Date.parse(release.published_at);
  if (Number.isNaN(publishedAt)) return false;
  return Date.now() - publishedAt < FRESH_RELEASE_WINDOW_MS;
}

function emptyRelease(
  allReleasesUrl = `https://github.com/${resolveReleaseRepository()}/releases`,
): LatestRelease {
  return {
    version: null,
    publishedAt: null,
    htmlUrl: null,
    allReleasesUrl,
    assets: {},
  };
}
