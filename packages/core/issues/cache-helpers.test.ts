import { describe, expect, it } from "vitest";
import type { Issue, IssueStatusCategory, ListIssuesCache } from "../types";
import { insertByPosition, patchIssueInBuckets, patchNeedsInvalidation } from "./cache-helpers";
import { statusCategoryOfKey, normalizeStatusPatch } from "./status-category";

const WS_ID = "ws-1";

// The server now sends status_category on EVERY issue, built-ins included, so
// fixtures must carry it. Without it a patch that leaves a stale category on the
// entity looks correct here while being wrong in production. (MUL-6243)
function mk(id: string, status: Issue["status"], position: number): Issue {
  return {
    id,
    status_category: statusCategoryOfKey(status),
    workspace_id: WS_ID,
    number: 1,
    identifier: `MUL-${id}`,
    title: id,
    description: null,
    status,
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
  properties: {},
    labels: [],
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  };
}

function cache(byStatus: ListIssuesCache["byStatus"]): ListIssuesCache {
  return { byStatus };
}

// The cache is bucketed by CATEGORY (MUL-6243), so tests index it with one.
function ids(c: ListIssuesCache, status: IssueStatusCategory): string[] {
  return (c.byStatus[status]?.issues ?? []).map((i: Issue) => i.id);
}

describe("insertByPosition", () => {
  it("inserts at the position-sorted slot", () => {
    const a = mk("a", "todo", 1);
    const c = mk("c", "todo", 3);
    const b = mk("b", "todo", 2);
    expect(insertByPosition([a, c], b).map((i) => i.id)).toEqual([
      "a",
      "b",
      "c",
    ]);
  });

  it("appends when the new position is the largest", () => {
    const a = mk("a", "todo", 1);
    const z = mk("z", "todo", 9);
    expect(insertByPosition([a], z).map((i) => i.id)).toEqual(["a", "z"]);
  });

  it("prepends when the new position is the smallest", () => {
    const b = mk("b", "todo", 2);
    const a = mk("a", "todo", 1);
    expect(insertByPosition([b], a).map((i) => i.id)).toEqual(["a", "b"]);
  });
});

describe("patchIssueInBuckets — cross-status move", () => {
  it("inserts the moved card at its position slot, not the end", () => {
    const c0 = cache({
      todo: { issues: [mk("moved", "todo", 5)], total: 1 },
      in_progress: {
        issues: [mk("x", "in_progress", 1), mk("y", "in_progress", 3)],
        total: 2,
      },
    });
    // Move "moved" into in_progress at position 2 (between x and y).
    const next = patchIssueInBuckets(c0, "moved", {
      status: "in_progress",
      position: 2,
    });
    expect(ids(next, "in_progress")).toEqual(["x", "moved", "y"]);
    expect(ids(next, "todo")).toEqual([]);
  });

  it("adjusts both bucket totals", () => {
    const c0 = cache({
      todo: { issues: [mk("moved", "todo", 5)], total: 1 },
      in_progress: { issues: [mk("x", "in_progress", 1)], total: 1 },
    });
    const next = patchIssueInBuckets(c0, "moved", {
      status: "in_progress",
      position: 2,
    });
    expect(next.byStatus.todo?.total).toBe(0);
    expect(next.byStatus.in_progress?.total).toBe(2);
  });

  // MUL-4261: `cancelled` is now a first-class paginated bucket, so cancelling
  // an issue rebuckets it into `cancelled` (instead of dropping it) and the
  // rebucketed card stays locatable for later patches.
  it("rebuckets a cancelled issue and keeps it locatable", () => {
    const c0 = cache({
      todo: { issues: [mk("a", "todo", 1)], total: 1 },
      cancelled: { issues: [], total: 0 },
    });
    const cancelled = patchIssueInBuckets(c0, "a", { status: "cancelled" });
    expect(ids(cancelled, "todo")).toEqual([]);
    expect(ids(cancelled, "cancelled")).toEqual(["a"]);
    expect(cancelled.byStatus.cancelled?.total).toBe(1);

    // A follow-up edit still finds the card in the cancelled bucket.
    const renamed = patchIssueInBuckets(cancelled, "a", { title: "renamed" });
    expect(renamed.byStatus.cancelled?.issues[0]?.title).toBe("renamed");
  });
});

describe("patchIssueInBuckets — same status", () => {
  it("keeps the slot for a plain field update (no reorder)", () => {
    const c0 = cache({
      todo: {
        issues: [mk("a", "todo", 1), mk("b", "todo", 2), mk("c", "todo", 3)],
        total: 3,
      },
    });
    // A remote label/title edit must not move the card.
    const next = patchIssueInBuckets(c0, "b", { title: "renamed" });
    expect(ids(next, "todo")).toEqual(["a", "b", "c"]);
    expect(next.byStatus.todo?.issues[1]?.title).toBe("renamed");
  });

  it("re-sorts within the column when position changes", () => {
    const c0 = cache({
      todo: {
        issues: [mk("a", "todo", 1), mk("b", "todo", 2), mk("c", "todo", 3)],
        total: 3,
      },
    });
    // Drag "a" below "b" (new position 2.5).
    const next = patchIssueInBuckets(c0, "a", { position: 2.5 });
    expect(ids(next, "todo")).toEqual(["b", "a", "c"]);
  });
});

