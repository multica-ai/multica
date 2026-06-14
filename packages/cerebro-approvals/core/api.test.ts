import { beforeEach, describe, expect, it, vi } from "vitest";

const mockCerebroRequest = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return {
    ...actual,
    api: { cerebroRequest: mockCerebroRequest },
  };
});

import { approveApproval, fetchApproval, fetchApprovals } from "./api";

beforeEach(() => {
  mockCerebroRequest.mockReset();
});

// These tests enforce the API Response Compatibility rule: malformed/partial
// backend responses must downgrade to a safe fallback, never throw into the UI.
describe("approvals api compatibility", () => {
  it("returns the empty page when the list response is garbage", async () => {
    mockCerebroRequest.mockResolvedValueOnce({ unexpected: true });
    const res = await fetchApprovals("ws", { status: null, limit: 50, offset: 0 });
    expect(res.approvals).toEqual([]);
    expect(res.pending).toBe(0);
  });

  it("fills defaults for missing fields on list rows", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      approvals: [{ id: "a1", capability: "issue.delete" }],
      total: 1,
      pending: 1,
    });
    const res = await fetchApprovals("ws", { status: "pending", limit: 50, offset: 0 });
    expect(res.approvals).toHaveLength(1);
    const row = res.approvals[0]!;
    expect(row.id).toBe("a1");
    expect(row.status).toBe("pending");
    expect(row.matched_grant_ids).toEqual([]);
    expect(row.agent_id).toBeNull();
  });

  it("returns null when a single approval response is null", async () => {
    mockCerebroRequest.mockResolvedValueOnce(null);
    const res = await fetchApproval("ws", "missing");
    expect(res).toBeNull();
  });

  it("tolerates a partial response from approve()", async () => {
    mockCerebroRequest.mockResolvedValueOnce({ id: "a1", status: "approved" });
    const res = await approveApproval("ws", "a1", { note: "ok" });
    expect(res?.status).toBe("approved");
    expect(res?.capability).toBe("");
  });
});
