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

import { getPermissionChanges } from "./api";

beforeEach(() => {
  mockCerebroRequest.mockReset();
});

// Enforces the API Response Compatibility rule (FIR-3091 punkt 8 fase 2): a
// malformed or partial changes response must downgrade to an empty list so the
// Changes tab renders "no changes" rather than throwing into the UI.
describe("permission-changes api compatibility", () => {
  it("returns an empty result on a garbage response", async () => {
    mockCerebroRequest.mockResolvedValueOnce({ unexpected: true });
    const res = await getPermissionChanges("ws", "tools:Bash");
    expect(res.changes).toEqual([]);
  });

  it("returns an empty result when the body is null", async () => {
    mockCerebroRequest.mockResolvedValueOnce(null);
    const res = await getPermissionChanges("ws", "tools:Bash");
    expect(res.changes).toEqual([]);
  });

  it("drops changes to [] when changes is the wrong type", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      tool_key: "tools:Bash",
      changes: "nope",
    });
    const res = await getPermissionChanges("ws", "tools:Bash");
    expect(res.changes).toEqual([]);
  });

  it("fills defaults for partial change rows", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      tool_key: "tools:Bash",
      changes: [{ layer: "agent", subject_id: "a1", action: "set" }],
    });
    const res = await getPermissionChanges("ws", "tools:Bash");
    expect(res.changes).toHaveLength(1);
    const row = res.changes[0]!;
    expect(row.layer).toBe("agent");
    expect(row.subject_id).toBe("a1");
    expect(row.action).toBe("set");
    expect(row.old_setting).toBe("");
    expect(row.new_setting).toBe("");
    expect(row.actor_type).toBe("");
    expect(row.created_at).toBe("");
  });

  it("parses a complete change row verbatim", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      tool_key: "tools:Bash",
      changes: [
        {
          layer: "agent",
          subject_id: "a1",
          resource_pattern: "",
          action: "clear",
          old_setting: "allow",
          new_setting: "",
          actor_type: "member",
          actor_id: "u1",
          created_at: "2026-07-12T10:00:00Z",
        },
      ],
    });
    const res = await getPermissionChanges("ws", "tools:Bash");
    const row = res.changes[0]!;
    expect(row.action).toBe("clear");
    expect(row.old_setting).toBe("allow");
    expect(row.new_setting).toBe("");
    expect(row.actor_type).toBe("member");
    expect(row.actor_id).toBe("u1");
    expect(row.created_at).toBe("2026-07-12T10:00:00Z");
  });
});
