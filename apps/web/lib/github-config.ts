import {
  DEFAULT_GITHUB_BRANCH,
  DEFAULT_GITHUB_REPO,
  githubWebUrl,
} from "@multica/core/constants/github";
import { resolveGithubConfig, type GithubConfig } from "@multica/core/github/config";

function envGithubRepo(): string | undefined {
  const repo = process.env.MULTICA_GITHUB_REPO?.trim();
  return repo || undefined;
}

function envGithubBranch(): string | undefined {
  const branch = process.env.MULTICA_GITHUB_BRANCH?.trim();
  return branch || undefined;
}

/** Server-side GitHub config for landing SSR (reads MULTICA_GITHUB_REPO). */
export function getServerGithubConfig(): GithubConfig {
  return resolveGithubConfig({
    repo: envGithubRepo(),
    branch: envGithubBranch(),
  });
}

export const defaultGithubWebUrl = githubWebUrl(DEFAULT_GITHUB_REPO);
export const defaultGithubBranch = DEFAULT_GITHUB_BRANCH;
export const defaultGithubRepo = DEFAULT_GITHUB_REPO;
