import { describe, it, expect } from "vitest";
import type {
  Issue,
  SearchIssueResult,
  SearchProjectResult,
} from "@multica/core/types";
import { buildSearchRows } from "./search-rows";

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

/** Header titles and row keys, top to bottom — what the user actually sees. */
const shape = (rows: ReturnType<typeof buildSearchRows>) =>
  rows.map((r) => (r.kind === "header" ? `#${r.title}` : r.key));

describe("buildSearchRows", () => {
  it("renders Recent for an empty query", () => {
    const rows = buildSearchRows({
      query: "  ",
      issues: [],
      projects: [],
      recentIssues: [{ id: "r1" } as Issue, { id: "r2" } as Issue],
    });
    expect(shape(rows)).toEqual(["#Recent", "r-r1", "r-r2"]);
  });

  it("returns nothing when there is neither a query nor recent history", () => {
    expect(
      buildSearchRows({ query: "", issues: [], projects: [], recentIssues: [] }),
    ).toEqual([]);
  });

  // MUL-5824 regression: this screen renders every project before every issue,
  // so a cancelled project used to be the first row even next to a live issue.
  it("puts a cancelled project below a live issue instead of first", () => {
    const rows = buildSearchRows({
      query: "search",
      issues: [issue({ id: "i-live", title: "search live", status: "in_progress" })],
      projects: [project({ id: "p-dead", title: "search dead", status: "cancelled" })],
      recentIssues: [],
    });

    expect(shape(rows)).toEqual([
      "#Issues",
      "i-i-live",
      "#Cancelled",
      "p-p-dead",
    ]);
  });

  it("keeps live rows of both types above every cancelled row", () => {
    const rows = buildSearchRows({
      query: "search",
      issues: [
        issue({ id: "i-dead", number: 1, title: "search a", status: "cancelled" }),
        issue({ id: "i-live", number: 2, title: "search b", status: "todo" }),
        issue({ id: "i-done", number: 3, title: "search c", status: "done" }),
      ],
      projects: [
        project({ id: "p-dead", title: "search p1", status: "cancelled" }),
        project({ id: "p-live", title: "search p2", status: "planned" }),
      ],
      recentIssues: [],
    });

    expect(shape(rows)).toEqual([
      "#Projects",
      "p-p-live",
      "#Issues",
      // 'done' stays live — only cancelled work is demoted.
      "i-i-live",
      "i-i-done",
      "#Cancelled",
      "p-p-dead",
      "i-i-dead",
    ]);
  });

  it("keeps a cancelled direct hit at the top", () => {
    const rows = buildSearchRows({
      query: "MUL-7",
      issues: [issue({ id: "i-hit", number: 7, identifier: "MUL-7", status: "cancelled" })],
      projects: [],
      recentIssues: [],
    });

    expect(shape(rows)).toEqual(["#Issues", "i-i-hit"]);
  });

  it("omits the Cancelled section when nothing is demoted", () => {
    const rows = buildSearchRows({
      query: "search",
      issues: [issue({ id: "i1", status: "todo" })],
      projects: [project({ id: "p1", status: "completed" })],
      recentIssues: [],
    });
    expect(shape(rows)).toEqual(["#Projects", "p-p1", "#Issues", "i-i1"]);
  });
});
