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

import { approveApproval, fetchAllApprovals, fetchApproval, fetchApprovals } from "./api";

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
    // Human-readable enrichment fields default to null when the backend omits them.
    expect(row.requester_name).toBeNull();
    expect(row.issue_identifier).toBeNull();
    expect(row.issue_title).toBeNull();
    expect(row.triggered_by_name).toBeNull();
    expect(row.triggered_by_id).toBeNull();
    expect(row.task_id).toBeNull();
    expect(row.chat_session_id).toBeNull();
    expect(row.trigger_comment_id).toBeNull();
    expect(row.surface).toBeNull();
  });

  it("surfaces the enrichment fields (names + issue) when present", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      approvals: [
        {
          id: "a1",
          capability: "draft_reply",
          requester_type: "agent",
          requester_id: "11111111-1111-1111-1111-111111111111",
          requester_name: "Sara CTO Bot",
          agent_name: "Sara CTO Bot",
          issue_id: "22222222-2222-2222-2222-222222222222",
          issue_identifier: "TECH-3498",
          issue_title: "Human-readable approvals inbox",
          triggered_by_id: "33333333-3333-3333-3333-333333333333",
          triggered_by_name: "Jesper Hvejsel",
        },
      ],
      total: 1,
      pending: 1,
    });
    const res = await fetchApprovals("ws", { status: "pending", limit: 50, offset: 0 });
    const row = res.approvals[0]!;
    expect(row.requester_name).toBe("Sara CTO Bot");
    expect(row.issue_identifier).toBe("TECH-3498");
    expect(row.issue_title).toBe("Human-readable approvals inbox");
    expect(row.triggered_by_name).toBe("Jesper Hvejsel");
  });

  it("returns null when a single approval response is null", async () => {
    mockCerebroRequest.mockResolvedValueOnce(null);
    const res = await fetchApproval("ws", "missing");
    expect(res).toBeNull();
  });

  it("sends server-side origin filters for inline approval reads", async () => {
    mockCerebroRequest.mockResolvedValueOnce({ approvals: [], total: 0, pending: 0 });
    await fetchApprovals("ws", {
      status: "pending",
      limit: 50,
      offset: 0,
      origin: {
        task_id: "task-1",
        issue_id: "issue-1",
        chat_session_id: "chat-1",
        trigger_comment_id: "comment-1",
        surface: "chat",
      },
    });
    expect(mockCerebroRequest).toHaveBeenCalledWith(
      "/api/workspaces/ws/approvals?status=pending&limit=50&offset=0&task_id=task-1&issue_id=issue-1&chat_session_id=chat-1&trigger_comment_id=comment-1&surface=chat",
    );
  });

  it("loads every page for a busy inline approval origin", async () => {
    const firstPage = Array.from({ length: 200 }, (_, index) => ({
      id: `approval-${index}`,
      status: "pending",
    }));
    mockCerebroRequest
      .mockResolvedValueOnce({ approvals: firstPage, total: 201, pending: 201 })
      .mockResolvedValueOnce({
        approvals: [{ id: "approval-200", status: "pending" }],
        total: 201,
        pending: 201,
      });

    const res = await fetchAllApprovals("ws", {
      status: null,
      origin: { issue_id: "issue-1", surface: "issue" },
    });

    expect(res.approvals).toHaveLength(201);
    expect(mockCerebroRequest).toHaveBeenNthCalledWith(
      1,
      "/api/workspaces/ws/approvals?limit=200&offset=0&issue_id=issue-1&surface=issue",
    );
    expect(mockCerebroRequest).toHaveBeenNthCalledWith(
      2,
      "/api/workspaces/ws/approvals?limit=200&offset=200&issue_id=issue-1&surface=issue",
    );
  });

  it("tolerates a partial response from approve()", async () => {
    mockCerebroRequest.mockResolvedValueOnce({ id: "a1", status: "approved" });
    const res = await approveApproval("ws", "a1", { note: "ok" });
    expect(res?.status).toBe("approved");
    expect(res?.capability).toBe("");
  });
});
