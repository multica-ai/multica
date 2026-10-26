// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import { AgentRuntimeSchema, EMPTY_RUNTIME } from "./schema";

describe("runtime sharing response contract", () => {
  it("preserves grants from a new server and defaults missing grants to empty", () => {
    const member = "11111111-1111-4111-8111-111111111111";
    expect(AgentRuntimeSchema.parse({ id: "r", workspace_id: "w", shared_with: [member] }).shared_with).toEqual([member]);
    expect(AgentRuntimeSchema.parse({ id: "r", workspace_id: "w" }).shared_with).toEqual([]);
  });
  it.each([null, "all", [123], ["not-a-uuid"]])("fails closed for malformed grants: %j", (shared_with) => {
    expect(AgentRuntimeSchema.parse({ id: "r", workspace_id: "w", shared_with }).shared_with).toEqual([]);
  });
  it("falls back safely for a malformed runtime", () => {
    expect(parseWithFallback({ id: 123 }, AgentRuntimeSchema, EMPTY_RUNTIME, { endpoint: "test" })).toEqual(EMPTY_RUNTIME);
  });
});
