import { beforeEach, describe, expect, it, vi } from "vitest";

const mockCerebroRequest = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/api")>(
      "@multica/core/api",
    );
  return {
    ...actual,
    api: { cerebroRequest: mockCerebroRequest },
  };
});

import { getTestAsUserAccess, runTestAsUser } from "./api";

beforeEach(() => {
  mockCerebroRequest.mockReset();
});

// Enforces the API Response Compatibility rule (FIR-1771): a malformed or
// partial backend response must downgrade to a safe fallback, never throw.
describe("test-as-user api compatibility", () => {
  it("access defaults to false on a garbage response", async () => {
    mockCerebroRequest.mockResolvedValueOnce({ unexpected: true });
    expect(await getTestAsUserAccess("ws")).toBe(false);
  });

  it("access defaults to false when the body is null", async () => {
    mockCerebroRequest.mockResolvedValueOnce(null);
    expect(await getTestAsUserAccess("ws")).toBe(false);
  });

  it("access reads the explicit allowed flag", async () => {
    mockCerebroRequest.mockResolvedValueOnce({ allowed: true });
    expect(await getTestAsUserAccess("ws")).toBe(true);
  });

  it("resolve returns [] when tools is missing or wrong type", async () => {
    mockCerebroRequest.mockResolvedValueOnce({ tools: "nope" });
    const rows = await runTestAsUser("ws", { userId: "u", agentId: "a" });
    expect(rows).toEqual([]);
  });

  it("resolve fills defaults for partial rows", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      tools: [{ tool_key: "tools:Bash" }],
    });
    const rows = await runTestAsUser("ws", {
      userId: "u",
      agentId: "a",
      runtimeId: "r",
    });
    expect(rows).toHaveLength(1);
    const row = rows[0]!;
    expect(row.tool_key).toBe("tools:Bash");
    expect(row.effective.setting).toBe("");
    expect(row.capped_by_groups).toEqual([]);
  });
});