describe("patchIssueInBuckets — unknown issue", () => {
  it("returns the cache unchanged when the id is absent", () => {
    const c0 = cache({ todo: { issues: [mk("a", "todo", 1)], total: 1 } });
    expect(patchIssueInBuckets(c0, "ghost", { position: 9 })).toBe(c0);
  });
});

// `Issue.status` is still the closed 7-value union — widening it to `string` is
// the follow-up PR. At runtime the server already sends custom keys, so these
// tests cast to describe the real payloads the cache has to survive.
const key = (k: string) => k as Issue["status"];
const cat = (c: string) => c as IssueStatusCategory;

// A status KEY change WITHIN one category must not be treated as a bucket move.
// The cache is bucketed by category while `patch.status` is a key, so comparing
// them directly sent `in_review` -> a custom `human_review` down the cross-bucket
// path, which deleted from and re-inserted into the same bucket using a
// pre-delete snapshot: the card ended up duplicated and the total one too high.
describe("patchIssueInBuckets — status key changes within a category", () => {
  function inReviewCache(): ListIssuesCache {
    return cache({
      in_review: { issues: [mk("a", "in_review", 100), mk("b", "in_review", 200)], total: 2 },
    });
  }

  it("built-in -> custom in the same category replaces in place", () => {
    const next = patchIssueInBuckets(inReviewCache(), "a", {
      status: key("human_review"),
      status_category: cat("in_review"),
    });
    expect(ids(next, "in_review")).toEqual(["a", "b"]);
    expect(next.byStatus.in_review?.total).toBe(2);
    expect(next.byStatus.in_review?.issues.find((i) => i.id === "a")?.status).toBe("human_review");
  });

  it("custom A -> custom B in the same category replaces in place", () => {
    const start = cache({
      in_review: { issues: [{ ...mk("a", key("human_review"), 100), status_category: cat("in_review") }], total: 1 },
    });
    const next = patchIssueInBuckets(start, "a", {
      status: key("second_review"),
      status_category: cat("in_review"),
    });
    expect(ids(next, "in_review")).toEqual(["a"]);
    expect(next.byStatus.in_review?.total).toBe(1);
    expect(next.byStatus.in_review?.issues[0]?.status).toBe("second_review");
  });

  it("custom cross-category move updates both buckets exactly once", () => {
    const next = patchIssueInBuckets(inReviewCache(), "a", {
      status: key("gate_approved"),
      status_category: cat("done"),
    });
    expect(ids(next, "in_review")).toEqual(["b"]);
    expect(next.byStatus.in_review?.total).toBe(1);
    expect(ids(next, "done")).toEqual(["a"]);
    expect(next.byStatus.done?.total).toBe(1);
  });

  // merged inherits the PREVIOUS issue's status_category, so resolving from it
  // would silently keep the card in its old column after a real move.
  it("ignores the stale category on the existing issue", () => {
    const start = cache({
      in_review: { issues: [{ ...mk("a", key("human_review"), 100), status_category: cat("in_review") }], total: 1 },
    });
    const next = patchIssueInBuckets(start, "a", { status: "done" });
    expect(ids(next, "in_review")).toEqual([]);
    expect(ids(next, "done")).toEqual(["a"]);
  });

  it("no-ops on an unresolvable custom status, and flags it for invalidation", () => {
    const start = inReviewCache();
    const next = patchIssueInBuckets(start, "a", { status: key("mystery_status") });
    expect(next).toBe(start);
    expect(patchNeedsInvalidation({ status: key("mystery_status") })).toBe(true);
    expect(patchNeedsInvalidation({ status: "done" })).toBe(false);
    expect(patchNeedsInvalidation({ title: "no status change" })).toBe(false);
    expect(patchNeedsInvalidation({ status: key("x"), status_category: cat("done") })).toBe(false);
  });
});

// The entity must never disagree with the bucket it sits in. A bare
// `{...issue, ...patch}` kept the previous status_category, and because the
// batch API returns only `{updated: n}` and does not refetch bucketed lists,
// that inconsistency was permanent — the next off-window count then decremented
// the wrong bucket.
describe("patchIssueInBuckets — status_category follows status", () => {
  it("built-in -> built-in rewrites the category on the entity", () => {
    const start = cache({ todo: { issues: [mk("a", "todo", 100)], total: 1 } });
    expect(start.byStatus.todo?.issues[0]?.status_category).toBe("todo");

    const next = patchIssueInBuckets(start, "a", { status: "done" });
    const moved = next.byStatus.done?.issues[0];
    expect(moved?.status).toBe("done");
    expect(moved?.status_category).toBe("done");
    expect(ids(next, "todo")).toEqual([]);
  });

  it("built-in -> custom takes the authoritative category from the patch", () => {
    const start = cache({ todo: { issues: [mk("a", "todo", 100)], total: 1 } });
    const next = patchIssueInBuckets(start, "a", {
      status: key("human_review"),
      status_category: cat("in_review"),
    });
    const moved = next.byStatus.in_review?.issues[0];
    expect(moved?.status).toBe("human_review");
    expect(moved?.status_category).toBe("in_review");
  });

  it("never leaves the previous category on an unresolvable status", () => {
    expect(normalizeStatusPatch({ status: key("mystery") }).status_category).toBeUndefined();
    expect(normalizeStatusPatch({ status: "done" }).status_category).toBe("done");
    // A patch that does not touch status is passed through untouched.
    const untouched = { title: "renamed" };
    expect(normalizeStatusPatch(untouched)).toBe(untouched);
  });
});
