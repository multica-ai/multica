// @vitest-environment jsdom
// FIR-1645 — the Archived block: folds by default, searches/sorts/groups its
// archived entries, and gives inbox-message rows two restore actions
// (unarchive, and unarchive + mark unread) while chat rows get only unarchive.
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { InboxItem, ChatSession } from "@multica/core/types";
import type { DynInboxEntry } from "../section-filter";
import type { InboxSectionConfig } from "../layout";
import { ArchivedInboxBlock } from "./archived-inbox-block";

const { unarchiveMock, markUnreadMock } = vi.hoisted(() => ({
  unarchiveMock: vi.fn(),
  markUnreadMock: vi.fn(),
}));

afterEach(() => {
  cleanup();
  unarchiveMock.mockReset();
  markUnreadMock.mockReset();
  mockEntries = [];
});

let mockEntries: DynInboxEntry[] = [];
vi.mock("../use-archived-entries", () => ({
  useArchivedInboxEntries: () => ({
    entries: mockEntries,
    isLoading: false,
    hasNextPage: false,
    isFetchingNextPage: false,
    fetchNextPage: vi.fn(),
  }),
}));

// Real React context for the per-row provider (so the row stub below can read
// the second action exactly as CerebroUnarchiveAction does), plus mutation stubs.
vi.mock("@multica/cerebro-inbox", async () => {
  const React = await import("react");
  const Ctx = React.createContext<{ onUnarchiveUnread: () => void } | null>(null);
  return {
    ArchivedRowActionsProvider: ({
      value,
      children,
    }: {
      value: { onUnarchiveUnread: () => void };
      children: React.ReactNode;
    }) => React.createElement(Ctx.Provider, { value }, children),
    useArchivedRowActions: () => React.useContext(Ctx),
    useUnarchiveInbox: () => ({ mutate: unarchiveMock }),
    useMarkInboxUnread: () => ({ mutate: markUnreadMock }),
  };
});

// Stub the row to expose the two restore affordances declaratively.
vi.mock("./dynamic-inbox-row", async () => {
  const React = await import("react");
  const { useArchivedRowActions } = await import("@multica/cerebro-inbox");
  return {
    DynamicInboxRow: ({
      entry,
      onUnarchive,
    }: {
      entry: DynInboxEntry;
      onUnarchive?: () => void;
    }) => {
      const actions = useArchivedRowActions();
      const title =
        entry.kind === "notif"
          ? entry.item.title
          : entry.kind === "chat"
            ? (entry.session.title ?? entry.id)
            : entry.id;
      return React.createElement(
        "div",
        null,
        React.createElement("span", null, title),
        onUnarchive
          ? React.createElement(
              "button",
              { type: "button", "aria-label": `unarchive ${title}`, onClick: onUnarchive },
              "U",
            )
          : null,
        actions
          ? React.createElement(
              "button",
              {
                type: "button",
                "aria-label": `unarchive-unread ${title}`,
                onClick: actions.onUnarchiveUnread,
              },
              "UU",
            )
          : null,
      );
    },
  };
});

function notif(id: string, title: string): DynInboxEntry {
  return {
    kind: "notif",
    id,
    time: Number(id),
    item: { id, title } as unknown as InboxItem,
  };
}
function chat(id: string, title: string): DynInboxEntry {
  return {
    kind: "chat",
    id,
    time: Number(id),
    session: { id, title } as unknown as ChatSession,
  };
}

function section(overrides: Partial<InboxSectionConfig> = {}): InboxSectionConfig {
  return { id: "arch-1", kind: "archived", groupBy: "type", sort: "newest", ...overrides };
}

function renderBlock(extra: Partial<InboxSectionConfig> = {}) {
  return render(
    <ArchivedInboxBlock
      wsId="ws"
      section={section(extra)}
      selectedKey={null}
      onSelect={vi.fn()}
      onChange={vi.fn()}
      onRemove={vi.fn()}
    />,
  );
}

describe("ArchivedInboxBlock", () => {
  it("starts folded by default — rows are hidden until expanded", async () => {
    mockEntries = [notif("2", "Alpha"), chat("1", "Beta")];
    renderBlock({ defaultCollapsed: true });
    expect(screen.queryByText("Alpha")).toBeNull();
    // Count is still shown in the header.
    expect(screen.getByText("2")).toBeTruthy();

    await userEvent.click(screen.getByRole("button", { name: "Expand" }));
    expect(screen.getByText("Alpha")).toBeTruthy();
    expect(screen.getByText("Beta")).toBeTruthy();
  });

  it("groups by type with Issues and Chat headers", async () => {
    mockEntries = [notif("2", "Alpha"), chat("1", "Beta")];
    renderBlock({ defaultCollapsed: false, groupBy: "type" });
    expect(screen.getByText("Issues")).toBeTruthy();
    expect(screen.getByText("Chat")).toBeTruthy();
  });

  it("renders ungrouped (no type headers) when groupBy is none (FIR-1686)", async () => {
    mockEntries = [notif("2", "Alpha"), chat("1", "Beta")];
    renderBlock({ defaultCollapsed: false, groupBy: "none" });
    // Both rows show, but the type sub-headers do not.
    expect(screen.getByText("Alpha")).toBeTruthy();
    expect(screen.getByText("Beta")).toBeTruthy();
    expect(screen.queryByText("Issues")).toBeNull();
    expect(screen.queryByText("Chat")).toBeNull();
  });

  it("filters with the in-block search", async () => {
    mockEntries = [notif("2", "Alpha"), notif("1", "Gamma")];
    renderBlock({ defaultCollapsed: false });
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    await userEvent.type(screen.getByRole("searchbox", { name: "Search archived" }), "alph");
    expect(screen.getByText("Alpha")).toBeTruthy();
    expect(screen.queryByText("Gamma")).toBeNull();
  });

  it("inbox-message rows offer unarchive AND unarchive+mark-unread; chat rows only unarchive", async () => {
    mockEntries = [notif("2", "Alpha"), chat("1", "Beta")];
    renderBlock({ defaultCollapsed: false });

    await userEvent.click(screen.getByRole("button", { name: "unarchive Alpha" }));
    expect(unarchiveMock).toHaveBeenCalledTimes(1);
    expect(markUnreadMock).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("button", { name: "unarchive-unread Alpha" }));
    expect(unarchiveMock).toHaveBeenCalledTimes(2);
    expect(markUnreadMock).toHaveBeenCalledTimes(1);

    // Chat rows expose no "mark unread" affordance.
    expect(screen.queryByRole("button", { name: "unarchive-unread Beta" })).toBeNull();
  });
});
