// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  createWorkflowDraft,
  moveWorkflowStatus,
  workflowFromDefinition,
  workflowProblems,
  workflowToSpec,
  removeWorkflowStatus,
} from "./workflow";
import type { IssueWorkflowResponse } from "../types";

describe("workflow drafts", () => {
  it("materializes order as explicit handoffs without running or assigning an agent", () => {
    const draft = createWorkflowDraft(["Ready", "Work", "Review", "Done"]);
    const reordered = moveWorkflowStatus(draft, 2, 1);
    expect(reordered.initialKey).toBe(draft.initialKey);
    expect(reordered.statuses.map((s) => s.policy.next_status_key)).toEqual([
      "status_3",
      "status_2",
      "status_4",
      "",
    ]);
    expect(draft.statuses[0]?.policy.next_status_key).toBe("status_2");
    expect(
      reordered.statuses.every((s) => s.policy.executor.type === "none"),
    ).toBe(true);
  });

  it("preserves stable keys, initial identity, and explicit nonsequential links on import", () => {
    const draft = createWorkflowDraft(["Ready", "Review", "Work", "Done"]);
    draft.statuses[0]!.policy.next_status_key = "status_3";
    const definition = {
      mode: "custom",
      workflow: { initial_status_id: "id_2" },
      statuses: draft.statuses.map((s, i) => ({
        ...s,
        id: `id_${i}`,
        spec_key: s.key,
        entry_policy: s.policy,
        position: i,
        archived_at: null,
      })),
    } as unknown as IssueWorkflowResponse;
    const copied = workflowFromDefinition(definition);
    expect(copied.initialKey).toBe("status_3");
    expect(copied.statuses[0]?.policy.next_status_key).toBe("status_3");
    copied.statuses[0]!.policy.next_status_key = "status_4";
    expect(draft.statuses[0]?.policy.next_status_key).toBe("status_3");
    expect(workflowToSpec(copied, "New project").statuses[0]?.key).toBe(
      "status_1",
    );
  });

  it("repairs incoming links when removing a status while retaining unrelated links", () => {
    const draft = createWorkflowDraft(["Ready", "Work", "Review", "Done"]);
    const next = removeWorkflowStatus(draft, "status_2");
    expect(next.statuses[0]?.policy.next_status_key).toBe("status_3");
    expect(next.statuses[1]?.policy.next_status_key).toBe("status_4");
    expect(removeWorkflowStatus(draft, draft.initialKey)).toBe(draft);
  });

  it("blocks unresolved participants and missing instructions in automated templates", () => {
    const draft = createWorkflowDraft(["Ready", "Work", "Done"]);
    draft.statuses[1]!.policy.executor = { type: "agent", id: "" };
    draft.statuses[1]!.policy.assignee = { type: "agent", id: "" };
    expect(workflowProblems(draft).map((p) => p.problem)).toEqual([
      "executor",
      "instructions",
    ]);
    draft.statuses[1]!.policy.executor = { type: "agent", id: "agent" };
    draft.statuses[1]!.policy.assignee = { type: "agent", id: "agent" };
    draft.statuses[1]!.policy.instructions = "Implement and report";
    expect(workflowProblems(draft)).toEqual([]);
  });

  it("rejects ambiguous status names, stale handoff targets and a missing starting status", () => {
    const draft = createWorkflowDraft([" Review ", "review"]);
    draft.initialKey = "gone";
    draft.statuses[0]!.policy.next_status_key = "gone";
    expect(workflowProblems(draft).map((p) => p.problem)).toEqual([
      "initial",
      "next",
      "duplicate",
    ]);
  });
});
