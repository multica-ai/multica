import { describe, it, expect } from "vitest";
import type { SearchIssueResult, SearchProjectResult } from "../types/api";
import {
  isIssueDirectHit,
  isProjectDirectHit,
  parseSearchQueryNumber,
  partitionAggregatedSearchResults,
  partitionStable,
} from "./cancelled-rank";

function issue(
  partial: Partial<SearchIssueResult> & { id: string },
): SearchIssueResult {
  return {
    id: partial.id,
    number: partial.number ?? 1,
    identifier: partial.identifier ?? `MUL-${partial.number ?? 1}`,
    title: partial.title ?? "Untitled",
    status: partial.status ?? "todo",
    match_source: partial.match_source ?? "title",
  } as SearchIssueResult;
}

function project(
  partial: Partial<SearchProjectResult> & { id: string },
): SearchProjectResult {
  return {
    id: partial.id,
    title: partial.title ?? "Untitled",
    status: partial.status ?? "in_progress",
    match_source: partial.match_source ?? "title",
  } as SearchProjectResult;
}

describe("parseSearchQueryNumber", () => {
  it("reads an identifier and a bare number, like the server does", () => {
    expect(parseSearchQueryNumber("MUL-123")).toBe(123);
    expect(parseSearchQueryNumber("mul-123")).toBe(123);
    expect(parseSearchQueryNumber("  123 ")).toBe(123);
  });

  it("returns null for anything that is not a target", () => {
    expect(parseSearchQueryNumber("search")).toBeNull();
    expect(parseSearchQueryNumber("MUL-")).toBeNull();
    expect(parseSearchQueryNumber("0")).toBeNull();
    expect(parseSearchQueryNumber("")).toBeNull();
  });
});

describe("direct hits", () => {
  it("treats an exact identifier, number, or title as targeting the issue", () => {
    const it1 = issue({ id: "i1", number: 42, identifier: "MUL-42", title: "Ship it" });
    expect(isIssueDirectHit(it1, "MUL-42")).toBe(true);
    expect(isIssueDirectHit(it1, "42")).toBe(true);
    expect(isIssueDirectHit(it1, "  ship IT ")).toBe(true);
    expect(isIssueDirectHit(it1, "ship")).toBe(false);
  });

  it("treats only an exact title as targeting the project", () => {
    const p = project({ id: "p1", title: "Search revamp" });
    expect(isProjectDirectHit(p, "search revamp")).toBe(true);
    expect(isProjectDirectHit(p, "search")).toBe(false);
    expect(isProjectDirectHit(p, "")).toBe(false);
  });

  // The @mention picker holds the identifier and title but no `number`, so the
  // number has to be derived from the identifier for it to share this rule.
  it("derives the issue number from the identifier when number is absent", () => {
    const row = { identifier: "MUL-77", title: "Abandoned plan" };
    expect(isIssueDirectHit(row, "77")).toBe(true);
    expect(isIssueDirectHit(row, "MUL-77")).toBe(true);
    expect(isIssueDirectHit(row, "mul-77")).toBe(true);
    expect(isIssueDirectHit(row, "Abandoned plan")).toBe(true);
    expect(isIssueDirectHit(row, "plan")).toBe(false);
    expect(isIssueDirectHit(row, "78")).toBe(false);
  });

  it("prefers an explicit number over the identifier", () => {
    // Defensive: if the two ever disagree, `number` is the authoritative field.
    const row = { number: 42, identifier: "MUL-77", title: "t" };
    expect(isIssueDirectHit(row, "42")).toBe(true);
    expect(isIssueDirectHit(row, "77")).toBe(false);
  });

  it("is not a direct hit when nothing identifies the row", () => {
    expect(isIssueDirectHit({}, "MUL-1")).toBe(false);
    expect(isProjectDirectHit({}, "anything")).toBe(false);
  });
});

describe("partitionStable", () => {
  it("preserves relative order on both sides", () => {
    const { live, cancelled } = partitionStable(
      [1, 2, 3, 4, 5, 6],
      (n) => n % 2 === 0,
    );
    expect(live).toEqual([1, 3, 5]);
    expect(cancelled).toEqual([2, 4, 6]);
  });
});

describe("partitionAggregatedSearchResults", () => {
  it("keeps a cancelled project out of the leading slot a live issue deserves", () => {
    // The regression Sol-Boy flagged: the palette renders every project before
    // every issue, so one cancelled project used to be the first row overall.
    const parts = partitionAggregatedSearchResults({
      issues: [issue({ id: "i-live", number: 2, title: "search live", status: "in_progress" })],
      projects: [project({ id: "p-cancelled", title: "search cancelled", status: "cancelled" })],
      query: "search",
    });

    expect(parts.liveProjects).toEqual([]);
    expect(parts.liveIssues.map((i) => i.id)).toEqual(["i-live"]);
    expect(parts.cancelledProjects.map((p) => p.id)).toEqual(["p-cancelled"]);
    expect(parts.hasCancelled).toBe(true);
  });

  it("demotes cancelled rows of both types while keeping server order within each side", () => {
    const parts = partitionAggregatedSearchResults({
      issues: [
        issue({ id: "i1", number: 1, title: "search a", status: "cancelled" }),
        issue({ id: "i2", number: 2, title: "search b", status: "todo" }),
        issue({ id: "i3", number: 3, title: "search c", status: "cancelled" }),
        issue({ id: "i4", number: 4, title: "search d", status: "done" }),
      ],
      projects: [
        project({ id: "p1", title: "search p1", status: "cancelled" }),
        project({ id: "p2", title: "search p2", status: "planned" }),
      ],
      query: "search",
    });

    expect(parts.liveProjects.map((p) => p.id)).toEqual(["p2"]);
    // 'done' is live: finished work is still worth referencing.
    expect(parts.liveIssues.map((i) => i.id)).toEqual(["i2", "i4"]);
    expect(parts.cancelledProjects.map((p) => p.id)).toEqual(["p1"]);
    expect(parts.cancelledIssues.map((i) => i.id)).toEqual(["i1", "i3"]);
  });

  it("exempts direct hits of either type", () => {
    const parts = partitionAggregatedSearchResults({
      issues: [issue({ id: "i-hit", number: 7, identifier: "MUL-7", status: "cancelled" })],
      projects: [project({ id: "p-other", title: "MUL-7 cleanup", status: "cancelled" })],
      query: "MUL-7",
    });

    expect(parts.liveIssues.map((i) => i.id)).toEqual(["i-hit"]);
    expect(parts.cancelledProjects.map((p) => p.id)).toEqual(["p-other"]);
  });

  it("reports no cancelled rows when nothing is demoted", () => {
    const parts = partitionAggregatedSearchResults({
      issues: [issue({ id: "i1", status: "todo" })],
      projects: [project({ id: "p1", status: "completed" })],
      query: "x",
    });
    expect(parts.hasCancelled).toBe(false);
  });
});
