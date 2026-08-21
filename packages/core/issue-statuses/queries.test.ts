// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  buildIssueStatusCatalog,
  compareIssueStatusEntries,
  isIssueStatusCategory,
  issueStatusColor,
} from "./queries";
import type { IssueStatusEntry } from "../types";

function entry(key: string, category: string, name = key, archivedAt: string | null = null): IssueStatusEntry {
  return {
    id: key, workspace_id: "ws-1", key, name, description: "",
    category: category as IssueStatusEntry["category"], color: "#123456",
    is_system: false, position: 0, archived_at: archivedAt,
    created_at: "", updated_at: "",
  };
}

describe("buildIssueStatusCatalog", () => {
  // The catalog is fetched async, but a status must render on the very first
  // paint. Built-in keys are their own category, so an unloaded catalog still
  // resolves all 7 — which is what keeps the default workspace identical
  // before the request lands.
  it("resolves every built-in with no catalog loaded", () => {
    const c = buildIssueStatusCatalog(undefined);
    expect(c.isLoaded).toBe(false);
    for (const key of ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"]) {
      expect(c.categoryOf(key)).toBe(key);
    }
    expect(c.labelOf("in_review")).toBe("In Review");
  });

  it("maps a custom status to its category and name", () => {
    const c = buildIssueStatusCatalog([entry("human_review", "in_review", "Human Review")]);
    expect(c.categoryOf("human_review")).toBe("in_review");
    expect(c.labelOf("human_review")).toBe("Human Review");
    expect(c.entryOf("human_review")?.key).toBe("human_review");
  });

  // An issue can carry a status created moments ago in another session. Falling
  // back to a renderable category beats dropping the issue.
  it("falls back for a status the catalog does not know", () => {
    const c = buildIssueStatusCatalog([]);
    expect(c.categoryOf("ghost")).toBe("todo");
    expect(c.labelOf("ghost")).toBe("ghost");
    expect(c.entryOf("ghost")).toBeUndefined();
  });

  it("ignores a corrupt category rather than trusting it", () => {
    const c = buildIssueStatusCatalog([entry("weird", "started")]);
    expect(c.categoryOf("weird")).toBe("todo");
  });

  // The 7 built-ins carry a seeded hex the server refuses to let anyone edit,
  // and every surface paints them from their category token instead. A caller
  // that read the seed drew the SAME status in two different greens depending
  // on which control it was looking at. (MUL-6440)
  it("gives a built-in no color of its own", () => {
    const builtIn = { ...entry("in_review", "in_review", "In Review"), is_system: true, color: "#22c55e" };
    const c = buildIssueStatusCatalog([builtIn, entry("qa", "in_review", "QA")]);
    expect(c.colorOf("in_review")).toBeNull();
    expect(c.colorOf("qa")).toBe("#123456");
  });

  it("gives no color to a status it cannot resolve", () => {
    const c = buildIssueStatusCatalog(undefined);
    expect(c.colorOf("in_review")).toBeNull();
    expect(c.colorOf("ghost")).toBeNull();
  });

  it("issueStatusColor answers the same question for an entry in hand", () => {
    expect(issueStatusColor(undefined)).toBeNull();
    expect(issueStatusColor({ ...entry("qa", "in_review"), is_system: true })).toBeNull();
    expect(issueStatusColor(entry("qa", "in_review"))).toBe("#123456");
  });

  it("groups by category in catalog order", () => {
    const c = buildIssueStatusCatalog([
      entry("human_review", "in_review"),
      entry("gate_approved", "done"),
    ]);
    expect(c.inCategory("in_review").map((e) => e.key)).toEqual(["human_review"]);
    expect(c.inCategory("backlog")).toEqual([]);
  });

  it("isIssueStatusCategory accepts exactly the 7", () => {
    expect(isIssueStatusCategory("in_review")).toBe(true);
    expect(isIssueStatusCategory("human_review")).toBe(false);
  });
});

// Archiving retires a status from FUTURE assignment but leaves existing issues
// on it. Those issues must keep their real name, colour and category — dropping
// archived rows from resolution would degrade them to a raw key with a guessed
// category, which is exactly what the product decision rules out.
describe("archived statuses stay resolvable", () => {
  const archived = entry("gate_approved", "done", "Gate Approved", "2026-01-01T00:00:00Z");
  const active = entry("human_review", "in_review", "Human Review");
  const c = buildIssueStatusCatalog([active, archived]);

  it("keeps name and category for an issue left on an archived status", () => {
    expect(c.labelOf("gate_approved")).toBe("Gate Approved");
    expect(c.categoryOf("gate_approved")).toBe("done");
    expect(c.entryOf("gate_approved")?.color).toBe("#123456");
  });

  it("excludes archived from the assignable set", () => {
    expect(c.activeStatuses.map((e) => e.key)).toEqual(["human_review"]);
    expect(c.statuses.map((e) => e.key)).toEqual(["human_review", "gate_approved"]);
  });

  it("excludes archived from a category's pickable list", () => {
    expect(c.inCategory("done")).toEqual([]);
    expect(c.inCategory("in_review").map((e) => e.key)).toEqual(["human_review"]);
  });
});

describe("compareIssueStatusEntries", () => {
  function at(key: string, category: string, position: number): IssueStatusEntry {
    return { ...entry(key, category), position };
  }

  // Mirrors ListIssueStatusEntries in issue_status.sql. An optimistic reorder
  // re-sorts the cached array with this, so a drift from the server's ORDER BY
  // would show one order until the refetch lands and a different one after.
  it("orders by category rank before position", () => {
    const sorted = [at("qa", "done", 1), at("review", "in_review", 9)].sort(
      compareIssueStatusEntries,
    );
    expect(sorted.map((e) => e.key)).toEqual(["review", "qa"]);
  });

  it("orders by position within a category", () => {
    const sorted = [at("b", "in_review", 2), at("a", "in_review", 1)].sort(
      compareIssueStatusEntries,
    );
    expect(sorted.map((e) => e.key)).toEqual(["a", "b"]);
  });

  // Positions collide while a reorder is mid-flight; without the key tiebreak
  // the list order would depend on the sort's stability.
  it("falls back to key when positions tie", () => {
    const sorted = [at("zeta", "todo", 1), at("alpha", "todo", 1)].sort(
      compareIssueStatusEntries,
    );
    expect(sorted.map((e) => e.key)).toEqual(["alpha", "zeta"]);
  });

  // The built-in is seeded at position 0 and cannot be PATCHed, so it is
  // permanently the head of its category. Reorder writes start at 1 for that
  // reason; if the comparator ever let a custom status sort ahead of it, the
  // picker would open on a status the workspace never chose as its default.
  it("keeps the built-in ahead of custom statuses in its category", () => {
    const builtIn = { ...at("in_review", "in_review", 0), is_system: true };
    const sorted = [at("qa", "in_review", 1), builtIn].sort(compareIssueStatusEntries);
    expect(sorted.map((e) => e.key)).toEqual(["in_review", "qa"]);
  });
});
