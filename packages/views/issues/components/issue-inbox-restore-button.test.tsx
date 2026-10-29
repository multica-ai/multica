import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import type { InboxItem } from "@multica/core/types";
import { RESOURCES } from "../../locales";
import { IssueInboxRestoreButton } from "./issue-inbox-restore-button";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));

const notification = (patch: Partial<InboxItem>) => ({ id: "item-1", issue_id: "issue-1", archived: true, ...patch }) as InboxItem;

function setup(archived: InboxItem[]) {
  let rows = archived;
  const listArchivedInboxPage = vi.fn(async (_filters: unknown, options: { groupId?: string }) => ({
    items: rows.filter((row) => row.issue_id === options.groupId),
    hasMore: false,
    nextCursor: null,
  }));
  const unarchiveInbox = vi.fn(async (id: string) => {
    rows = rows.filter((row) => row.id !== id);
    return notification({ id, archived: false });
  });
  setApiInstance({
    listArchivedInboxPage,
    unarchiveInbox,
    listInbox: async () => [],
    getInboxUnreadSummary: async () => [],
  } as unknown as ApiClient);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={RESOURCES}>
        <IssueInboxRestoreButton issueId="issue-1" />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { listArchivedInboxPage, unarchiveInbox };
}

afterEach(cleanup);

describe("IssueInboxRestoreButton", () => {
  it("stays hidden when the issue is not archived in the reader's inbox", async () => {
    const api = setup([]);
    await waitFor(() => expect(api.listArchivedInboxPage).toHaveBeenCalled());
    expect(api.listArchivedInboxPage.mock.calls[0]![1]).toMatchObject({ groupId: "issue-1" });
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("restores the archived notification and hides itself", async () => {
    const api = setup([notification({})]);
    fireEvent.click(await screen.findByRole("button", { name: "Restore to inbox" }));
    await waitFor(() => expect(api.unarchiveInbox).toHaveBeenCalledWith("item-1"));
    await waitFor(() => expect(screen.queryByRole("button")).toBeNull());
  });
});
