import { afterEach, describe, expect, it } from "vitest";

import { githubLatestDownloadBase } from "./github-release-base";

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
