import { describe, expect, it } from "vitest";
import type {
  IssueWorkflowStatusNode,
  IssueTableGroupDescriptor,
} from "@multica/core/types";
import { buildWorkflowStatusGroups } from "./workflow-status-groups";

function node(
  id: string,
  workflowId: string,
  name: string,
  position: number,
): IssueWorkflowStatusNode {
  return {
    id,
    workflow_id: workflowId,
    legacy_status_key: "todo",
    spec_key: name.toLowerCase().replaceAll(" ", "_"),
    name,
    description: "",
    color: "#2563eb",
    position,
    phase: "unstarted",
    outcome: null,
    entry_policy: {
      assignee: { type: "keep" },
      executor: { type: "none" },
      instructions: "",
      advance: "human_confirms",
    },
    entry_policy_revision: 1,
    archived_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

describe("buildWorkflowStatusGroups", () => {
  it("preserves empty current nodes and distinct historical nodes with the same name", () => {
    const statuses = [
      node("current-spec", "current", "Technical Spec", 0),
      node("current-implementation", "current", "Implementation", 1),
    ];
    const descriptors: IssueTableGroupDescriptor[] = [
      {
        key: "workflow_status:current-implementation",
        value: {
          kind: "workflow_status",
          workflow_id: "current",
          workflow_status_id: "current-implementation",
          status: "todo",
          name: "Implementation",
          color: "#2563eb",
          position: 1,
        },
        count: 3,
      },
      {
        key: "workflow_status:historical-implementation",
        value: {
          kind: "workflow_status",
          workflow_id: "historical",
          workflow_status_id: "historical-implementation",
          status: "todo",
          name: "Implementation",
          color: "#7c3aed",
          position: 1,
          archived: true,
        },
        count: 1,
      },
    ];

    const groups = buildWorkflowStatusGroups(statuses, descriptors);
    expect(groups.map((group) => group.id)).toEqual([
      "workflow_status:current-spec",
      "workflow_status:current-implementation",
      "workflow_status:historical-implementation",
    ]);
    expect(groups[0]).toMatchObject({ title: "Technical Spec", createData: { workflow_status_id: "current-spec" } });
    expect(groups[1]).toMatchObject({
      title: "Implementation",
      totalCount: 3,
    });
    expect(groups[2]).toMatchObject({
      title: "Implementation",
      workflowStatusArchived: true,
      totalCount: 1,
    });
    expect(groups[2]?.createData).toBeUndefined();
  });
});
