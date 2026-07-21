import { describe, expect, it } from "vitest";
import type { Issue, IssueStatusDefinition } from "@multica/core/types";
import { issueMatchesStatusFilter, resolveStatusFilterIds } from "./status-filter";

// MUL-4809 — status filter selections are persisted, and older builds stored the
// 7 legacy tokens. Catalog-driven filtering selects by catalog id, so a stored
// selection can hold both shapes and must keep working without rewriting storage.

function status(over: Partial<IssueStatusDefinition>): IssueStatusDefinition {
  return {
    id: "id",
    workspace_id: "ws",
    name: "Name",
    description: "",
    icon: "todo",
    color: "muted-foreground",
    category: "todo",
    system_key: null,
    is_system: false,
    is_default: false,
    position: 0,
    archived: false,
    archived_at: null,
    created_at: "",
    updated_at: "",
    ...over,
  };
}

const TODO = status({ id: "todo-id", name: "Todo", system_key: "todo", is_system: true });
const REVIEW = status({
  id: "review-id",
  name: "In Review",
  system_key: "in_review",
  is_system: true,
  category: "in_progress",
});
const CUSTOM = status({ id: "qa-id", name: "Needs QA", category: "in_progress" });
const CATALOG = [TODO, REVIEW, CUSTOM];

describe("resolveStatusFilterIds", () => {
  it("keeps catalog ids as-is", () => {
    expect(resolveStatusFilterIds([CUSTOM.id], CATALOG)).toEqual([CUSTOM.id]);
  });

  it("maps a legacy token to the built-in carrying that system_key", () => {
    expect(resolveStatusFilterIds(["in_review"], CATALOG)).toEqual([REVIEW.id]);
  });

  it("does NOT fold custom statuses into a legacy Category token", () => {
    // "todo" was a lane the user picked; it must not silently widen to every
    // status that happens to share the Category.
    expect(resolveStatusFilterIds(["todo"], CATALOG)).toEqual([TODO.id]);
  });

  it("handles a mixed selection and de-duplicates", () => {
    const got = resolveStatusFilterIds(["todo", TODO.id, CUSTOM.id], CATALOG);
    expect(got.sort()).toEqual([CUSTOM.id, TODO.id].sort());
  });

  it("drops entries that resolve to nothing instead of erroring", () => {
    expect(resolveStatusFilterIds(["no-such-token", "missing-id"], CATALOG)).toEqual([]);
  });

  it("returns nothing when the catalog has not loaded", () => {
    expect(resolveStatusFilterIds(["todo"], [])).toEqual([]);
  });
});

describe("issueMatchesStatusFilter", () => {
  it("passes everything when no filter is set", () => {
    expect(issueMatchesStatusFilter({ status: "todo", status_id: TODO.id }, [], CATALOG)).toBe(true);
  });

  it("matches an issue by its status_id", () => {
    const issue: Pick<Issue, "status" | "status_id"> = {
      status: "in_progress",
      status_id: CUSTOM.id,
    };
    expect(issueMatchesStatusFilter(issue, [CUSTOM.id], CATALOG)).toBe(true);
    expect(issueMatchesStatusFilter(issue, [REVIEW.id], CATALOG)).toBe(false);
  });

  it("resolves a legacy selection against the issue's status_id", () => {
    const issue: Pick<Issue, "status" | "status_id"> = {
      status: "in_review",
      status_id: REVIEW.id,
    };
    expect(issueMatchesStatusFilter(issue, ["in_review"], CATALOG)).toBe(true);
    // The custom status shares the Category but is a different lane.
    const custom: Pick<Issue, "status" | "status_id"> = {
      status: "in_progress",
      status_id: CUSTOM.id,
    };
    expect(issueMatchesStatusFilter(custom, ["in_review"], CATALOG)).toBe(false);
  });

  it("falls back to the legacy token when the issue has no status_id", () => {
    // Unseeded workspace / server predating the catalog.
    expect(issueMatchesStatusFilter({ status: "todo", status_id: null }, ["todo"], CATALOG)).toBe(true);
    expect(issueMatchesStatusFilter({ status: "done", status_id: null }, ["todo"], CATALOG)).toBe(false);
  });

  it("does not hide everything while the catalog is still loading", () => {
    // Catalog empty => ids resolve to nothing; the legacy comparison still runs.
    expect(issueMatchesStatusFilter({ status: "todo", status_id: TODO.id }, ["todo"], [])).toBe(true);
  });
});
