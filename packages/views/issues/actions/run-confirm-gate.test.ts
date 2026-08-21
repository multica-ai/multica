// @vitest-environment node
import { describe, it, expect } from "vitest";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses";
import type { IssueStatusEntry } from "@multica/core/types";
import { runConfirmIntent, resolveStatusCategory, type GateIssue } from "./run-confirm-gate";

// The canonical matrix for "does this write need confirming" (MUL-6463). The
// hook and table suites only prove they route on this answer.
function entry(key: string, category: string): IssueStatusEntry {
  return {
    id: key,
    workspace_id: "ws-1",
    key,
    name: key,
    description: "",
    category: category as IssueStatusEntry["category"],
    color: "#ff0000",
    is_system: false,
    position: 0,
    archived_at: null,
    created_at: "",
    updated_at: "",
  };
}

// `later` parks like Backlog, `rework` starts work like Todo, `qa` sits in
// in_review — the three custom shapes a raw key comparison gets wrong.
const CATALOG = buildIssueStatusCatalog([
  entry("later", "backlog"),
  entry("rework", "todo"),
  entry("qa", "in_review"),
]);
// A catalog that has not loaded: every custom key is unresolvable.
const COLD = buildIssueStatusCatalog(undefined);

function issue(overrides: Partial<GateIssue> = {}): GateIssue {
  return {
    id: "issue-1",
    status: "backlog",
    assignee_type: "agent",
    assignee_id: "agent-1",
    ...overrides,
  };
}

describe("resolveStatusCategory", () => {
  it("prefers the category the payload carries", () => {
    expect(resolveStatusCategory("qa", "in_review", COLD)).toBe("in_review");
  });

  it("resolves a built-in key without a catalog", () => {
    expect(resolveStatusCategory("backlog", undefined, COLD)).toBe("backlog");
  });

  it("resolves a custom key through the catalog", () => {
    expect(resolveStatusCategory("later", undefined, CATALOG)).toBe("backlog");
  });

  it("answers null — never a guess — for a custom key nothing can resolve", () => {
    // catalog.categoryOf would say `todo` here, which is the guess this gate
    // exists to avoid: it decides whether a write may start an agent.
    expect(CATALOG.categoryOf("unknown")).toBe("todo");
    expect(resolveStatusCategory("unknown", undefined, CATALOG)).toBeNull();
    expect(resolveStatusCategory("later", undefined, COLD)).toBeNull();
  });
});

describe("runConfirmIntent — assign", () => {
  it("confirms when the issue is not parked", () => {
    expect(
      runConfirmIntent(issue({ status: "todo" }), { assignee_type: "agent", assignee_id: "a-2" }, CATALOG),
    ).toEqual({ issueIds: ["issue-1"], mode: "assign", assigneeType: "agent", assigneeId: "a-2" });
  });

  it.each([
    ["built-in backlog", "backlog", undefined],
    ["custom backlog-category status", "later", undefined],
    ["custom key resolved by the payload", "anything", "backlog" as const],
  ])("applies directly for a parked issue (%s) — assigning there never starts a run", (_label, status, carried) => {
    expect(
      runConfirmIntent(
        issue({ status, status_category: carried }),
        { assignee_type: "agent", assignee_id: "a-2" },
        CATALOG,
      ),
    ).toBeNull();
  });

  it("confirms when the issue's own category is unresolvable", () => {
    // Fails toward the dialog: a dismissed dialog costs a click, a silent
    // start costs an agent run.
    expect(
      runConfirmIntent(issue({ status: "later" }), { assignee_type: "agent", assignee_id: "a-2" }, COLD),
    ).not.toBeNull();
  });

  it("applies directly for a member assignee", () => {
    expect(
      runConfirmIntent(issue({ status: "todo" }), { assignee_type: "member", assignee_id: "u-1" }, CATALOG),
    ).toBeNull();
  });
});

describe("runConfirmIntent — promote", () => {
  it.each([
    ["built-in todo", "backlog", "todo"],
    ["custom Todo-category status", "backlog", "rework"],
    ["custom backlog-category origin", "later", "rework"],
    ["a non-todo target that still starts a run", "backlog", "in_progress"],
    ["a custom in_review target", "backlog", "qa"],
  ])("confirms the promotion (%s)", (_label, from, to) => {
    expect(runConfirmIntent(issue({ status: from }), { status: to }, CATALOG)).toEqual({
      issueIds: ["issue-1"],
      mode: "promote",
      status: to,
      assigneeType: "agent",
      assigneeId: "agent-1",
    });
  });

  it.each([
    ["unresolvable origin", COLD, { status: "later" }, "todo"],
    ["unresolvable target", CATALOG, { status: "backlog" }, "unknown"],
  ])("confirms when %s leaves it unknown whether a run starts", (_label, catalog, from, to) => {
    expect(runConfirmIntent(issue(from), { status: to }, catalog)).not.toBeNull();
  });

  it.each([
    ["no owner", { status: "backlog", assignee_type: null, assignee_id: null }, "todo"],
    ["member owner", { status: "backlog", assignee_type: "member" as const, assignee_id: "u-1" }, "todo"],
    ["already active", { status: "todo" }, "in_progress"],
    ["closing the issue", { status: "backlog" }, "done"],
    ["cancelling the issue", { status: "backlog" }, "cancelled"],
    ["re-parking inside backlog", { status: "backlog" }, "later"],
    ["the same status again", { status: "backlog" }, "backlog"],
  ])("applies directly when no run starts (%s)", (_label, from, to) => {
    expect(runConfirmIntent(issue(from as Partial<GateIssue>), { status: to }, CATALOG)).toBeNull();
  });
});
