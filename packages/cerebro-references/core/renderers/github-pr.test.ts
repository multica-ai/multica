import { describe, expect, it } from "vitest";
import {
  formatGithubPrBadge,
  formatGithubPrTitle,
  parseGithubPr,
  resolveGithubPrUrl,
} from "./github-pr";
import { getObjectRenderer } from "../registry";
import type { IssueReference } from "../types";

function makeRef(overrides: Partial<IssueReference> = {}): IssueReference {
  return {
    id: "ref-1",
    issue_id: "issue-1",
    object: "github_pr",
    type: null,
    ref_id: "firtal-group/firtal-cerebro#42",
    label: null,
    url: null,
    metadata: {},
    created_by_type: "member",
    created_by_id: "user-1",
    created_at: "2026-05-19T00:00:00Z",
    updated_at: "2026-05-19T00:00:00Z",
    ...overrides,
  };
}

describe("parseGithubPr", () => {
  it("parses org/repo#NNN", () => {
    expect(parseGithubPr("firtal-group/firtal-cerebro#153")).toEqual({
      org: "firtal-group",
      repo: "firtal-cerebro",
      number: 153,
      url: "https://github.com/firtal-group/firtal-cerebro/pull/153",
    });
  });

  it("parses repo#NNN with defaultOrg", () => {
    expect(
      parseGithubPr("firtal-cerebro#42", { defaultOrg: "firtal-group" }),
    ).toEqual({
      org: "firtal-group",
      repo: "firtal-cerebro",
      number: 42,
      url: "https://github.com/firtal-group/firtal-cerebro/pull/42",
    });
  });

  it("rejects repo#NNN when no defaultOrg is provided", () => {
    expect(parseGithubPr("firtal-cerebro#42")).toBeNull();
  });

  it("parses a full GitHub pull-request URL", () => {
    expect(
      parseGithubPr("https://github.com/firtal-group/firtal-cerebro/pull/153"),
    ).toEqual({
      org: "firtal-group",
      repo: "firtal-cerebro",
      number: 153,
      url: "https://github.com/firtal-group/firtal-cerebro/pull/153",
    });
  });

  it("strips trailing path segments and query strings from a URL", () => {
    expect(
      parseGithubPr(
        "https://github.com/firtal-group/firtal-cerebro/pull/153/files?diff=unified",
      ),
    ).toEqual({
      org: "firtal-group",
      repo: "firtal-cerebro",
      number: 153,
      url: "https://github.com/firtal-group/firtal-cerebro/pull/153",
    });
  });

  it.each([
    "",
    "   ",
    "no-hash",
    "firtal-group/firtal-cerebro",
    "firtal-group/firtal-cerebro#",
    "firtal-group/firtal-cerebro#abc",
    "firtal-group/firtal-cerebro#0",
    "a/b/c#1",
    "https://gitlab.com/firtal-group/firtal-cerebro/pull/1",
    "https://github.com/firtal-group/firtal-cerebro/issues/1",
  ])("returns null for invalid input %s", (input) => {
    expect(parseGithubPr(input)).toBeNull();
  });
});

describe("formatGithubPrBadge", () => {
  it.each([
    ["open", "Open"],
    ["merged", "Merged"],
    ["closed", "Closed"],
    ["draft", "Draft"],
    ["OPEN", "Open"],
  ] as const)("maps %s to %s", (state, label) => {
    expect(formatGithubPrBadge(makeRef({ metadata: { state } }))).toBe(label);
  });

  it("passes unknown states through as-is", () => {
    expect(formatGithubPrBadge(makeRef({ metadata: { state: "Renamed" } }))).toBe(
      "Renamed",
    );
  });

  it("returns null when metadata.state is missing or not a string", () => {
    expect(formatGithubPrBadge(makeRef({ metadata: {} }))).toBeNull();
    expect(formatGithubPrBadge(makeRef({ metadata: { state: "  " } }))).toBeNull();
    expect(
      formatGithubPrBadge(makeRef({ metadata: { state: 42 as unknown as string } })),
    ).toBeNull();
  });
});

describe("formatGithubPrTitle / resolveGithubPrUrl", () => {
  it("prefers an explicit label over the parsed ref_id", () => {
    expect(formatGithubPrTitle(makeRef({ label: "Backend PR" }))).toBe("Backend PR");
  });

  it("falls back to the canonical org/repo#NNN format", () => {
    expect(formatGithubPrTitle(makeRef({ ref_id: "firtal-group/firtal-cerebro#153" })))
      .toBe("firtal-group/firtal-cerebro#153");
  });

  it("uses the stored URL when present", () => {
    expect(
      resolveGithubPrUrl(
        makeRef({ url: "https://github.com/firtal-group/firtal-cerebro/pull/9" }),
      ),
    ).toBe("https://github.com/firtal-group/firtal-cerebro/pull/9");
  });

  it("computes a URL from ref_id when stored URL is empty", () => {
    expect(resolveGithubPrUrl(makeRef({ url: null, ref_id: "a/b#3" }))).toBe(
      "https://github.com/a/b/pull/3",
    );
  });
});

// The renderer registers itself when `./github-pr` is imported. The top
// of this test file imports it, so the registration must be observable
// through the public registry API.
describe("github_pr registration", () => {
  it("is registered against the registry", () => {
    const r = getObjectRenderer("github_pr");
    expect(r.object).toBe("github_pr");
    expect(r.formatBadge(makeRef({ metadata: { state: "merged" } }))).toBe("Merged");
    expect(r.formatTitle(makeRef({ label: "x" }))).toBe("x");
    expect(r.resolveUrl(makeRef({ ref_id: "a/b#7" }))).toBe(
      "https://github.com/a/b/pull/7",
    );
  });
});
