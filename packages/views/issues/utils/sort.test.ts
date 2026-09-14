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

function staticIssue(id: string, overrides: Partial<Issue> = {}): Issue {
  return {
    id,
    title: id,
    status: "todo",
    priority: "none",
    position: 0,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  } as Issue;
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

  it("keeps manual position ascending even with a stale desc direction", () => {
    const sorted = sortIssues(
      [issueWith("b", undefined, 2), issueWith("a", undefined, 1)],
      "position",
      "desc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["a", "b"]);
  });

  it("sorts updated timestamps newest first", () => {
    const sorted = sortIssues(
      [
        staticIssue("older", { updated_at: "2026-01-01T00:00:00Z" }),
        staticIssue("newer", { updated_at: "2026-02-01T00:00:00Z" }),
      ],
      "updated_at",
      "desc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["newer", "older"]);
  });

  it("sorts custom statuses by their effective category", () => {
    const sorted = sortIssues(
      [
        staticIssue("done", { status: "done" }),
        staticIssue("blocked", {
          status: "waiting_on_vendor",
          status_category: "blocked",
        }),
      ],
      "status",
      "asc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["blocked", "done"]);
  });

  it("keeps missing dates last in descending order", () => {
    const sorted = sortIssues(
      [
        staticIssue("missing", { due_date: null }),
        staticIssue("earlier", { due_date: "2026-01-01" }),
        staticIssue("later", { due_date: "2026-02-01" }),
      ],
      "due_date",
      "desc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["later", "earlier", "missing"]);
  });
});
