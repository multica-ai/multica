import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import type { InboxItem } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enInbox from "../../locales/en/inbox.json";

vi.mock("../../issues/components", () => ({ StatusIcon: () => null }));
vi.mock("../../issues/components/issue-agent-activity-indicator", () => ({
  IssueAgentActivityIndicator: () => null,
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("./inbox-detail-label", () => ({ InboxDetailLabel: () => null }));

// Import after mocks.
import { InboxContextMenuProvider, type InboxRowActions } from "./inbox-context-menu";
import { InboxListItem } from "./inbox-list-item";

const TEST_RESOURCES = { en: { inbox: enInbox } };

function item(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "workspace-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: "issue-1",
    title: "Issue title",
    body: null,
    issue_status: null,
    read: true,
    archived: false,
    created_at: "2026-06-15T08:00:00Z",
    details: null,
    ...overrides,
  };
}

function wrap(ui: ReactNode) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {ui}
    </I18nProvider>
  );
}

function renderRow({
  entry = item(),
  view = "inbox" as const,
  actions = {},
}: {
  entry?: InboxItem;
  view?: "inbox" | "archived";
  actions?: Partial<InboxRowActions>;
} = {}) {
  const rowActions: InboxRowActions = {
    onMarkRead: vi.fn(),
    onMarkUnread: vi.fn(),
    onAction: vi.fn(),
    ...actions,
  };
  render(
    wrap(
      <InboxContextMenuProvider view={view} actions={rowActions}>
        <InboxListItem
          item={entry}
          view={view}
          isSelected={false}
          onClick={vi.fn()}
          onAction={vi.fn()}
        />
      </InboxContextMenuProvider>,
    ),
  );
  const row = screen.getByText(entry.title).closest("button");
  if (!row) throw new Error("inbox row button not found");
  return { rowActions, row };
}

describe("inbox row context menu", () => {
  it("offers mark-as-unread on a read row and reports the item id", async () => {
    const onMarkUnread = vi.fn();
    const { row } = renderRow({
      entry: item({ id: "inbox-7", read: true }),
      actions: { onMarkUnread },
    });

    fireEvent.contextMenu(row);
    fireEvent.click(await screen.findByText("Mark as unread"));

    expect(onMarkUnread).toHaveBeenCalledWith("inbox-7");
  });

  it("offers mark-as-read on an unread row instead", async () => {
    // One toggle, not two entries: the row is either read or unread, and
    // offering the no-op direction is noise the user has to read past.
    const { row } = renderRow({ entry: item({ read: false }) });

    fireEvent.contextMenu(row);

    expect(await screen.findByText("Mark as read")).toBeInTheDocument();
    expect(screen.queryByText("Mark as unread")).toBeNull();
  });

  it("archives from the menu in the main view", async () => {
    const onAction = vi.fn();
    const { row } = renderRow({ entry: item({ id: "inbox-3" }), actions: { onAction } });

    fireEvent.contextMenu(row);
    fireEvent.click(await screen.findByText("Archive"));

    expect(onAction).toHaveBeenCalledWith("inbox-3");
  });

  it("drops the read toggle in the archived view and offers unarchive", async () => {
    // Archived rows deliberately render as read and the unread count excludes
    // them, so a read toggle there would report success and change nothing.
    const { row } = renderRow({
      entry: item({ read: true, archived: true }),
      view: "archived",
    });

    fireEvent.contextMenu(row);

    expect(await screen.findByText("Unarchive")).toBeInTheDocument();
    expect(screen.queryByText("Mark as unread")).toBeNull();
    expect(screen.queryByText("Mark as read")).toBeNull();
  });

  it("suppresses the browser's native menu so ours is the only one", async () => {
    const { row } = renderRow();

    const event = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });
    row.dispatchEvent(event);

    expect(event.defaultPrevented).toBe(true);
  });
});
