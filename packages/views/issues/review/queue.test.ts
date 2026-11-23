import { describe, expect, it } from "vitest";
import type { Issue, IssueTableQuerySpec, IssueTableRowsResponse } from "@multica/core/types";
import { nextReviewIssue, readReviewQueue, ReviewQueueChanged } from "./queue";

const query: IssueTableQuerySpec = {
  scope: { kind: "workspace" },
  filters: { statuses: ["in_review"], priorities: ["low"], negate: ["priorities"], include_sub_issues: true },
  sort: { field: "title", direction: "desc" },
};
const issue = (id: string) => ({ id, status: "in_review" }) as Issue;
const page = (ids: string[], cursor: string | null): IssueTableRowsResponse => ({
  query_fingerprint: "query", group_key: "status:in_review", parent_id: null,
  total: 0, branch_total: ids.length, rows: ids.map((id) => ({ issue: issue(id), direct_child_count: 0 })), next_cursor: cursor,
});
const counts = (n: number) => async () => ({
  query_fingerprint: "query", total: n,
  groups: [{ key: "status:in_review", value: { kind: "status" as const, status: "in_review" }, count: n }], next_cursor: null,
});

describe("review pagination integrity (row branch_total is page size)", () => {
  it("preserves the whole query and traverses all cursors, including an uneven last page", async () => {
    const received: unknown[] = [];
    const pages = [page(["a", "b"], "tail"), page(["c"], null)];
    const rows = await readReviewQueue(query, async (request) => {
      received.push(request);
      expect(request.query).toBe(query);
      return pages.shift()!;
    }, counts(3));
    expect(rows.map((row) => row.id)).toEqual(["a", "b", "c"]);
    expect(received).toMatchObject([
      { group_key: "status:in_review", page: { cursor: null } },
      { group_key: "status:in_review", page: { cursor: "tail" } },
    ]);
  });
  it.each(["fallback", "fingerprint", "duplicate", "loop", "truncated", "wrong-status"])("rejects %s instead of claiming the queue ended", async (kind) => {
    let calls = 0;
    await expect(readReviewQueue(query, async () => {
      calls++;
      if (kind === "fallback") return { ...page([], null), query_fingerprint: "" };
      if (kind === "fingerprint") return { ...page(["a"], null), query_fingerprint: "other" };
      if (kind === "duplicate") return page(["a", "a"], null);
      if (kind === "loop") return page([String(calls)], "repeat");
      if (kind === "wrong-status") return { ...page(["a"], null), rows: [{ issue: { ...issue("a"), status: "done" }, direct_child_count: 0 }] };
      return page(["a"], null);
    }, counts(2))).rejects.toBeInstanceOf(ReviewQueueChanged);
  });
  it("rejects a membership-count change during traversal", async () => {
    let reads = 0;
    await expect(readReviewQueue(query, async () => page(["a"], null), async () => counts(++reads)()))
      .rejects.toBeInstanceOf(ReviewQueueChanged);
  });
  it("revalidates membership while preserving the write-time order and wrapping", () => {
    const before = ["a", "b", "c", "d"].map(issue);
    expect(nextReviewIssue(before, "b", ["d", "a"].map(issue))?.id).toBe("d");
    expect(nextReviewIssue(before, "d", ["a", "b"].map(issue))?.id).toBe("a");
    expect(nextReviewIssue(before, "d", [])).toBeNull();
    expect(() => nextReviewIssue(before, "gone", [])).toThrow(ReviewQueueChanged);
  });
});
