// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { STATUS_ORDER } from "@multica/core/issues/config";
import {
  type InboxPriorityFilterSupport,
  useInboxFilterStore,
} from "@multica/core/inbox/filter-store";
import type { InboxItem, IssueStatusEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { InboxFilterMenu } from "./inbox-filter-menu";

// The directory hook resolves the current workspace through the route, which
// this leaf render has none of — stubbed the same way the sibling
// inbox-detail-label suite stubs it.
const ACTOR_NAMES: Record<string, string> = { alice: "Alice", bob: "Bob" };
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (type: string, id: string) =>
      type === "system" ? "Multica" : (ACTOR_NAMES[id] ?? "Unknown"),
    getActorInitials: () => "??",
    getActorAvatarUrl: () => null,
  }),
}));

function statusEntry(category: (typeof STATUS_ORDER)[number]): IssueStatusEntry {
  return {
    id: `status-${category}`,
    workspace_id: "ws-1",
    key: category,
    name: category,
    description: "",
    category,
    color: "#888888",
    is_system: true,
    position: 0,
    archived_at: null,
    created_at: "",
    updated_at: "",
  };
}

function item(
  id: string,
  issueStatus: InboxItem["issue_status"],
  issuePriority: InboxItem["issue_priority"],
  overrides: Partial<InboxItem> = {},
): InboxItem {
  return {
    id,
    workspace_id: "ws-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: null,
    actor_id: null,
    type: "mentioned",
    severity: "info",
    issue_id: `issue-${id}`,
    title: id,
    body: null,
    issue_status: issueStatus,
    issue_priority: issuePriority,
    read: false,
    archived: false,
    created_at: "2026-08-24T00:00:00Z",
    details: null,
    ...overrides,
  };
}

const ITEMS = [
  item("todo-high", "todo", "high"),
  item("done-low", "done", "low"),
];

function renderMenu({
  items = ITEMS,
  priorityFilterSupport = "supported",
}: {
  items?: InboxItem[];
  priorityFilterSupport?: InboxPriorityFilterSupport;
} = {}) {
  setApiInstance({
    listIssueStatuses: async () => ({
      statuses: STATUS_ORDER.map(statusEntry),
      categories: [],
      total: STATUS_ORDER.length,
    }),
  } as unknown as ApiClient);
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <InboxFilterMenu
        wsId="ws-1"
        items={items}
        priorityFilterSupport={priorityFilterSupport}
      />
    </QueryClientProvider>,
  );
}

async function openSubmenu(name: "From" | "Status" | "Priority") {
  fireEvent.click(screen.getByRole("button", { name: "Filter inbox" }));
  const trigger = await screen.findByRole("menuitem", {
    name: new RegExp(`^${name}`),
  });
  fireEvent.click(trigger);
}

beforeEach(() => {
  useInboxFilterStore.setState({ filtersByWorkspace: {} });
});

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
});

describe("InboxFilterMenu", () => {
  it("selects a status and exposes the active count on the trigger", async () => {
    renderMenu();
    await openSubmenu("Status");
    const todo = await screen.findByRole("menuitemcheckbox", {
      name: /Todo.*1 notification/,
    });

    fireEvent.click(todo);

    expect(
      useInboxFilterStore.getState().filtersByWorkspace["ws-1"]?.statuses,
    ).toEqual(["todo"]);
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "1 active filter" }),
      ).toHaveTextContent("1"),
    );
  });

  it("selects a priority", async () => {
    renderMenu();
    await openSubmenu("Priority");
    const high = await screen.findByRole("menuitemcheckbox", {
      name: /High.*1 notification/,
    });

    fireEvent.click(high);

    expect(
      useInboxFilterStore.getState().filtersByWorkspace["ws-1"]?.priorities,
    ).toEqual(["high"]);
  });

  it("clears every active filter from the root menu", async () => {
    const store = useInboxFilterStore.getState();
    store.toggleStatusFilter("ws-1", "todo");
    store.togglePriorityFilter("ws-1", "high");
    renderMenu();

    fireEvent.click(screen.getByRole("button", { name: "2 active filters" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Clear filters" }));

    expect(
      useInboxFilterStore.getState().filtersByWorkspace["ws-1"],
    ).toBeUndefined();
  });

  it("offers only the actors the visible rows carry", async () => {
    renderMenu({
      items: [
        item("from-alice", "todo", "high", {
          actor_type: "member",
          actor_id: "alice",
        }),
        item("from-bob", "done", "low", {
          actor_type: "agent",
          actor_id: "bob",
        }),
      ],
    });

    await openSubmenu("From");
    const alice = await screen.findByRole("menuitemcheckbox", {
      name: /Alice.*1 notification/,
    });
    expect(
      screen.getByRole("menuitemcheckbox", { name: /Bob.*1 notification/ }),
    ).toBeTruthy();

    fireEvent.click(alice);

    expect(
      useInboxFilterStore.getState().filtersByWorkspace["ws-1"]?.actors,
    ).toEqual(["member:alice"]);
  });

  it("hides the From submenu when no row carries an actor", async () => {
    renderMenu();

    fireEvent.click(screen.getByRole("button", { name: "Filter inbox" }));

    expect(screen.queryByRole("menuitem", { name: /^From/ })).toBeNull();
  });

  it("toggles unread only and counts the unread rows behind it", async () => {
    renderMenu({
      items: [
        item("unread", "todo", "high"),
        item("read", "done", "low", { read: true }),
      ],
    });

    fireEvent.click(screen.getByRole("button", { name: "Filter inbox" }));
    const unread = await screen.findByRole("menuitemcheckbox", {
      name: /Unread only.*1 notification/,
    });

    fireEvent.click(unread);

    expect(
      useInboxFilterStore.getState().filtersByWorkspace["ws-1"]?.unreadOnly,
    ).toBe(true);
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "1 active filter" }),
      ).toHaveTextContent("1"),
    );
  });

  it("hides priority and clears its filter for a legacy response", async () => {
    useInboxFilterStore.getState().togglePriorityFilter("ws-1", "high");
    renderMenu({
      items: [item("legacy", "todo", undefined)],
      priorityFilterSupport: "unsupported",
    });

    fireEvent.click(screen.getByRole("button", { name: "Filter inbox" }));
    expect(
      screen.queryByRole("menuitem", { name: /^Priority/ }),
    ).toBeNull();
    await waitFor(() =>
      expect(
        useInboxFilterStore.getState().filtersByWorkspace["ws-1"],
      ).toBeUndefined(),
    );
  });
});
