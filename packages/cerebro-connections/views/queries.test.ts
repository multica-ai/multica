import { describe, expect, it } from "vitest";

import { TestResultSchema } from "./queries";

// TECH-3410: the connection test result now carries discovered API endpoints.
// Per CLAUDE.md "API Response Compatibility", the schema must survive drift —
// missing, extra, and wrong-typed fields must not throw into the UI.
describe("TestResultSchema — endpoints", () => {
  it("parses a result with discovered endpoints", () => {
    const parsed = TestResultSchema.parse({
      reachable: true,
      status_code: 200,
      endpoints: [
        { path: "/orders", methods: ["GET", "POST"] },
        { path: "/orders/{id}", methods: ["GET", "DELETE"] },
      ],
    });
    expect(parsed.reachable).toBe(true);
    expect(parsed.endpoints).toHaveLength(2);
    expect(parsed.endpoints?.[0]?.methods).toEqual(["GET", "POST"]);
  });

  it("defaults methods to [] when the server omits them", () => {
    const parsed = TestResultSchema.parse({
      reachable: true,
      endpoints: [{ path: "/health" }],
    });
    expect(parsed.endpoints?.[0]?.methods).toEqual([]);
  });

  it("tolerates a result with no endpoints field (MCP connection)", () => {
    const parsed = TestResultSchema.parse({ reachable: true, tools: [{ name: "draft_reply" }] });
    expect(parsed.endpoints).toBeUndefined();
    expect(parsed.tools).toHaveLength(1);
  });

  it("rejects a malformed endpoints entry so parseWithFallback can fall back", () => {
    // A path that is not a string is a contract violation — the schema must
    // throw here (parseWithFallback catches it upstream and returns the
    // fallback rather than white-screening).
    expect(() =>
      TestResultSchema.parse({ reachable: true, endpoints: [{ path: 123, methods: ["GET"] }] }),
    ).toThrow();
  });
});
