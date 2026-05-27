import { describe, expect, it } from "vitest";
import { buildStatusPresentation } from "./presentation";
import type { CerebroStatusModel } from "./types";

function model(statuses: CerebroStatusModel["statuses"]): CerebroStatusModel {
  return {
    id: "m1",
    workspace_id: "w1",
    name: "Plan-først",
    statuses,
    project_count: 0,
    created_by_id: "",
    created_by_type: "",
    created_at: "",
    updated_at: "",
  };
}

describe("buildStatusPresentation", () => {
  it("orders base statuses by position and maps label + color", () => {
    const r = buildStatusPresentation(
      model([
        { key: "done", label: "Færdig", color: "#0a0", base_status: "done", position: 2 },
        { key: "plan", label: "Plan", color: "#00f", base_status: "todo", position: 0 },
        { key: "wip", label: "I gang", color: "#fa0", base_status: "in_progress", position: 1 },
      ]),
    );
    expect(r.orderedBaseStatuses).toEqual(["todo", "in_progress", "done"]);
    expect(r.labelByBase.todo).toBe("Plan");
    expect(r.colorByBase.in_progress).toBe("#fa0");
  });

  it("collapses duplicate base statuses to the first entry (FIR-1553 scope)", () => {
    const r = buildStatusPresentation(
      model([
        { key: "plan", label: "Plan", color: "#00f", base_status: "in_progress", position: 0 },
        { key: "wip", label: "I gang", color: "#fa0", base_status: "in_progress", position: 1 },
      ]),
    );
    expect(r.orderedBaseStatuses).toEqual(["in_progress"]);
    expect(r.labelByBase.in_progress).toBe("Plan");
  });

  it("skips unknown base statuses", () => {
    const r = buildStatusPresentation(
      model([
        { key: "x", label: "Phantom", color: "", base_status: "not_a_status", position: 0 },
        { key: "todo", label: "At gøre", color: "", base_status: "todo", position: 1 },
      ]),
    );
    expect(r.orderedBaseStatuses).toEqual(["todo"]);
  });
});
