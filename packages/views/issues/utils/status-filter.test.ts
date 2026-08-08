import { describe, expect, it } from "vitest";
import type { Issue, IssueStatusDefinition } from "@multica/core/types";
import {
  issueMatchesStatusFilter,
  resolveStatusFilterIds,
  selectionToLegacyTokens,
  resolveStatusFilterTokens,
} from "./status-filter";

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

  it("matches a NULL-status_id row when a BUILT-IN is selected by catalog id", () => {
    // A workspace upgraded before backfill: the row carries only the legacy
    // token. Selecting the built-in Todo (a catalog UUID) must still find it —
    // the fix for the open_only/status-filter data-loss regression (MUL-4809).
    const legacy: Pick<Issue, "status" | "status_id"> = { status: "todo", status_id: null };
    expect(issueMatchesStatusFilter(legacy, [TODO.id], CATALOG)).toBe(true);
    expect(issueMatchesStatusFilter(legacy, [REVIEW.id], CATALOG)).toBe(false);
  });

  it("does NOT let a custom status id claim a legacy NULL row", () => {
    // The NULL row was never in the custom status, so a custom-only selection
    // must not match it — even though they share the in_progress Category.
    const legacy: Pick<Issue, "status" | "status_id"> = {
      status: "in_progress",
      status_id: null,
    };
    expect(issueMatchesStatusFilter(legacy, [CUSTOM.id], CATALOG)).toBe(false);
  });
});

describe("selectionToLegacyTokens", () => {
  it("maps a built-in catalog id to its system_key", () => {
    expect(selectionToLegacyTokens([TODO.id], CATALOG)).toEqual(["todo"]);
  });
  it("keeps a raw legacy token", () => {
    expect(selectionToLegacyTokens(["in_review"], CATALOG)).toEqual(["in_review"]);
  });
  it("drops a custom status id (a NULL legacy row was never in it)", () => {
    expect(selectionToLegacyTokens([CUSTOM.id], CATALOG)).toEqual([]);
  });
  it("drops a catalog-id shape while the catalog is unloaded", () => {
    expect(
      selectionToLegacyTokens(["11111111-1111-4111-8111-111111111111"], []),
    ).toEqual([]);
  });
});

describe("resolveStatusFilterTokens", () => {
  // The List / status-grouped Board pick their server branches from the legacy
  // lanes. The filter menu stores a catalog id even for built-ins, so without
  // this projection the lane set is empty and the surface renders zero rows.
  it("projects a BUILT-IN catalog id onto its own lane", () => {
    expect(resolveStatusFilterTokens([TODO.id], CATALOG)).toEqual(["todo"]);
  });

  it("projects a CUSTOM catalog id onto its Category lane", () => {
    expect(resolveStatusFilterTokens([CUSTOM.id], CATALOG)).toEqual([
      CUSTOM.category,
    ]);
  });

  it("keeps a legacy token from an older persisted selection", () => {
    expect(resolveStatusFilterTokens(["in_review"], CATALOG)).toEqual([
      "in_review",
    ]);
  });

  it("drops ids it cannot resolve so the caller shows every lane", () => {
    // Cold start: catalog still loading. Returning nothing makes the caller fall
    // back to all lanes — the row queries carry status_ids, so rows stay correct
    // and no lane is wrongly hidden. Returning the raw id would hide everything.
    const unloadedId = "11111111-1111-4111-8111-111111111111";
    expect(resolveStatusFilterTokens([unloadedId], [])).toEqual([]);
  });
});

describe("resolveStatusFilterIds cold start", () => {
  // Before the catalog settles nothing can be matched by id or system_key. A
  // catalog-id-shaped entry is already what the server wants, so it must pass
  // through as status_ids; otherwise the caller falls back to the legacy
  // `statuses` facet and the first request after a refresh sends UUIDs into a
  // 7-token enum, which the server rejects with 400.
  it("passes catalog-id-shaped entries through while the catalog is loading", () => {
    const id = "11111111-1111-4111-8111-111111111111";
    expect(resolveStatusFilterIds([id], [])).toEqual([id]);
  });

  it("still drops a legacy token while the catalog is loading", () => {
    // A token cannot be turned into an id without the catalog, and sending it as
    // status_ids would 400. It stays on the legacy facet instead.
    expect(resolveStatusFilterIds(["todo"], [])).toEqual([]);
  });
});
