// @vitest-environment node
import { describe, expect, it } from "vitest";
import { fixtureControlInput } from "./fixture-control.js";

const base = {
  environmentId: "00000000-0000-4000-8000-000000000000",
  conversationId: "conversation",
  expectedRequestId: "request",
};
const start = { id: "call", name: "mcp__example__search", input: { query: "mock" } };

describe("fixture control input", () => {
  it("accepts tool calls and JSON results alongside existing text actions", () => {
    for (const input of [
      { action: "inspect", expectedRequestId: undefined },
      { action: "chunk", text: "Still working" },
      { action: "release", text: "Done" },
      { action: "fail", text: "Failed" },
      { action: "tool_start", tool: start },
      { action: "tool_result", tool: { id: "call", output: { matches: 2 } } },
      { action: "tool_result", tool: { id: "call", output: null } },
      { action: "tool_result", tool: { id: "call", output: "failure", isError: true } },
    ]) expect(fixtureControlInput.safeParse({ ...base, ...input }).success).toBe(true);
  });

  it("rejects missing request identity, mismatched tool payloads and ambiguous text", () => {
    for (const input of [
      { action: "tool_start", tool: start, expectedRequestId: undefined },
      { action: "tool_start" },
      { action: "tool_start", tool: { id: "call", name: "Read", input: [] } },
      { action: "tool_start", tool: { id: "call", output: "wrong" } },
      { action: "tool_result", tool: start },
      { action: "tool_result", tool: { id: "call" } },
      { action: "tool_result", tool: { id: "call", output: "x", isError: "true" } },
      { action: "tool_start", tool: start, text: "ambiguous" },
      { action: "release", tool: start },
    ]) expect(fixtureControlInput.safeParse({ ...base, ...input }).success).toBe(false);
  });
});
