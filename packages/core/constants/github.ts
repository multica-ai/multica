export const DEFAULT_GITHUB_REPO = "multica-ai/multica";
export const DEFAULT_GITHUB_BRANCH = "main";
export const DEFAULT_DOCS_BASE_URL = "https://multica.ai/docs";
export const DEFAULT_CHANGELOG_URL = "https://multica.ai/changelog";

const INSTALL_SCRIPT = "install.sh";
const FORK_INSTALL_SCRIPT = "install-fork.sh";
const INSTALL_PS1_SCRIPT = "install.ps1";
const FORK_INSTALL_PS1_SCRIPT = "install-fork.ps1";

export function githubWebUrl(repo = DEFAULT_GITHUB_REPO): string {
  return `https://github.com/${repo}`;
}

export function githubIssuesUrl(repo = DEFAULT_GITHUB_REPO): string {
  return `${githubWebUrl(repo)}/issues`;
}

export function githubReleasesUrl(repo = DEFAULT_GITHUB_REPO): string {
  return `${githubWebUrl(repo)}/releases`;
}

export function githubReleasesApiUrl(repo = DEFAULT_GITHUB_REPO): string {
  return `https://api.github.com/repos/${repo}/releases`;
}

export function githubReleasesLatestApiUrl(repo = DEFAULT_GITHUB_REPO): string {
  return `${githubReleasesApiUrl(repo)}/latest`;
}

export function githubReleasesListApiUrl(
  repo = DEFAULT_GITHUB_REPO,
  perPage = 2,
): string {
  return `${githubReleasesApiUrl(repo)}?per_page=${perPage}`;
}

export function githubReleasesLatestDownloadUrl(
  repo = DEFAULT_GITHUB_REPO,
): string {
  return `${githubReleasesUrl(repo)}/latest/download`;
}

export function buildCliInstallCommand(
  repo = DEFAULT_GITHUB_REPO,
  branch = DEFAULT_GITHUB_BRANCH,
): string {
  const script =
    repo === DEFAULT_GITHUB_REPO ? INSTALL_SCRIPT : FORK_INSTALL_SCRIPT;
  const url = `https://raw.githubusercontent.com/${repo}/${branch}/scripts/${script}`;
  if (repo === DEFAULT_GITHUB_REPO) {
    return `curl -fsSL ${url} | bash`;
  }
  return `MULTICA_GITHUB_REPO=${repo} MULTICA_GITHUB_BRANCH=${branch} curl -fsSL ${url} | bash`;
}

export function buildCliInstallPs1Command(
  repo = DEFAULT_GITHUB_REPO,
  branch = DEFAULT_GITHUB_BRANCH,
): string {
  const script =
    repo === DEFAULT_GITHUB_REPO ? INSTALL_PS1_SCRIPT : FORK_INSTALL_PS1_SCRIPT;
  const url = `https://raw.githubusercontent.com/${repo}/${branch}/scripts/${script}`;
  if (repo === DEFAULT_GITHUB_REPO) {
    return `irm ${url} | iex`;
  }
  return `$env:MULTICA_GITHUB_REPO='${repo}'; $env:MULTICA_GITHUB_BRANCH='${branch}'; irm ${url} | iex`;
}
