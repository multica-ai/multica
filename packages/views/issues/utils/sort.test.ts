import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import { sortIssues } from "./sort";

const propertyId = "prop-effort";

function issueWith(id: string, value?: number | string, position = 0): Issue {
  return {
    id,
    position,
    properties: value === undefined ? {} : { [propertyId]: value },
  } as unknown as Issue;
}

function issueUpdatedAt(id: string, updatedAt: string): Issue {
  return {
    id,
    position: 0,
    updated_at: updatedAt,
    properties: {},
  } as unknown as Issue;
}

describe("sortIssues property sorts", () => {
  it("sorts number values numerically, missing values last", () => {
    const sorted = sortIssues(
      [issueWith("big", 10), issueWith("none"), issueWith("small", 2)],
      `property:${propertyId}`,
      "asc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["small", "big", "none"]);
  });

  it("desc reverses values but keeps missing values last", () => {
    const sorted = sortIssues(
      [issueWith("none"), issueWith("small", 2), issueWith("big", 10)],
      `property:${propertyId}`,
      "desc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["big", "small", "none"]);
  });

  it("sorts date-only strings chronologically via lexical compare", () => {
    const sorted = sortIssues(
      [issueWith("later", "2026-08-01"), issueWith("earlier", "2026-07-13")],
      `property:${propertyId}`,
      "asc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["earlier", "later"]);
  });

  it("falls back to position order for the static fields", () => {
    const sorted = sortIssues(
      [issueWith("b", undefined, 2), issueWith("a", undefined, 1)],
      "position",
      "asc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["a", "b"]);
  });
});

describe("sortIssues updated date sort", () => {
  it("sorts updated_at in both directions", () => {
    const issues = [
      issueUpdatedAt("older", "2026-07-15T10:00:00Z"),
      issueUpdatedAt("newer", "2026-07-16T10:00:00Z"),
    ];

    expect(sortIssues(issues, "updated_at", "asc").map((issue) => issue.id))
      .toEqual(["older", "newer"]);
    expect(sortIssues(issues, "updated_at", "desc").map((issue) => issue.id))
      .toEqual(["newer", "older"]);
  });
});
