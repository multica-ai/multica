import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { toast } from "sonner";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { I18nProvider } from "@multica/core/i18n/react";
import { inboxListOptions } from "@multica/core/inbox/queries";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import type { InboxItem } from "@multica/core/types";
import { getTestResources } from "../../test/i18n";
import { useArchiveInboxBatchWithUndo, useArchiveInboxWithUndo } from "./use-leave-inbox-row";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));

const api = {
  archiveInbox: vi.fn(async (id: string) => ({ id })),
  unarchiveInbox: vi.fn(async (id: string) => ({ id })),
  listInbox: vi.fn(async () => []),
  getInboxUnreadSummary: vi.fn(async () => []),
};

function item(id: string, overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id,
    workspace_id: "ws",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: null,
    title: id,
    body: null,
    issue_status: "in_progress",
    issue_priority: "none",
    read: true,
    archived: false,
    created_at: "2026-11-10T08:00:00Z",
    details: null,
    ...overrides,
  };
}

function setup<T>(hook: () => T, items: InboxItem[] = []) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  qc.setQueryData(inboxListOptions("ws").queryKey, items);
  const resources = getTestResources();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <I18nProvider locale="en" resources={resources}>
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    </I18nProvider>
  );
  return renderHook(hook, { wrapper }).result;
}

const undoOf = () =>
  (vi.mocked(toast.success).mock.calls.at(-1)?.[1] as unknown as { action: { onClick: () => void } }).action;
const left = () => useInboxSelectionStore.getState().leftRowKeyByWorkspace.ws;

beforeEach(() => {
  vi.clearAllMocks();
  setApiInstance(api as unknown as ApiClient);
  useInboxSelectionStore.setState({ heldRows: {}, leftRowKeyByWorkspace: { ws: "issue-a" } });
});
afterEach(cleanup);

describe("useArchiveInboxWithUndo", () => {
  it("returns the row it left when undone, so it can be kept in place again", async () => {
    const archive = setup(useArchiveInboxWithUndo);
    await act(() => archive.current("n-a", "issue-a"));
    expect(left()).toBe("issue-a");

    await act(async () => undoOf().onClick());
    expect(left()).toBeUndefined();
    expect(api.unarchiveInbox).toHaveBeenCalledWith("n-a");
  });

  it("returns the row when the archive fails", async () => {
    api.archiveInbox.mockRejectedValueOnce(new Error("Offline"));
    const archive = setup(useArchiveInboxWithUndo);
    await act(() => archive.current("n-a", "issue-a"));
    expect(toast.error).toHaveBeenCalledWith("Offline");
    expect(left()).toBeUndefined();
  });
});

describe("useArchiveInboxBatchWithUndo", () => {
  const items = [
    item("n-a1", { issue_id: "issue-a" }),
    item("n-a2", { issue_id: "issue-a" }),
    item("n-b", { issue_id: "issue-b", read: false }),
    item("n-notice"),
  ];
  const isRead = (row: InboxItem) => row.read === true;

  it("says how many it archived, and its undo restores each row it took", async () => {
    const archiveBatch = setup(useArchiveInboxBatchWithUndo, items);
    await act(() => archiveBatch.current(async () => ({ count: 3 }), isRead, "Failed", "issue-a"));
    expect(toast.success).toHaveBeenCalledWith("3 notifications archived", {
      action: { label: "Undo", onClick: expect.any(Function) },
    });

    await act(async () => undoOf().onClick());
    expect(api.unarchiveInbox.mock.calls.map(([id]) => id)).toEqual(["n-a1", "n-notice"]);
    expect(left()).toBeUndefined();
  });

  it("stays quiet when there was nothing to archive", async () => {
    const archiveBatch = setup(useArchiveInboxBatchWithUndo, items);
    await act(() => archiveBatch.current(async () => ({ count: 0 }), isRead, "Failed", null));
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("reports a failure and returns the open row it left", async () => {
    const archiveBatch = setup(useArchiveInboxBatchWithUndo, items);
    await act(() => archiveBatch.current(() => Promise.reject(new Error("")), isRead, "Failed", "issue-a"));
    expect(toast.error).toHaveBeenCalledWith("Failed");
    expect(left()).toBeUndefined();
  });
});
