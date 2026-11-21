import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { toast } from "sonner";
import { I18nProvider } from "@multica/core/i18n/react";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import type { InboxItem } from "@multica/core/types";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { RESOURCES } from "../../../locales";
import { InboxRestoreMenuItem, useArchivedInboxItemId } from "../inbox-restore";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));

const notification = (patch: Partial<InboxItem>) => ({ id: "item-1", issue_id: "issue-1", archived: true, ...patch }) as InboxItem;

function Harness({ enabled }: { enabled: boolean }) {
  const itemId = useArchivedInboxItemId("issue-1", enabled);
  return (
    <DropdownMenu open>
      <DropdownMenuTrigger>more</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem>Pin to sidebar</DropdownMenuItem>
        {itemId && <InboxRestoreMenuItem itemId={itemId} Item={DropdownMenuItem} />}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** A menu that closes on a pick, unmounting its items, as the issue menus do. */
function ClosingHarness() {
  const [open, setOpen] = useState(true);
  const itemId = useArchivedInboxItemId("issue-1", true);
  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger>more</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem>Pin to sidebar</DropdownMenuItem>
        {itemId && <InboxRestoreMenuItem itemId={itemId} Item={DropdownMenuItem} />}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function setup(
  archived: InboxItem[],
  { enabled = true, closing = false, unarchive }: { enabled?: boolean; closing?: boolean; unarchive?: () => Promise<InboxItem> } = {},
) {
  let rows = archived;
  const listArchivedInboxPage = vi.fn(async (_filters: unknown, options: { groupId?: string }) => ({
    items: rows.filter((row) => row.issue_id === options.groupId),
    hasMore: false,
    nextCursor: null,
  }));
  const unarchiveInbox = vi.fn(
    unarchive ??
      (async (id: string) => {
        rows = rows.filter((row) => row.id !== id);
        return notification({ id, archived: false });
      }),
  );
  setApiInstance({
    listArchivedInboxPage,
    unarchiveInbox,
    listInbox: async () => [],
    getInboxUnreadSummary: async () => [],
  } as unknown as ApiClient);
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={RESOURCES}>
        {closing ? <ClosingHarness /> : <Harness enabled={enabled} />}
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { listArchivedInboxPage, unarchiveInbox };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("inbox restore menu entry", () => {
  it("is absent when the issue is not archived in the reader's inbox", async () => {
    const api = setup([]);
    await waitFor(() => expect(api.listArchivedInboxPage).toHaveBeenCalled());
    expect(api.listArchivedInboxPage.mock.calls[0]![1]).toMatchObject({ groupId: "issue-1" });
    await screen.findByRole("menuitem", { name: "Pin to sidebar" });
    expect(screen.queryByRole("menuitem", { name: "Restore to inbox" })).toBeNull();
  });

  it("skips the lookup when the host owns inbox actions", async () => {
    const api = setup([notification({})], { enabled: false });
    await screen.findByRole("menuitem", { name: "Pin to sidebar" });
    expect(api.listArchivedInboxPage).not.toHaveBeenCalled();
    expect(screen.queryByRole("menuitem", { name: "Restore to inbox" })).toBeNull();
  });

  it("restores the archived notification and drops the entry", async () => {
    const api = setup([notification({})]);
    fireEvent.click(await screen.findByRole("menuitem", { name: "Restore to inbox" }));
    await waitFor(() => expect(api.unarchiveInbox).toHaveBeenCalledWith("item-1"));
    await waitFor(() => expect(screen.queryByRole("menuitem", { name: "Restore to inbox" })).toBeNull());
  });

  it("still confirms the restore after the menu closes", async () => {
    let finish!: () => void;
    const api = setup([notification({})], {
      closing: true,
      unarchive: () => new Promise((resolve) => (finish = () => resolve(notification({ archived: false })))),
    });
    fireEvent.click(await screen.findByRole("menuitem", { name: "Restore to inbox" }));
    await waitFor(() => expect(screen.queryByRole("menuitem", { name: "Pin to sidebar" })).toBeNull());
    await waitFor(() => expect(api.unarchiveInbox).toHaveBeenCalled());
    await act(async () => finish());

    expect(toast.success).toHaveBeenCalledWith("Notification restored");
  });

  it("still reports a failed restore after the menu closes", async () => {
    let fail!: (err: Error) => void;
    const api = setup([notification({})], {
      closing: true,
      unarchive: () => new Promise((_resolve, reject) => (fail = reject)),
    });
    fireEvent.click(await screen.findByRole("menuitem", { name: "Restore to inbox" }));
    await waitFor(() => expect(screen.queryByRole("menuitem", { name: "Pin to sidebar" })).toBeNull());
    await waitFor(() => expect(api.unarchiveInbox).toHaveBeenCalled());
    await act(async () => fail(new Error("Restore rejected")));

    expect(toast.error).toHaveBeenCalledWith("Restore rejected");
    expect(toast.success).not.toHaveBeenCalled();
  });
});
