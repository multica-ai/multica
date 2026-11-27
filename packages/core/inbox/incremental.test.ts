// @vitest-environment node
import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { InboxItem } from "../types";
import { INBOX_REFRESH_INTERVAL_MS, coalescedRefresh, inboxRowFromPush } from "./incremental";

function pushed(overrides: Partial<InboxItem> = {}): InboxItem {
  // What inbox:new carries: the notification, without the issue's fields.
  return {
    id: "n2",
    workspace_id: "ws-1",
    recipient_type: "member",
    recipient_id: "user-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: "issue-a",
    title: "Comment",
    body: "hello",
    read: false,
    archived: false,
    created_at: "2026-11-24T10:00:00Z",
    details: { comment_id: "c2" },
    ...overrides,
  } as InboxItem;
}

const listed: InboxItem = {
  ...pushed({ id: "n1", created_at: "2026-11-24T09:00:00Z", details: null }),
  issue_identifier: "MUL-1",
  issue_status: "in_progress",
  issue_priority: "high",
  issue_project_id: "project-1",
  issue_assignee_type: "member",
  issue_assignee_id: "user-2",
  issue_due_date: null,
  issue_label_ids: ["label-1"],
  snoozed_until: null,
  snoozed_at: null,
};

describe("inboxRowFromPush", () => {
  it("takes the issue fields from another row of the issue", () => {
    expect(inboxRowFromPush(pushed(), [listed])).toEqual({
      ...pushed(),
      issue_identifier: "MUL-1",
      issue_status: "in_progress",
      issue_priority: "high",
      issue_project_id: "project-1",
      issue_assignee_type: "member",
      issue_assignee_id: "user-2",
      issue_due_date: null,
      issue_label_ids: ["label-1"],
    });
  });

  it("falls back to a row of the issue held elsewhere, such as the archive", () => {
    expect(inboxRowFromPush(pushed(), [], [{ ...listed, archived: true, issue_identifier: "MUL-9" }]))
      .toMatchObject({ id: "n2", issue_identifier: "MUL-9" });
  });

  it("takes the status from the issue's listed row, not from a push written earlier", () => {
    expect(inboxRowFromPush(pushed({ issue_status: "todo" }), [listed])?.issue_status).toBe("in_progress");
  });

  it("knows nothing about an issue without another row", () => {
    expect(inboxRowFromPush(pushed(), [{ ...listed, issue_id: "issue-b" }])).toBeNull();
  });

  it("does not write an issue-less notice, which the list hides from a guest", () => {
    expect(inboxRowFromPush(pushed({ issue_id: null, type: "project_owner_joined" }), [listed])).toBeNull();
  });

  it("does not order two rows of one issue from the same second", () => {
    // Pushes can arrive out of order, and times are to the second: the newer
    // row cannot be told apart, so the server's order is needed.
    const sameSecond = { ...listed, id: "n1", created_at: "2026-11-24T10:00:00Z" };
    expect(inboxRowFromPush(pushed({ created_at: "2026-11-24T10:00:00Z" }), [sameSecond])).toBeNull();
    // A row of that second held elsewhere is not in the list it would sort in.
    expect(inboxRowFromPush(pushed({ created_at: "2026-11-24T10:00:00Z" }), [listed], [sameSecond])).not.toBeNull();
  });

  it.each([
    ["id", { id: "" }],
    ["created_at", { created_at: "" }],
    ["type", { type: undefined }],
    ["title", { title: undefined }],
    ["read", { read: undefined }],
    ["archived", { archived: undefined }],
  ])("does not write an item without a usable %s, which the list reads on every row", (_field, missing) => {
    expect(inboxRowFromPush(pushed(missing as Partial<InboxItem>), [listed])).toBeNull();
  });

  it("does not write an archived row into the active list", () => {
    expect(inboxRowFromPush(pushed({ archived: true }), [listed])).toBeNull();
  });
});

