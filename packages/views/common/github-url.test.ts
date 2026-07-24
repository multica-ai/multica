import { describe, it, expect } from "vitest";
import { githubShortLabel, midTruncate } from "./github-url";

describe("githubShortLabel", () => {
  it("extracts owner/repo from an https URL", () => {
    expect(githubShortLabel("https://github.com/octocat/hello-world")).toBe(
      "octocat/hello-world",
    );
  });

  it("ignores extra path segments after owner/repo", () => {
    expect(
      githubShortLabel("https://github.com/octocat/hello-world/tree/main"),
    ).toBe("octocat/hello-world");
  });

  it("strips a trailing .git suffix", () => {
    expect(githubShortLabel("https://github.com/octocat/hello-world.git")).toBe(
      "octocat/hello-world",
    );
  });

  it("handles the www.github.com host", () => {
    expect(githubShortLabel("https://www.github.com/octocat/hello-world")).toBe(
      "octocat/hello-world",
    );
  });

  it("handles ssh:// URLs and strips .git", () => {
    expect(
      githubShortLabel("ssh://git@github.com/octocat/hello-world.git"),
    ).toBe("octocat/hello-world");
  });

  it("handles scp-style shorthand (git@github.com:owner/repo.git)", () => {
    expect(githubShortLabel("git@github.com:octocat/hello-world.git")).toBe(
      "octocat/hello-world",
    );
  });

  it("handles scp-style shorthand without a .git suffix", () => {
    expect(githubShortLabel("git@github.com:octocat/hello-world")).toBe(
      "octocat/hello-world",
    );
  });

  it("middle-truncates so a shared long prefix stays distinguishable", () => {
    const a = githubShortLabel(
      "https://github.com/example-organization/very-long-repository-name-for-customer-alpha.git",
    );
    const b = githubShortLabel(
      "https://github.com/example-organization/very-long-repository-name-for-customer-beta.git",
    );
    expect(a).not.toBe(b);
    expect(a.length).toBeLessThanOrEqual(40);
    expect(a.endsWith("alpha")).toBe(true);
    expect(b.endsWith("beta")).toBe(true);
  });

  it("returns enterprise-host URLs unchanged", () => {
    const url = "https://github.enterprise.com/octocat/hello-world";
    expect(githubShortLabel(url)).toBe(url);
  });

  it("returns malformed input unchanged", () => {
    expect(githubShortLabel("not a url")).toBe("not a url");
  });

  it("returns a github URL without a repo segment unchanged", () => {
    const url = "https://github.com/octocat";
    expect(githubShortLabel(url)).toBe(url);
  });
});

describe("midTruncate", () => {
  it("leaves short strings untouched", () => {
    expect(midTruncate("short")).toBe("short");
  });

  it("caps the result at maxLen and inserts an ellipsis", () => {
    const out = midTruncate("a".repeat(100), 21);
    expect(out.length).toBe(21);
    expect(out).toContain("…");
    expect(out.startsWith("aaaaaaaaaa")).toBe(true);
    expect(out.endsWith("aaaaaaaaaa")).toBe(true);
  });
});
