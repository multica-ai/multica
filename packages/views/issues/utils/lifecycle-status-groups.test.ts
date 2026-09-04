import { describe, expect, it } from "vitest";
import type {
  IssueLifecycleStatusNode,
  IssueTableGroupDescriptor,
} from "@multica/core/types";
import { buildLifecycleStatusGroups } from "./lifecycle-status-groups";

function node(
  id: string,
  lifecycleId: string,
  name: string,
  position: number,
): IssueLifecycleStatusNode {
  return {
    id,
    lifecycle_id: lifecycleId,
    legacy_status_key: "todo",
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

describe("buildLifecycleStatusGroups", () => {
  it("preserves empty current nodes and distinct historical nodes with the same name", () => {
    const statuses = [
      node("current-spec", "current", "Technical Spec", 0),
      node("current-implementation", "current", "Implementation", 1),
    ];
    const descriptors: IssueTableGroupDescriptor[] = [
      {
        key: "lifecycle_status:current-implementation",
        value: {
          kind: "lifecycle_status",
          lifecycle_id: "current",
          lifecycle_status_id: "current-implementation",
          status: "todo",
          name: "Implementation",
          color: "#2563eb",
          position: 1,
        },
        count: 3,
      },
      {
        key: "lifecycle_status:historical-implementation",
        value: {
          kind: "lifecycle_status",
          lifecycle_id: "historical",
          lifecycle_status_id: "historical-implementation",
          status: "todo",
          name: "Implementation",
          color: "#7c3aed",
          position: 1,
          archived: true,
        },
        count: 1,
      },
    ];

    const groups = buildLifecycleStatusGroups(statuses, descriptors);
    expect(groups.map((group) => group.id)).toEqual([
      "lifecycle_status:current-spec",
      "lifecycle_status:current-implementation",
      "lifecycle_status:historical-implementation",
    ]);
    expect(groups[0]).toMatchObject({ title: "Technical Spec" });
    expect(groups[1]).toMatchObject({
      title: "Implementation",
      totalCount: 3,
    });
    expect(groups[2]).toMatchObject({
      title: "Implementation",
      lifecycleStatusArchived: true,
      totalCount: 1,
    });
    expect(groups[2]?.createData).toBeUndefined();
  });
});
