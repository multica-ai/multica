// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { IssueWorkflowResponse } from "../types";
import {
  activeWorkflowStatuses,
  resolveCreateWorkflowStatus,
} from "./status-selection";

const data = {
  workflow: { id: "flow-a", initial_status_id: "ready" },
  statuses: [
    {
      id: "review",
      workflow_id: "flow-a",
      name: "Review",
      position: 1,
      legacy_status_key: "todo",
    },
    { id: "ready", workflow_id: "flow-a", name: "Ready", position: 2 },
    {
      id: "retired",
      workflow_id: "flow-a",
      archived_at: "yesterday",
      position: 0,
    },
    { id: "foreign", workflow_id: "flow-b", position: 0 },
  ],
} as unknown as IssueWorkflowResponse;

describe("create workflow status selection", () => {
  it("uses configured initial identity, not the first node or hardcoded todo", () => {
    expect(
      resolveCreateWorkflowStatus(data, "a", { projectId: "a" })?.id,
    ).toBe("ready");
    expect(activeWorkflowStatuses(data).map((node) => node.id)).toEqual([
      "review",
      "ready",
    ]);
  });
  it("preserves valid explicit column and draft selections only within their project", () => {
    expect(
      resolveCreateWorkflowStatus(data, "a", {
        projectId: "a",
        nodeId: "review",
      })?.id,
    ).toBe("review");
    expect(
      resolveCreateWorkflowStatus(data, "a", {
        projectId: "b",
        nodeId: "review",
      })?.id,
    ).toBe("ready");
    expect(
      resolveCreateWorkflowStatus(data, "a", {
        projectId: "a",
        nodeId: "retired",
      })?.id,
    ).toBe("ready");
    expect(
      resolveCreateWorkflowStatus(data, "a", {
        projectId: "a",
        nodeId: "foreign",
      })?.id,
    ).toBe("ready");
  });
  it("resolves legacy defaults only with a unique match, and never substitutes on load failure", () => {
    expect(
      resolveCreateWorkflowStatus(data, "a", {
        projectId: "a",
        legacyKey: "todo",
      })?.id,
    ).toBe("review");
    expect(
      resolveCreateWorkflowStatus(data, null, {
        projectId: "a",
        legacyKey: "todo",
      })?.id,
    ).toBe("ready");
    expect(
      resolveCreateWorkflowStatus(undefined, "a", {
        projectId: "a",
        nodeId: "review",
      }),
    ).toBeUndefined();
    expect(
      resolveCreateWorkflowStatus(
        {
          ...data,
          workflow: { ...data.workflow, initial_status_id: "retired" },
        },
        "a",
        { projectId: "a" },
      ),
    ).toBeUndefined();
  });
});
