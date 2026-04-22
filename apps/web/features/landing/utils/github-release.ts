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
 * On any failure (network, rate limit, malformed payload) returns a
 * `null`-shaped result and logs — the page degrades to a "version
 * unavailable" view rather than 500ing.
 */

export interface LatestRelease {
  version: string | null;
  publishedAt: string | null;
  htmlUrl: string | null;
  assets: DownloadAssets;
}

const GITHUB_LATEST_URL =
  "https://api.github.com/repos/multica-ai/multica/releases/latest";

const REVALIDATE_SECONDS = 300;

interface GitHubReleasePayload {
  tag_name?: string;
  published_at?: string;
  html_url?: string;
  assets?: Array<{ name: string; browser_download_url: string }>;
}

export async function fetchLatestRelease(): Promise<LatestRelease> {
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
    const res = await fetch(GITHUB_LATEST_URL, {
      next: { revalidate: REVALIDATE_SECONDS },
      headers,
    });
    if (!res.ok) {
      throw new Error(`GitHub API responded ${res.status}`);
    }
    const data = (await res.json()) as GitHubReleasePayload;
    return {
      version: data.tag_name ?? null,
      publishedAt: data.published_at ?? null,
      htmlUrl: data.html_url ?? null,
      assets: parseReleaseAssets(data.assets ?? []),
    };
  } catch (err) {
    console.warn("[download] fetchLatestRelease failed:", err);
    return {
      version: null,
      publishedAt: null,
      htmlUrl: null,
      assets: {},
    };
  }
}
