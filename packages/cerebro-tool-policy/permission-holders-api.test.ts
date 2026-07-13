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

import { getPermissionHolders } from "./api";

beforeEach(() => {
  mockCerebroRequest.mockReset();
});

// Enforces the API Response Compatibility rule (FIR-3091 punkt 8): a malformed or
// partial holders response must downgrade to a safe, not-enforced fallback so the
// detail page renders "no holders" rather than throwing into the UI.
describe("permission-holders api compatibility", () => {
  it("returns an empty, not-enforced result on a garbage response", async () => {
    mockCerebroRequest.mockResolvedValueOnce({ unexpected: true });
    const res = await getPermissionHolders("ws", "tools:Bash");
    expect(res.enforced).toBe(false);
    expect(res.holders).toEqual([]);
  });

  it("returns an empty result when the body is null", async () => {
    mockCerebroRequest.mockResolvedValueOnce(null);
    const res = await getPermissionHolders("ws", "tools:Bash");
    expect(res.holders).toEqual([]);
  });

  it("drops holders to [] when holders is the wrong type", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      tool_key: "tools:Bash",
      enforced: true,
      holders: "nope",
    });
    const res = await getPermissionHolders("ws", "tools:Bash");
    expect(res.holders).toEqual([]);
  });

  it("fills defaults for partial holder rows", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      tool_key: "tools:Bash",
      enforced: true,
      holders: [{ layer: "agent", subject_id: "a1" }],
    });
    const res = await getPermissionHolders("ws", "tools:Bash");
    expect(res.enforced).toBe(true);
    expect(res.holders).toHaveLength(1);
    const row = res.holders[0]!;
    expect(row.layer).toBe("agent");
    expect(row.subject_id).toBe("a1");
    expect(row.resource_pattern).toBe("");
    expect(row.setting).toBe("");
  });

  it("passes the tool_key through as a query param", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      tool_key: "tools:Web fetch",
      enforced: false,
      holders: [],
    });
    await getPermissionHolders("ws", "tools:Web fetch");
    expect(mockCerebroRequest).toHaveBeenCalledWith(
      "/api/workspaces/ws/tool-policy/holders?tool_key=tools%3AWeb%20fetch",
    );
  });
});
