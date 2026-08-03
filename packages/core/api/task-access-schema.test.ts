import { describe, expect, it } from "vitest";

import { TaskAccessSnapshotSchema } from "./schemas";

describe("TaskAccessSnapshotSchema", () => {
  it("accepts additive diagnostics from the operator contract", () => {
    const parsed = TaskAccessSnapshotSchema.parse({
      enforcement_enabled: false,
      task_id: "task-1",
      agent_id: "agent-1",
      allowed_tools: [],
      issued_at: "2026-08-02T20:00:00Z",
      expires_at: "2026-08-02T21:00:00Z",
      status: "active",
      diagnostics: [{
        code: "task_empty",
        state: "empty",
        title: "No task capabilities",
        message: "No capabilities were frozen for this run.",
        source_policy: "Task Mandate",
        recovery_action: "Check Runtime diagnostics.",
      }],
    });
    expect(parsed.diagnostics[0]?.source_policy).toBe("Task Mandate");
  });

  it("rejects malformed capability names so the client can use its safe fallback", () => {
    expect(() => TaskAccessSnapshotSchema.parse({
      enforcement_enabled: false,
      task_id: "task-1",
      agent_id: "agent-1",
      allowed_tools: [42],
      issued_at: "2026-08-02T20:00:00Z",
      expires_at: "2026-08-02T21:00:00Z",
      status: "active",
    })).toThrow();
  });
});
