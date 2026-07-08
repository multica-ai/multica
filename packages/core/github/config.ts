import {
  DEFAULT_CHANGELOG_URL,
  DEFAULT_DOCS_BASE_URL,
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
  /** Help → Docs (and helper-agent docs fetch target). */
  docsUrl: string;
  /** Help → Changelog. */
  changelogUrl: string;
  isUpstream: boolean;
}

export function resolveGithubConfig(overrides?: {
  repo?: string;
  branch?: string;
  docsBaseUrl?: string;
  changelogUrl?: string;
}): GithubConfig {
  const repo = overrides?.repo?.trim() || DEFAULT_GITHUB_REPO;
  const branch = overrides?.branch?.trim() || DEFAULT_GITHUB_BRANCH;
  const isUpstream = repo === DEFAULT_GITHUB_REPO;
  const releasesUrl = githubReleasesUrl(repo);
  const docsOverride = overrides?.docsBaseUrl?.trim();
  const changelogOverride = overrides?.changelogUrl?.trim();
  return {
    repo,
    branch,
    webUrl: githubWebUrl(repo),
    issuesUrl: githubIssuesUrl(repo),
    releasesUrl,
    releasesLatestApiUrl: githubReleasesLatestApiUrl(repo),
    releasesListApiUrl: githubReleasesListApiUrl(repo),
    releasesLatestDownloadUrl: githubReleasesLatestDownloadUrl(repo),
    cliInstallCommand: buildCliInstallCommand(repo, branch),
    cliInstallPs1Command: buildCliInstallPs1Command(repo, branch),
    docsUrl: docsOverride || DEFAULT_DOCS_BASE_URL,
    changelogUrl:
      changelogOverride ||
      (isUpstream ? DEFAULT_CHANGELOG_URL : releasesUrl),
    isUpstream,
  };
}

export function githubConfigFromStore(): GithubConfig {
  const { githubRepo, githubBranch, docsBaseUrl, changelogUrl } =
    configStore.getState();
  return resolveGithubConfig({
    repo: githubRepo || undefined,
    branch: githubBranch || undefined,
    docsBaseUrl: docsBaseUrl || undefined,
    changelogUrl: changelogUrl || undefined,
  });
}

export function useGithubConfig(): GithubConfig {
  const repoOverride = useConfigStore((s) => s.githubRepo);
  const branchOverride = useConfigStore((s) => s.githubBranch);
  const docsBaseUrl = useConfigStore((s) => s.docsBaseUrl);
  const changelogUrl = useConfigStore((s) => s.changelogUrl);
  return resolveGithubConfig({
    repo: repoOverride || undefined,
    branch: branchOverride || undefined,
    docsBaseUrl: docsBaseUrl || undefined,
    changelogUrl: changelogUrl || undefined,
  });
}
