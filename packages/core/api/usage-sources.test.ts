// @vitest-environment node
import { describe, expect, it } from "vitest";
import { AgentTaskSchema } from "./schemas";

describe("run accounting source wire compatibility", () => {
  it.each([undefined, null, 1, "final_model_usage", ["final_model_usage", 1]].map((value) => ({ value })))("isolates malformed sources $value", ({ value }) => {
    const task = AgentTaskSchema.parse({ id: "run", status: "completed", usage_sources: value });
    expect(task.id).toBe("run");
    expect(task.status).toBe("completed");
    expect(task.usage_sources).toBeUndefined();
  });
  it.each([[], ["none"], ["assistant_fallback", "final_model_usage"], ["future_source"]].map((sources) => ({ sources })))("preserves source evidence $sources", ({ sources }) => {
    expect(AgentTaskSchema.parse({ id: "run", usage_sources: sources }).usage_sources).toEqual(sources);
  });
});
