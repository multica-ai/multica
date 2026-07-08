import {
  DEFAULT_GITHUB_REPO,
  githubReleasesLatestDownloadUrl,
} from "@multica/core/constants/github";

/** Base URL for GitHub release asset downloads (checksums + CLI archives). */
export function githubLatestDownloadBase(): string {
  const repo = process.env.MULTICA_GITHUB_REPO?.trim() || DEFAULT_GITHUB_REPO;
  return githubReleasesLatestDownloadUrl(repo);
}
