import { describe, expect, it } from "vitest";
import {
  buildCliInstallCommand,
  DEFAULT_GITHUB_BRANCH,
  DEFAULT_GITHUB_REPO,
  githubIssuesUrl,
  githubWebUrl,
} from "../constants/github";
import { resolveGithubConfig } from "./config";

describe("resolveGithubConfig", () => {
  it("uses upstream defaults when no overrides are provided", () => {
    const cfg = resolveGithubConfig();
    expect(cfg.repo).toBe(DEFAULT_GITHUB_REPO);
    expect(cfg.branch).toBe(DEFAULT_GITHUB_BRANCH);
    expect(cfg.webUrl).toBe(githubWebUrl());
    expect(cfg.issuesUrl).toBe(githubIssuesUrl());
    expect(cfg.cliInstallCommand).toBe(buildCliInstallCommand());
    expect(cfg.isUpstream).toBe(true);
    expect(cfg.docsUrl).toBe("https://multica.ai/docs");
    expect(cfg.changelogUrl).toBe("https://multica.ai/changelog");
  });

  it("builds fork install commands from overrides", () => {
    const cfg = resolveGithubConfig({
      repo: "acme/multica",
      branch: "main",
    });
    expect(cfg.cliInstallCommand).toBe(
      "MULTICA_GITHUB_REPO=acme/multica MULTICA_GITHUB_BRANCH=main curl -fsSL https://raw.githubusercontent.com/acme/multica/main/scripts/install-fork.sh | bash",
    );
    expect(cfg.webUrl).toBe("https://github.com/acme/multica");
    expect(cfg.issuesUrl).toBe("https://github.com/acme/multica/issues");
    expect(cfg.isUpstream).toBe(false);
    // Docs stay on the public Multica site unless MULTICA_DOCS_BASE_URL is set;
    // changelog points at the fork's GitHub Releases.
    expect(cfg.docsUrl).toBe("https://multica.ai/docs");
    expect(cfg.changelogUrl).toBe(
      "https://github.com/acme/multica/releases",
    );
  });

  it("honors explicit docs and changelog overrides", () => {
    const cfg = resolveGithubConfig({
      repo: "acme/multica",
      docsBaseUrl: "https://docs.example.com",
      changelogUrl: "https://docs.example.com/changelog",
    });
    expect(cfg.docsUrl).toBe("https://docs.example.com");
    expect(cfg.changelogUrl).toBe("https://docs.example.com/changelog");
  });

  it("passes repo override through fork install command", () => {
    expect(
      buildCliInstallCommand("myuser/my-fork", "develop"),
    ).toBe(
      "MULTICA_GITHUB_REPO=myuser/my-fork MULTICA_GITHUB_BRANCH=develop curl -fsSL https://raw.githubusercontent.com/myuser/my-fork/develop/scripts/install-fork.sh | bash",
    );
  });
});
