// @vitest-environment node
import { describe, expect, it } from "vitest";
import { CodeGraphRepositorySchema } from "./schemas";

describe("CodeGraph repository details", () => {
  it("accepts older responses without assignments", () => {
    expect(CodeGraphRepositorySchema.parse({ id: "repo" }).agents).toEqual([]);
  });
  it("validates assigned agent records", () => {
    const agent = { id: "agent", name: "Reviewer", created_at: "2026-09-21T00:00:00Z" };
    expect(CodeGraphRepositorySchema.parse({ agents: [agent] }).agents).toEqual([agent]);
    expect(CodeGraphRepositorySchema.safeParse({ agents: [{ id: 42 }] }).success).toBe(false);
  });
});
