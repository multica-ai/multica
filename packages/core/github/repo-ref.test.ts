// @vitest-environment node
import { describe, it, expect } from "vitest";
import {
  GIT_REF_MAX_LENGTH,
  looksLikeCommitSha,
  splitGithubUrlRef,
  validateGitRef,
} from "./repo-ref";

describe("validateGitRef", () => {
  it("accepts an empty ref as 'use the default branch'", () => {
    expect(validateGitRef("")).toEqual({ ok: true });
    expect(validateGitRef("   ")).toEqual({ ok: true });
  });

  it.each([
    ["a plain branch", "main"],
    ["a slashed branch", "release/2026-09"],
    ["a deeply slashed branch", "team/alice/feat/new-thing"],
    ["a semver tag", "v1.2.3"],
    ["a short SHA", "a1b2c3d"],
    ["a full SHA", "5e0b1cfa0a7d6a1a0f4b3f2e1d0c9b8a7f6e5d4c"],
    ["a branch with dots inside", "release.2026.09"],
    ["a branch with a hyphenated tail", "fix/MUL-7504-pin-ref"],
  ])("accepts %s", (_label, ref) => {
    expect(validateGitRef(ref)).toEqual({ ok: true });
  });

  it.each([
    ["a space", "my branch"],
    ["a tilde", "main~1"],
    ["a caret", "main^"],
    ["a colon", "origin:main"],
    ["a question mark", "main?"],
    ["an asterisk", "refs/heads/*"],
    ["a backslash", "feat\\thing"],
    ["an interior control character", "ma\nin"],
  ])("rejects %s", (_label, ref) => {
    expect(validateGitRef(ref)).toEqual({ ok: false, reason: "invalid_characters" });
  });

  it.each([
    ["a range", "main..dev"],
    ["reflog syntax", "main@{1}"],
    ["a lone at-sign", "@"],
    ["a leading slash", "/main"],
    ["a trailing slash", "main/"],
    ["a doubled slash", "feat//thing"],
    ["a leading dot", ".hidden"],
    ["a trailing dot", "main."],
    ["a lock suffix", "main.lock"],
    ["a lock suffix on an inner segment", "feat.lock/thing"],
    ["a dot-prefixed inner segment", "feat/.hidden"],
  ])("rejects %s", (_label, ref) => {
    expect(validateGitRef(ref)).toEqual({ ok: false, reason: "invalid_format" });
  });

  it("rejects a ref past the length cap but accepts one exactly at it", () => {
    expect(validateGitRef("a".repeat(GIT_REF_MAX_LENGTH))).toEqual({ ok: true });
    expect(validateGitRef("a".repeat(GIT_REF_MAX_LENGTH + 1))).toEqual({
      ok: false,
      reason: "too_long",
    });
  });
});

describe("splitGithubUrlRef", () => {
  it("splits a browse URL into clone URL and ref", () => {
    expect(splitGithubUrlRef("https://github.com/multica-ai/multica/tree/main")).toEqual({
      url: "https://github.com/multica-ai/multica",
      ref: "main",
    });
  });

  it("keeps a multi-segment branch together", () => {
    expect(
      splitGithubUrlRef("https://github.com/multica-ai/multica/tree/release/2026-09"),
    ).toEqual({
      url: "https://github.com/multica-ai/multica",
      ref: "release/2026-09",
    });
  });

  it("drops a .git suffix before /tree and a trailing slash after the ref", () => {
    expect(
      splitGithubUrlRef("https://github.com/multica-ai/multica.git/tree/main/"),
    ).toEqual({ url: "https://github.com/multica-ai/multica", ref: "main" });
  });

  it("leaves a plain clone URL untouched", () => {
    for (const url of [
      "https://github.com/multica-ai/multica",
      "https://github.com/multica-ai/multica.git",
      "git@github.com:multica-ai/multica.git",
      "https://gitlab.com/owner/repo/tree/main",
    ]) {
      expect(splitGithubUrlRef(url)).toEqual({ url });
    }
  });

  it("leaves /blob and /pull URLs alone — neither names a checkout baseline", () => {
    const blob = "https://github.com/multica-ai/multica/blob/main/README.md";
    const pull = "https://github.com/multica-ai/multica/pull/8572";
    expect(splitGithubUrlRef(blob)).toEqual({ url: blob });
    expect(splitGithubUrlRef(pull)).toEqual({ url: pull });
  });

  it("does not split when the extracted ref would be invalid", () => {
    const url = "https://github.com/multica-ai/multica/tree/main..dev";
    expect(splitGithubUrlRef(url)).toEqual({ url });
  });

  it("trims surrounding whitespace from a pasted URL", () => {
    expect(splitGithubUrlRef("  https://github.com/o/r/tree/dev  ")).toEqual({
      url: "https://github.com/o/r",
      ref: "dev",
    });
  });
});

describe("looksLikeCommitSha", () => {
  it("recognises full-length object ids", () => {
    expect(looksLikeCommitSha("5e0b1cfa0a7d6a1a0f4b3f2e1d0c9b8a7f6e5d4c")).toBe(true);
    expect(looksLikeCommitSha("5E0B1CFA0A7D6A1A0F4B3F2E1D0C9B8A7F6E5D4C")).toBe(true);
    expect(looksLikeCommitSha("a".repeat(64))).toBe(true);
    expect(looksLikeCommitSha("  " + "b".repeat(40) + "  ")).toBe(true);
  });

  it("leaves anything that could plausibly be a branch alone", () => {
    for (const value of [
      "main",
      "release/2026-09",
      "v1.2.3",
      "a1b2c3d", // a short SHA is also a legal branch name — not our call to make
      "deadbeef",
      "a".repeat(39),
      "a".repeat(41),
      "g".repeat(40), // not hex
      "",
    ]) {
      expect(looksLikeCommitSha(value)).toBe(false);
    }
  });

  it("is independent of validateGitRef — a SHA is still a valid ref to store", () => {
    const sha = "5e0b1cfa0a7d6a1a0f4b3f2e1d0c9b8a7f6e5d4c";
    expect(validateGitRef(sha)).toEqual({ ok: true });
    expect(looksLikeCommitSha(sha)).toBe(true);
  });
});
