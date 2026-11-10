import { queryOptions } from "@tanstack/react-query";

export const MULTICA_GITHUB_REPO = "multica-ai/multica";
export const MULTICA_GITHUB_URL = `https://github.com/${MULTICA_GITHUB_REPO}`;

/**
 * Star count for the Multica repository, or null when GitHub cannot be reached
 * (offline, rate-limited, an air-gapped self-hosted install). Callers fall back
 * to a label without a count. Fetched from the browser on purpose: each user
 * spends their own IP's anonymous rate limit instead of a shared server one.
 */
export async function fetchGithubStars(): Promise<number | null> {
  try {
    const res = await fetch(`https://api.github.com/repos/${MULTICA_GITHUB_REPO}`, {
      headers: { Accept: "application/vnd.github+json" },
    });
    if (!res.ok) return null;
    const data = (await res.json()) as { stargazers_count?: unknown };
    return typeof data.stargazers_count === "number" ? data.stargazers_count : null;
  } catch {
    return null;
  }
}

export function githubStarsOptions() {
  return queryOptions({
    queryKey: ["github", "stars", MULTICA_GITHUB_REPO] as const,
    queryFn: fetchGithubStars,
    staleTime: 6 * 60 * 60 * 1000,
    retry: false,
  });
}

/**
 * Compact star count matching GitHub's own repo-header style: one decimal
 * thousands/millions with the trailing ".0" trimmed ("1k", "37.6k", "1.2m").
 * Counts below 1,000 render exactly.
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
