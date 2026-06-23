import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import {
  ISSUE_GRAPH_NODE_WIDTH,
  layoutIssueGraph,
} from "./issue-graph-layout";

function issue(
  id: string,
  number: number,
  parentIssueId: string | null = null,
): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number,
    identifier: `MUL-${number}`,
    title: id,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: parentIssueId,
    project_id: null,
    workflow_id: null,
    workflow_run_id: null,
  stage_id: null,
    position: number,
    start_date: null,
    due_date: null,
    metadata: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

describe("layoutIssueGraph", () => {
  it("places descendants in columns to the right of their parent", () => {
    const positions = layoutIssueGraph([
      issue("root", 1),
      issue("child", 2, "root"),
      issue("grandchild", 3, "child"),
    ]);

    expect(positions.get("child")!.x).toBeGreaterThan(
      positions.get("root")!.x + ISSUE_GRAPH_NODE_WIDTH,
    );
    expect(positions.get("grandchild")!.x).toBeGreaterThan(
      positions.get("child")!.x + ISSUE_GRAPH_NODE_WIDTH,
    );
  });

  it("treats an issue with a missing parent as a root", () => {
    const positions = layoutIssueGraph([issue("orphan", 1, "missing")]);
    expect(positions.get("orphan")).toEqual({ x: 0, y: 0 });
  });

  it("still positions every node when legacy data contains a cycle", () => {
    const positions = layoutIssueGraph([
      issue("a", 1, "b"),
      issue("b", 2, "a"),
    ]);
    expect([...positions.keys()].sort()).toEqual(["a", "b"]);
  });
});
