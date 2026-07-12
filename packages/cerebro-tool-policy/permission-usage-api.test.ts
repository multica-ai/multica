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

import { getPermissionUsage } from "./api";

beforeEach(() => {
  mockCerebroRequest.mockReset();
});

// Enforces the API Response Compatibility rule (FIR-3091 punkt 8 fase 3): a
// malformed or partial usage response must downgrade to an empty list so the
// Usage tab renders "no usage" rather than throwing into the UI.
describe("permission-usage api compatibility", () => {
  it("returns an empty result on a garbage response", async () => {
    mockCerebroRequest.mockResolvedValueOnce({ unexpected: true });
    const res = await getPermissionUsage("ws", "repo.checkout");
    expect(res.usage).toEqual([]);
  });

  it("returns an empty result when the body is null", async () => {
    mockCerebroRequest.mockResolvedValueOnce(null);
    const res = await getPermissionUsage("ws", "repo.checkout");
    expect(res.usage).toEqual([]);
  });

  it("drops usage to [] when usage is the wrong type", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      tool_key: "repo.checkout",
      usage: "nope",
    });
    const res = await getPermissionUsage("ws", "repo.checkout");
    expect(res.usage).toEqual([]);
  });

  it("fills defaults for partial usage rows", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      tool_key: "repo.checkout",
      usage: [{ enforcement_point: "repo_checkout", decision: "deny" }],
    });
    const res = await getPermissionUsage("ws", "repo.checkout");
    expect(res.usage).toHaveLength(1);
    const row = res.usage[0]!;
    expect(row.enforcement_point).toBe("repo_checkout");
    expect(row.decision).toBe("deny");
    expect(row.subject_type).toBe("");
    expect(row.subject_id).toBe("");
    expect(row.resource).toBe("");
    expect(row.decided_by).toBe("");
    expect(row.created_at).toBe("");
  });

  it("parses a complete usage row verbatim", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      tool_key: "repo.checkout",
      usage: [
        {
          enforcement_point: "repo_checkout",
          subject_type: "agent",
          subject_id: "a1",
          resource: "https://github.com/acme/repo",
          decision: "allow",
          decided_by: "workspace",
          created_at: "2026-07-12T14:00:00Z",
        },
      ],
    });
    const res = await getPermissionUsage("ws", "repo.checkout");
    const row = res.usage[0]!;
    expect(row.enforcement_point).toBe("repo_checkout");
    expect(row.subject_type).toBe("agent");
    expect(row.subject_id).toBe("a1");
    expect(row.resource).toBe("https://github.com/acme/repo");
    expect(row.decision).toBe("allow");
    expect(row.decided_by).toBe("workspace");
    expect(row.created_at).toBe("2026-07-12T14:00:00Z");
  });
});