describe("coalescedRefresh", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  const key = ["inbox", "ws-1"] as const;

  it("starts at once, keeps one run out, and merges what arrives meanwhile into one", async () => {
    const qc = new QueryClient();
    const releases: Array<() => void> = [];
    let running = 0;
    let maxRunning = 0;
    const refresh = vi.fn(() => {
      running += 1;
      maxRunning = Math.max(maxRunning, running);
      return new Promise<void>((resolve) =>
        releases.push(() => {
          running -= 1;
          resolve();
        }),
      );
    });

    void coalescedRefresh(qc, key, refresh);
    expect(refresh).toHaveBeenCalledTimes(1);
    for (let i = 0; i < 10; i++) void coalescedRefresh(qc, key, refresh);
    expect(refresh).toHaveBeenCalledTimes(1);

    releases.shift()!();
    await vi.advanceTimersByTimeAsync(INBOX_REFRESH_INTERVAL_MS);
    expect(refresh).toHaveBeenCalledTimes(2);
    releases.shift()!();
    await vi.advanceTimersByTimeAsync(INBOX_REFRESH_INTERVAL_MS * 5);
    expect(refresh).toHaveBeenCalledTimes(2);
    expect(maxRunning).toBe(1);
  });

  it("waits out the interval after a run that finished quickly", async () => {
    const qc = new QueryClient();
    const refresh = vi.fn(async () => {});
    await coalescedRefresh(qc, key, refresh);
    const second = coalescedRefresh(qc, key, refresh);
    await vi.advanceTimersByTimeAsync(INBOX_REFRESH_INTERVAL_MS - 1);
    expect(refresh).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    await second;
    expect(refresh).toHaveBeenCalledTimes(2);
  });

  it("keeps clients and keys apart", () => {
    const refresh = vi.fn(async () => {});
    const qc = new QueryClient();
    void coalescedRefresh(qc, key, refresh);
    void coalescedRefresh(qc, ["inbox", "unread-summary"], refresh);
    void coalescedRefresh(new QueryClient(), key, refresh);
    expect(refresh).toHaveBeenCalledTimes(3);
  });

  it("runs again after a refresh that failed", async () => {
    const qc = new QueryClient();
    const refresh = vi.fn().mockRejectedValueOnce(new Error("offline")).mockResolvedValue(undefined);
    await expect(coalescedRefresh(qc, key, refresh)).resolves.toBeUndefined();
    const again = coalescedRefresh(qc, key, refresh);
    await vi.advanceTimersByTimeAsync(INBOX_REFRESH_INTERVAL_MS);
    await again;
    expect(refresh).toHaveBeenCalledTimes(2);
  });

  it("follows a read already on the wire with one refresh that every call meanwhile shares", async () => {
    // A first load takes 250ms; notifications arrive at 10ms and 20ms.
    const qc = new QueryClient();
    void qc.fetchQuery({
      queryKey: [...key, "list"],
      queryFn: () => new Promise((resolve) => setTimeout(() => resolve([]), 250)),
    });
    const refresh = vi.fn(async () => {});

    await vi.advanceTimersByTimeAsync(10);
    const first = coalescedRefresh(qc, key, refresh);
    await vi.advanceTimersByTimeAsync(10);
    const second = coalescedRefresh(qc, key, refresh);
    expect(second).toBe(first);
    expect(refresh).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(INBOX_REFRESH_INTERVAL_MS * 5);
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("does not wait on a read paused until the window is focused again", async () => {
    // The desktop dock badge reads the summary while the window is hidden; a
    // retry there pauses until focus.
    const qc = new QueryClient();
    const query = qc.getQueryCache().build(qc, { queryKey: [...key, "list"] });
    query.setState({ fetchStatus: "paused" });
    const refresh = vi.fn(async () => {});

    void coalescedRefresh(qc, key, refresh);

    expect(refresh).toHaveBeenCalledTimes(1);
  });
});
