"use client";

import { useEffect, useState } from "react";

/**
 * Live GitHub star count for the landing header's "GitHub" button.
 *
 * Fetched client-side on purpose: the badge lives in the shared
 * {@link LandingHeader}, which renders on every marketing page, so a single
 * client fetch covers them all without threading a server value through eight
 * render sites. Each visitor calls the GitHub API from their own IP, which
 * sidesteps the shared-outbound-IP rate limit that the server-side
 * `github-release.ts` fetcher has to work around with a PAT.
 *
 * The result is memoized at module scope (plus an in-flight promise) so
 * client-side navigation between landing pages reuses the first fetch instead
 * of hitting the API again. A failed fetch caches `null` so we don't retry in
 * a loop; the button just degrades to its plain "GitHub" label.
 */

const REPO = "multica-ai/multica";

// `undefined` = never fetched; `number` = resolved count; `null` = fetch failed.
let cachedStars: number | null | undefined;
let inFlight: Promise<number | null> | null = null;

async function loadStars(): Promise<number | null> {
  if (cachedStars !== undefined) return cachedStars;
  if (inFlight) return inFlight;

  inFlight = fetch(`https://api.github.com/repos/${REPO}`, {
    headers: { Accept: "application/vnd.github+json" },
  })
    .then((res) => {
      if (!res.ok) throw new Error(`GitHub API responded ${res.status}`);
      return res.json() as Promise<{ stargazers_count?: unknown }>;
    })
    .then((data) => {
      const count =
        typeof data.stargazers_count === "number" ? data.stargazers_count : null;
      cachedStars = count;
      return count;
    })
    .catch(() => {
      cachedStars = null;
      return null;
    })
    .finally(() => {
      inFlight = null;
    });

  return inFlight;
}

export function useGithubStars(): number | null {
  const [stars, setStars] = useState<number | null>(cachedStars ?? null);

  useEffect(() => {
    let active = true;
    void loadStars().then((count) => {
      if (active && count != null) setStars(count);
    });
    return () => {
      active = false;
    };
  }, []);

  return stars;
}

/**
 * Compact star count matching GitHub's own repo-header style: one decimal
 * thousands/millions with the trailing ".0" trimmed ("1k", "37.6k", "1.2m").
 * Counts below 1,000 render exactly. Mirrors GitHub's `toFixed(1)` rounding so
 * our badge reads the same as the figure on the repo page.
 */
export function formatStarCount(n: number): string {
  if (n >= 1_000_000) {
    return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, "")}m`;
  }
  if (n >= 1_000) {
    return `${(n / 1_000).toFixed(1).replace(/\.0$/, "")}k`;
  }
  return String(n);
}
