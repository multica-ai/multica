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
  });

  it("builds fork install commands from overrides", () => {
    const cfg = resolveGithubConfig({
      repo: "Git-on-my-level/multica",
      branch: "main",
    });
    expect(cfg.cliInstallCommand).toBe(
      "curl -fsSL https://raw.githubusercontent.com/Git-on-my-level/multica/main/scripts/install.sh | bash",
    );
    expect(cfg.webUrl).toBe("https://github.com/Git-on-my-level/multica");
    expect(cfg.issuesUrl).toBe(
      "https://github.com/Git-on-my-level/multica/issues",
    );
  });
});
