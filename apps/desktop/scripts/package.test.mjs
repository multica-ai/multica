import { describe, it, expect } from "vitest";
import { normalizeGitVersion, stripLeadingSeparator } from "./package.mjs";

describe("normalizeGitVersion", () => {
  it("returns null for empty / nullish input", () => {
    expect(normalizeGitVersion("")).toBe(null);
    expect(normalizeGitVersion(null)).toBe(null);
    expect(normalizeGitVersion(undefined)).toBe(null);
  });

  it("strips the leading v on a clean tag", () => {
    expect(normalizeGitVersion("v0.1.36")).toBe("0.1.36");
    expect(normalizeGitVersion("v1.0.0")).toBe("1.0.0");
  });

  it("preserves the prerelease suffix between tags", () => {
    expect(normalizeGitVersion("v0.1.35-14-gf1415e96")).toBe(
      "0.1.35-14-gf1415e96",
    );
  });

  it("preserves the dirty suffix on a modified worktree", () => {
    expect(normalizeGitVersion("v0.1.35-14-gf1415e96-dirty")).toBe(
      "0.1.35-14-gf1415e96-dirty",
    );
  });

  it("handles v-prefixed prerelease tags", () => {
    expect(normalizeGitVersion("v1.0.0-alpha")).toBe("1.0.0-alpha");
    expect(normalizeGitVersion("v1.0.0-rc.2")).toBe("1.0.0-rc.2");
  });

  it("falls back to 0.0.0-<hash> when no tags are reachable", () => {
    // `git describe --tags --always` returns just the short commit hash
    // when there are no tags in the history at all.
    expect(normalizeGitVersion("f1415e96")).toBe("0.0.0-f1415e96");
    expect(normalizeGitVersion("abc1234")).toBe("0.0.0-abc1234");
  });
});

describe("stripLeadingSeparator", () => {
  it("removes the leading -- inserted by npm/pnpm", () => {
    expect(stripLeadingSeparator(["--", "--mac", "--arm64", "--publish", "always"])).toEqual([
      "--mac", "--arm64", "--publish", "always",
    ]);
  });

  it("leaves args untouched when there is no leading --", () => {
    expect(stripLeadingSeparator(["--mac", "--arm64"])).toEqual(["--mac", "--arm64"]);
  });

  it("does not strip a -- that appears mid-argv", () => {
    expect(stripLeadingSeparator(["--mac", "--", "--arm64"])).toEqual([
      "--mac", "--", "--arm64",
    ]);
  });

  it("handles an empty array", () => {
    expect(stripLeadingSeparator([])).toEqual([]);
  });
});
