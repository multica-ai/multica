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
  it("reorders statuses without changing their actions or initial status", () => {
    const draft = createWorkflowDraft(["Ready", "Work", "Review", "Done"]);
    draft.statuses[2]!.policy = { executor: { type: "agent", id: "reviewer" }, instructions: "Review the change" };
    const reordered = moveWorkflowStatus(draft, 2, 1);
    expect(reordered.initialKey).toBe(draft.initialKey);
    expect(reordered.statuses).toEqual([draft.statuses[0], draft.statuses[2], draft.statuses[1], draft.statuses[3]]);
    expect(draft.statuses.map((s) => s.name)).toEqual(["Ready", "Work", "Review", "Done"]);
  });

  it("preserves stable keys, initial identity, and independent actions on import", () => {
    const draft = createWorkflowDraft(["Ready", "Review", "Work", "Done"]);
    draft.statuses[0]!.policy.instructions = "Review the specification";
    draft.statuses[0]!.icon = "three_quarters";
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
    expect(workflowToSpec(copied, "New project").statuses[0]?.icon).toBe("three_quarters");
    expect(copied.statuses[0]?.policy.instructions).toBe("Review the specification");
    copied.statuses[0]!.policy.instructions = "Changed instructions";
    expect(draft.statuses[0]?.policy.instructions).toBe("Review the specification");
    expect(workflowToSpec(copied, "New project").statuses[0]?.key).toBe(
      "status_1",
    );
  });

  it("removes only the selected status and preserves the initial status", () => {
    const draft = createWorkflowDraft(["Ready", "Work", "Review", "Done"]);
    const next = removeWorkflowStatus(draft, "status_2");
    expect(next.statuses).toEqual([draft.statuses[0], draft.statuses[2], draft.statuses[3]]);
    expect(removeWorkflowStatus(draft, draft.initialKey)).toBe(draft);
  });

  it("blocks unresolved participants and missing instructions in automated statuses", () => {
    const draft = createWorkflowDraft(["Ready", "Work", "Done"]);
    draft.statuses[1]!.policy.executor = { type: "agent", id: "" };
    expect(workflowProblems(draft).map((p) => p.problem)).toEqual([
      "executor",
      "instructions",
    ]);
    draft.statuses[1]!.policy.executor = { type: "agent", id: "agent" };
    draft.statuses[1]!.policy.instructions = "Implement and report";
    expect(workflowProblems(draft)).toEqual([]);
  });

  it("rejects ambiguous status names and a missing starting status", () => {
    const draft = createWorkflowDraft([" Review ", "review"]);
    draft.initialKey = "gone";
    expect(workflowProblems(draft).map((p) => p.problem)).toEqual([
      "initial",
      "duplicate",
    ]);
  });
});
