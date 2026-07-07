import {
  DEFAULT_GITHUB_BRANCH,
  DEFAULT_GITHUB_REPO,
  buildCliInstallCommand,
  buildCliInstallPs1Command,
  githubIssuesUrl,
  githubReleasesLatestApiUrl,
  githubReleasesLatestDownloadUrl,
  githubReleasesListApiUrl,
  githubReleasesUrl,
  githubWebUrl,
} from "../constants/github";
import { configStore, useConfigStore } from "../config";

export interface GithubConfig {
  repo: string;
  branch: string;
  webUrl: string;
  issuesUrl: string;
  releasesUrl: string;
  releasesLatestApiUrl: string;
  releasesListApiUrl: string;
  releasesLatestDownloadUrl: string;
  cliInstallCommand: string;
  cliInstallPs1Command: string;
}

export function resolveGithubConfig(overrides?: {
  repo?: string;
  branch?: string;
}): GithubConfig {
  const repo = overrides?.repo?.trim() || DEFAULT_GITHUB_REPO;
  const branch = overrides?.branch?.trim() || DEFAULT_GITHUB_BRANCH;
  return {
    repo,
    branch,
    webUrl: githubWebUrl(repo),
    issuesUrl: githubIssuesUrl(repo),
    releasesUrl: githubReleasesUrl(repo),
    releasesLatestApiUrl: githubReleasesLatestApiUrl(repo),
    releasesListApiUrl: githubReleasesListApiUrl(repo),
    releasesLatestDownloadUrl: githubReleasesLatestDownloadUrl(repo),
    cliInstallCommand: buildCliInstallCommand(repo, branch),
    cliInstallPs1Command: buildCliInstallPs1Command(repo, branch),
  };
}

export function githubConfigFromStore(): GithubConfig {
  const { githubRepo, githubBranch } = configStore.getState();
  return resolveGithubConfig({
    repo: githubRepo || undefined,
    branch: githubBranch || undefined,
  });
}

export function useGithubConfig(): GithubConfig {
  const repoOverride = useConfigStore((s) => s.githubRepo);
  const branchOverride = useConfigStore((s) => s.githubBranch);
  return resolveGithubConfig({
    repo: repoOverride || undefined,
    branch: branchOverride || undefined,
  });
}
