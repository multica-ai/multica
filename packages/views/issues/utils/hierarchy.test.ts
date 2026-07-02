import { describe, it, expect } from "vitest";
import type { Issue } from "@multica/core/types";
import { buildHierarchy, type ParentInfo } from "./hierarchy";

function makeIssue(overrides: Partial<Issue> & { stage?: number | null } = {}): Issue {
  return {
    id: "i-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "MUL-1",
    title: "Test",
    description: null,
    status: "todo",
    priority: "medium",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "u-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    start_date: null,
    due_date: null,
    metadata: {},
    stage: null,
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
    ...overrides,
  } as Issue;
}

function makeParentInfo(parentId: string, identifier: string, status: string): ParentInfo {
  return { parentId, identifier, status };
}

describe("buildHierarchy", () => {
  // ── Scenario 1: same-status nesting ──
  it("nests children under their parent when parent and children share the same status", () => {
    const parent = makeIssue({ id: "p1", identifier: "OXY-1", status: "todo" });
    const child1 = makeIssue({ id: "c1", identifier: "OXY-2", status: "todo", parent_issue_id: "p1" });
    const child2 = makeIssue({ id: "c2", identifier: "OXY-3", status: "todo", parent_issue_id: "p1" });

    const issues = [parent, child1, child2];
    const childrenMap = new Map<string, Issue[]>([["p1", [child1, child2]]]);
    const statusIssueIds = new Set(["p1", "c1", "c2"]);
    const expandedParents = new Set<string>();

    const result = buildHierarchy(issues, childrenMap, statusIssueIds, expandedParents);

    // Parent is top-level with isParent=true, childCount=2.
    // Children are not expanded inline (parent not expanded), so the fallback
    // appends them as orphaned items.
    expect(result).toHaveLength(3);
    expect(result[0]!.issue.id).toBe("p1");
    expect(result[0]!.isParent).toBe(true);
    expect(result[0]!.childCount).toBe(2);
    expect(result[0]!.indent).toBe(0);

    // Children appended as orphaned (parent not in expandedParents).
    expect(result[1]!.issue.id).toBe("c1");
    expect(result[1]!.orphaned).toBe(true);
    expect(result[2]!.issue.id).toBe("c2");
    expect(result[2]!.orphaned).toBe(true);
  });

  it("expands children when parent is in expandedParents set", () => {
    const parent = makeIssue({ id: "p1", identifier: "OXY-1", status: "todo" });
    const child1 = makeIssue({ id: "c1", identifier: "OXY-2", status: "todo", parent_issue_id: "p1" });
    const child2 = makeIssue({ id: "c2", identifier: "OXY-3", status: "todo", parent_issue_id: "p1" });

    const issues = [parent, child1, child2];
    const childrenMap = new Map<string, Issue[]>([["p1", [child1, child2]]]);
    const statusIssueIds = new Set(["p1", "c1", "c2"]);
    const expandedParents = new Set(["p1"]);

    const result = buildHierarchy(issues, childrenMap, statusIssueIds, expandedParents);

    // Parent + 2 children rendered.
    expect(result).toHaveLength(3);
    expect(result[0]!.issue.id).toBe("p1");
    expect(result[0]!.isParent).toBe(true);
    // Children are rendered at indent=1 when parent is expanded.
    expect(result[1]!.issue.id).toBe("c1");
    expect(result[1]!.indent).toBe(1);
    expect(result[1]!.isParent).toBe(false);
    expect(result[2]!.issue.id).toBe("c2");
    expect(result[2]!.indent).toBe(1);
  });

  // ── Scenario 2: all-cross-status parent ──
  it("shows crossStatusChildCount and crossStatusChildren when parent has only cross-status children", () => {
    const parent = makeIssue({ id: "p1", identifier: "OXY-1", status: "done" });
    const child1 = makeIssue({ id: "c1", identifier: "OXY-2", status: "todo", parent_issue_id: "p1" });

    // Parent is in "done" status group; child is in "todo" — different group.
    const issues = [parent]; // only parent in this group
    const childrenMap = new Map<string, Issue[]>([["p1", [child1]]]);
    const statusIssueIds = new Set(["p1"]); // child1 is NOT in this status group
    const expandedParents = new Set<string>();
    const parentInfoMap = new Map([["p1", makeParentInfo("p1", "OXY-1", "done")]]);

    const result = buildHierarchy(issues, childrenMap, statusIssueIds, expandedParents, parentInfoMap);

    // Parent should NOT be marked as isParent (no same-status children),
    // but should have crossStatusChildCount > 0.
    expect(result).toHaveLength(1);
    expect(result[0]!.issue.id).toBe("p1");
    expect(result[0]!.isParent).toBe(false);
    expect(result[0]!.childCount).toBe(0);
    expect(result[0]!.crossStatusChildCount).toBe(1);
    expect(result[0]!.crossStatusChildren).toHaveLength(1);
    expect(result[0]!.crossStatusChildren[0]!.identifier).toBe("OXY-2");
  });

  // ── Scenario 3: mixed (some same-status + some cross-status) ──
  it("handles mixed children: some same-status, some cross-status", () => {
    const parent = makeIssue({ id: "p1", identifier: "OXY-1", status: "todo" });
    const sameChild = makeIssue({ id: "c1", identifier: "OXY-2", status: "todo", parent_issue_id: "p1" });
    const crossChild = makeIssue({ id: "c2", identifier: "OXY-3", status: "done", parent_issue_id: "p1" });

    const issues = [parent, sameChild];
    const childrenMap = new Map<string, Issue[]>([["p1", [sameChild, crossChild]]]);
    const statusIssueIds = new Set(["p1", "c1"]); // c2 is cross-status
    const expandedParents = new Set(["p1"]);
    const parentInfoMap = new Map([["p1", makeParentInfo("p1", "OXY-1", "todo")]]);

    const result = buildHierarchy(issues, childrenMap, statusIssueIds, expandedParents, parentInfoMap);

    expect(result).toHaveLength(2); // parent + same-status child
    expect(result[0]!.issue.id).toBe("p1");
    expect(result[0]!.isParent).toBe(true);
    expect(result[0]!.childCount).toBe(1);
    expect(result[0]!.crossStatusChildCount).toBe(1);
    expect(result[0]!.crossStatusChildren).toHaveLength(1);
    expect(result[0]!.crossStatusChildren[0]!.identifier).toBe("OXY-3");

    // Cross-status child rows appear in the crossStatusChildren list only.
    // The same-status child is rendered inline.
    expect(result[1]!.issue.id).toBe("c1");
    expect(result[1]!.indent).toBe(1);
  });

  // ── Scenario 4: multi-level recursion ──
  it("supports multi-level nesting: parent → child → grandchild", () => {
    const parent = makeIssue({ id: "p1", identifier: "OXY-1", status: "todo" });
    const child = makeIssue({ id: "c1", identifier: "OXY-2", status: "todo", parent_issue_id: "p1" });
    const grandchild = makeIssue({ id: "g1", identifier: "OXY-3", status: "todo", parent_issue_id: "c1" });

    const issues = [parent, child, grandchild];
    const childrenMap = new Map<string, Issue[]>([
      ["p1", [child]],
      ["c1", [grandchild]],
    ]);
    const statusIssueIds = new Set(["p1", "c1", "g1"]);
    const expandedParents = new Set(["p1", "c1"]);

    const result = buildHierarchy(issues, childrenMap, statusIssueIds, expandedParents);

    expect(result).toHaveLength(3);
    expect(result[0]!.issue.id).toBe("p1");
    expect(result[0]!.isParent).toBe(true);
    expect(result[0]!.indent).toBe(0);

    expect(result[1]!.issue.id).toBe("c1");
    expect(result[1]!.isParent).toBe(true); // has a grandchild
    expect(result[1]!.indent).toBe(1);

    expect(result[2]!.issue.id).toBe("g1");
    expect(result[2]!.isParent).toBe(false);
    expect(result[2]!.indent).toBe(2);
  });

  it("does not expand grandchild when child is not expanded", () => {
    const parent = makeIssue({ id: "p1", identifier: "OXY-1", status: "todo" });
    const child = makeIssue({ id: "c1", identifier: "OXY-2", status: "todo", parent_issue_id: "p1" });
    const grandchild = makeIssue({ id: "g1", identifier: "OXY-3", status: "todo", parent_issue_id: "c1" });

    const issues = [parent, child, grandchild];
    const childrenMap = new Map<string, Issue[]>([
      ["p1", [child]],
      ["c1", [grandchild]],
    ]);
    const statusIssueIds = new Set(["p1", "c1", "g1"]);
    const expandedParents = new Set(["p1"]); // only parent expanded, not child

    const result = buildHierarchy(issues, childrenMap, statusIssueIds, expandedParents);

    // Parent expanded → shows child inline. Child NOT expanded → grandchild
    // is appended as orphaned by the fallback.
    expect(result).toHaveLength(3);
    expect(result[0]!.issue.id).toBe("p1");
    expect(result[1]!.issue.id).toBe("c1");
    expect(result[1]!.indent).toBe(1);
    // Grandchild is orphaned since child is not expanded.
    expect(result[2]!.issue.id).toBe("g1");
    expect(result[2]!.orphaned).toBe(true);
  });

  // ── Scenario 5: cross-status child renders as top-level with parentInfo ──
  it("renders cross-status child at top level with parentInfo chip", () => {
    const crossChild = makeIssue({
      id: "c1",
      identifier: "OXY-2",
      status: "todo",
      parent_issue_id: "p1",
    });

    const issues = [crossChild]; // only the child in this group, parent is in another
    const childrenMap = new Map<string, Issue[]>();
    const statusIssueIds = new Set(["c1"]); // parent p1 NOT in this group
    const expandedParents = new Set<string>();
    const parentInfoMap = new Map([["p1", makeParentInfo("p1", "OXY-1", "done")]]);

    const result = buildHierarchy(issues, childrenMap, statusIssueIds, expandedParents, parentInfoMap);

    expect(result).toHaveLength(1);
    expect(result[0]!.issue.id).toBe("c1");
    expect(result[0]!.isParent).toBe(false);
    expect(result[0]!.indent).toBe(0);
    expect(result[0]!.parentInfo).toBeDefined();
    expect(result[0]!.parentInfo!.identifier).toBe("OXY-1");
    expect(result[0]!.parentInfo!.status).toBe("done");
  });

  // ── Scenario 6: orphan fallback ──
  it("marks orphaned issues that have a parent in the same status but parent is not in the list", () => {
    // Child whose parent is in the same status group but the parent issue
    // is not in the provided issues array (e.g. filtered out).
    const orphan = makeIssue({
      id: "o1",
      identifier: "OXY-5",
      status: "todo",
      parent_issue_id: "p-missing",
    });

    const issues = [orphan];
    const childrenMap = new Map<string, Issue[]>();
    const statusIssueIds = new Set(["o1", "p-missing"]); // parent would be in same status if present
    const expandedParents = new Set<string>();

    const result = buildHierarchy(issues, childrenMap, statusIssueIds, expandedParents);

    // Should be rendered as top-level with orphaned flag.
    expect(result).toHaveLength(1);
    expect(result[0]!.issue.id).toBe("o1");
    expect(result[0]!.orphaned).toBe(true);
    expect(result[0]!.indent).toBe(0);
  });

  // ── Scenario 7: no children at all ──
  it("handles issues with no children gracefully", () => {
    const issue1 = makeIssue({ id: "a1", identifier: "OXY-10", status: "todo" });
    const issue2 = makeIssue({ id: "a2", identifier: "OXY-11", status: "todo" });

    const issues = [issue1, issue2];
    const childrenMap = new Map<string, Issue[]>();
    const statusIssueIds = new Set(["a1", "a2"]);
    const expandedParents = new Set<string>();

    const result = buildHierarchy(issues, childrenMap, statusIssueIds, expandedParents);

    expect(result).toHaveLength(2);
    expect(result[0]!.isParent).toBe(false);
    expect(result[0]!.childCount).toBe(0);
    expect(result[0]!.crossStatusChildCount).toBe(0);
    expect(result[1]!.isParent).toBe(false);
  });

  // ── Scenario 8: multiple top-level parents ──
  it("handles multiple top-level parent issues independently", () => {
    const parent1 = makeIssue({ id: "p1", identifier: "OXY-1", status: "todo" });
    const child1 = makeIssue({ id: "c1", identifier: "OXY-2", status: "todo", parent_issue_id: "p1" });
    const parent2 = makeIssue({ id: "p2", identifier: "OXY-3", status: "todo" });
    const child2 = makeIssue({ id: "c2", identifier: "OXY-4", status: "todo", parent_issue_id: "p2" });

    const issues = [parent1, child1, parent2, child2];
    const childrenMap = new Map<string, Issue[]>([
      ["p1", [child1]],
      ["p2", [child2]],
    ]);
    const statusIssueIds = new Set(["p1", "c1", "p2", "c2"]);
    const expandedParents = new Set(["p1", "p2"]);

    const result = buildHierarchy(issues, childrenMap, statusIssueIds, expandedParents);

    expect(result).toHaveLength(4);
    expect(result[0]!.issue.id).toBe("p1");
    expect(result[1]!.issue.id).toBe("c1");
    expect(result[1]!.indent).toBe(1);
    expect(result[2]!.issue.id).toBe("p2");
    expect(result[3]!.issue.id).toBe("c2");
    expect(result[3]!.indent).toBe(1);
  });
});
