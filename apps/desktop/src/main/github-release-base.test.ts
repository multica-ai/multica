import { afterEach, describe, expect, it } from "vitest";

import {
  githubLatestDownloadBase,
  githubRepoFromAppUpdateYaml,
  githubReleasesLatestPageUrl,
  resolveGithubRepo,
} from "./github-release-base";

describe("githubRepoFromAppUpdateYaml", () => {
  it("parses owner and repo from app-update.yml", () => {
    expect(
      githubRepoFromAppUpdateYaml("owner: Git-on-my-level\nrepo: multica\nprovider: github\n"),
    ).toBe("Git-on-my-level/multica");
  });

  it("returns null when owner or repo is missing", () => {
    expect(githubRepoFromAppUpdateYaml("provider: github\n")).toBeNull();
  });
});

describe("resolveGithubRepo", () => {
  afterEach(() => {
    delete process.env.MULTICA_GITHUB_REPO;
  });

  it("prefers MULTICA_GITHUB_REPO when set", () => {
    process.env.MULTICA_GITHUB_REPO = "Git-on-my-level/multica";
    expect(resolveGithubRepo()).toBe("Git-on-my-level/multica");
  });

  it("falls back to the upstream default when env is unset", () => {
    expect(resolveGithubRepo()).toBe("multica-ai/multica");
  });
});

describe("githubLatestDownloadBase", () => {
  afterEach(() => {
    delete process.env.MULTICA_GITHUB_REPO;
  });

  it("uses the default repo when MULTICA_GITHUB_REPO is unset", () => {
    expect(githubLatestDownloadBase()).toBe(
      "https://github.com/multica-ai/multica/releases/latest/download",
    );
  });

  it("honors MULTICA_GITHUB_REPO for fork/self-host overrides", () => {
    process.env.MULTICA_GITHUB_REPO = "Git-on-my-level/multica";
    expect(githubLatestDownloadBase()).toBe(
      "https://github.com/Git-on-my-level/multica/releases/latest/download",
    );
  });
});

describe("githubReleasesLatestPageUrl", () => {
  afterEach(() => {
    delete process.env.MULTICA_GITHUB_REPO;
  });

  it("points at the latest release page for the resolved repo", () => {
    process.env.MULTICA_GITHUB_REPO = "Git-on-my-level/multica";
    expect(githubReleasesLatestPageUrl()).toBe(
      "https://github.com/Git-on-my-level/multica/releases/latest",
    );
  });
});
