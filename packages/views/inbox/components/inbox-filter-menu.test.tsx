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
import {
  BUILT_IN_STATUS_CATEGORY,
  BUILT_IN_STATUS_ORDER,
} from "@multica/core/issues/config";
import {
  type InboxFilterSupport,
  useInboxFilterStore,
} from "@multica/core/inbox/filter-store";
import type { InboxItem, IssueStatusEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { InboxFilterMenu } from "./inbox-filter-menu";

// The directory hook resolves the current workspace through the route, which
// this leaf render has none of — stubbed the same way the sibling
// inbox-detail-label suite stubs it.
const ACTOR_NAMES: Record<string, string> = { alice: "Alice", bob: "Bob" };
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (type: string, id: string) =>
      type === "system" ? "Multica" : (ACTOR_NAMES[id] ?? "Unknown"),
    getActorInitials: () => "??",
    getActorAvatarUrl: () => null,
  }),
}));

function statusEntry(key: (typeof BUILT_IN_STATUS_ORDER)[number]): IssueStatusEntry {
  return {
    id: `status-${key}`,
    workspace_id: "ws-1",
    key,
    name: key,
    description: "",
    category: BUILT_IN_STATUS_CATEGORY[key],
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
  issueFieldsSupport = "supported",
  archived = false,
  activeIssueIds,
  getArchivedInboxFacets = vi.fn(async () => ({ statuses: {}, priorities: {}, actors: {}, unreadCount: 0 })),
  statusEntries = BUILT_IN_STATUS_ORDER.map(statusEntry),
}: {
  items?: InboxItem[];
  priorityFilterSupport?: InboxFilterSupport;
  issueFieldsSupport?: InboxFilterSupport;
  archived?: boolean;
  activeIssueIds?: ReadonlySet<string>;
  getArchivedInboxFacets?: ReturnType<typeof vi.fn>;
  statusEntries?: IssueStatusEntry[];
} = {}) {
  setApiInstance({
    getArchivedInboxFacets,
    getAgentTaskSnapshot: async () => [],
    listIssueStatuses: async () => ({
      statuses: statusEntries,
      categories: [],
      total: statusEntries.length,
    }),
    listProjects: async () => ({ projects: PROJECTS, total: PROJECTS.length }),
    listLabels: async () => ({ labels: [], total: 0 }),
    listMembers: async () => [],
    listAgents: async () => [],
    listSquads: async () => [],
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
        issueFieldsSupport={issueFieldsSupport}
        activeIssueIds={activeIssueIds}
        archived={archived}
      />
    </QueryClientProvider>,
  );
}

const PROJECTS = [{ id: "project-1", title: "Launch", icon: null, status: "in_progress" }];

function openMenu(name = "Filter inbox") {
  fireEvent.click(screen.getByRole("button", { name }));
}

async function openSubmenu(name: string, trigger = "Filter inbox") {
  openMenu(trigger);
  fireEvent.click(await screen.findByRole("menuitem", { name: new RegExp(`^${name}`) }));
}

function storedFilters() {
  return useInboxFilterStore.getState().filtersByWorkspace["ws-1"];
}

beforeEach(() => {
  useInboxFilterStore.setState({ filtersByWorkspace: {} });
});

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
});

describe("InboxFilterMenu", () => {
  it("groups the fields into notification and issue sections", async () => {
    renderMenu({
      items: [item("from-alice", "todo", "high", { actor_type: "member", actor_id: "alice" })],
    });
    openMenu();

    const notification = await screen.findByText("Notification");
    const issue = screen.getByText("Issue");
    for (const name of ["Read state", "Type", "From", "Received", "Status", "Priority", "Agent status", "Project", "Assignee", "Label", "Due date"]) {
      expect(screen.getByRole("menuitem", { name: new RegExp(`^${name}`) })).toBeTruthy();
    }
    expect(notification.compareDocumentPosition(issue) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.queryByText("Active")).toBeNull();
  });

  it("pins the fields in use on top with their current value", async () => {
    const store = useInboxFilterStore.getState();
    store.toggleFilterValue("ws-1", "type", "mention");
    store.setFilterNegated("ws-1", "type", true);
    renderMenu();
    openMenu("1 active filter");

    expect(await screen.findByText("Active")).toBeTruthy();
    const type = screen.getByRole("menuitem", { name: /^Type/ });
    expect(type).toHaveTextContent(/is not/);
    expect(type).toHaveTextContent(/Mentions/);
    // Pinned once: the notification section no longer repeats it.
    expect(screen.getAllByRole("menuitem", { name: /^Type/ })).toHaveLength(1);
  });

  it("selects a notification type and switches it to is not", async () => {
    renderMenu({
      items: [
        item("mention", "todo", "high"),
        item("comment", "todo", "high", { type: "new_comment" }),
      ],
    });
    await openSubmenu("Type");
    fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: /Comments & reactions.*1 notification/ }));
    expect(storedFilters()?.types).toEqual(["comment"]);

    fireEvent.click(screen.getByRole("menuitemradio", { name: "is not" }));
    expect(storedFilters()?.negated).toEqual(["type"]);
  });

  it("keeps is not when it is chosen before the first type", async () => {
    renderMenu({ items: [item("mention", "todo", "high")] });
    await openSubmenu("Type");
    fireEvent.click(await screen.findByRole("menuitemradio", { name: "is not" }));
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: /^Agent runs/ }));

    expect(storedFilters()).toMatchObject({ types: ["run"], negated: ["type"] });
    expect(screen.getByRole("menuitemradio", { name: "is not" })).toHaveAttribute("aria-checked", "true");
  });

  it("finds a value by search and adds it in one click", async () => {
    renderMenu();
    openMenu();
    fireEvent.change(await screen.findByRole("textbox", { name: "Filter by field or value…" }), {
      target: { value: "last 7" },
    });

    fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: /Received.*Last 7 days/ }));
    expect(storedFilters()?.received).toBe("last_7_days");
  });

  it("offers a project from the directory and stores its id", async () => {
    renderMenu({ items: [item("in-project", "todo", "high", { issue_project_id: "project-1", issue_label_ids: [] })] });
    await openSubmenu("Project");

    fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: /Launch/ }));
    expect(storedFilters()?.projects).toEqual(["project-1"]);
  });

  it("hides the issue-field dimensions for a server without them and clears a carried-over selection", async () => {
    useInboxFilterStore.getState().toggleFilterValue("ws-1", "project", "project-1");
    renderMenu({ issueFieldsSupport: "unsupported" });
    openMenu();

    await screen.findByRole("menuitem", { name: /^Status/ });
    for (const name of ["Project", "Assignee", "Label", "Due date"]) {
      expect(screen.queryByRole("menuitem", { name: new RegExp(`^${name}`) })).toBeNull();
    }
    await waitFor(() => expect(storedFilters()).toBeUndefined());
  });

  it("hides type, received and is not on an older archive server", async () => {
    useInboxFilterStore.getState().toggleStatusFilter("ws-1", "done");
    renderMenu({ archived: true, issueFieldsSupport: "unsupported" });
    await openSubmenu("Status", "1 active filter");

    await screen.findByRole("menuitemcheckbox", { name: /Done/ });
    expect(screen.queryByRole("menuitemradio", { name: "is not" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: /^Type/ })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: /^Received/ })).toBeNull();
  });

  it("keeps a selected retired status removable when absent from archive facets and pages", async () => {
    useInboxFilterStore.getState().toggleStatusFilter("ws-1", "custom-retired");
    renderMenu({
      items: [], archived: true,
      statusEntries: [...BUILT_IN_STATUS_ORDER.map(statusEntry), {
        ...statusEntry("todo"), id: "retired", key: "custom-retired",
        name: "Retired status", is_system: false, archived_at: "2026-09-01T00:00:00Z",
      }],
    });
    await openSubmenu("Status", "1 active filter");
    const selected = await screen.findByRole("menuitemcheckbox", { name: "Retired status" });
    expect(selected).toHaveAttribute("aria-checked", "true");
    fireEvent.click(selected);
    expect(storedFilters()).toBeUndefined();
  });

  it("keeps a selected priority removable when absent from archive facets and pages", async () => {
    useInboxFilterStore.getState().togglePriorityFilter("ws-1", "high");
    renderMenu({ items: [], archived: true });
    await openSubmenu("Priority", "1 active filter");
    const selected = await screen.findByRole("menuitemcheckbox", { name: "High" });
    expect(selected).toHaveAttribute("aria-checked", "true");
    fireEvent.click(selected);
    expect(storedFilters()).toBeUndefined();
  });

  it("loads full-archive facets only when opened, including an actor absent from loaded rows", async () => {
    const getArchivedInboxFacets = vi.fn(async () => ({ statuses: { done: 251 }, priorities: { high: 251 }, actors: { "member:alice": 251 }, unreadCount: 0 }));
    renderMenu({ items: [], archived: true, getArchivedInboxFacets });
    expect(getArchivedInboxFacets).not.toHaveBeenCalled();
    await openSubmenu("From");
    expect(await screen.findByRole("menuitemcheckbox", { name: /Alice.*251/ })).toBeTruthy();
    expect(getArchivedInboxFacets).toHaveBeenCalledTimes(1);
  });

  it("selects a status and exposes the active count on the trigger", async () => {
    renderMenu();
    await openSubmenu("Status");
    fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: /Todo.*1 notification/ }));

    expect(storedFilters()?.statuses).toEqual(["todo"]);
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "1 active filter" })).toHaveTextContent("1"),
    );
  });

  it("selects a priority", async () => {
    renderMenu();
    await openSubmenu("Priority");
    fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: /High.*1 notification/ }));

    expect(storedFilters()?.priorities).toEqual(["high"]);
  });

  it("resets every active filter from the root menu", async () => {
    const store = useInboxFilterStore.getState();
    store.toggleStatusFilter("ws-1", "todo");
    store.togglePriorityFilter("ws-1", "high");
    renderMenu();

    openMenu("2 active filters");
    fireEvent.click(await screen.findByRole("menuitem", { name: "Reset all filters" }));

    expect(storedFilters()).toBeUndefined();
  });

  it("offers only the actors the view's notifications carry", async () => {
    renderMenu({
      items: [
        item("from-alice", "todo", "high", { actor_type: "member", actor_id: "alice" }),
        item("from-bob", "done", "low", { actor_type: "agent", actor_id: "bob" }),
      ],
    });

    await openSubmenu("From");
    const alice = await screen.findByRole("menuitemcheckbox", { name: /Alice.*1 notification/ });
    expect(screen.getByRole("menuitemcheckbox", { name: /Bob.*1 notification/ })).toBeTruthy();

    fireEvent.click(alice);
    expect(storedFilters()?.actors).toEqual(["member:alice"]);
  });

  it("hides From when no notification carries an actor", async () => {
    renderMenu();
    openMenu();

    await screen.findByRole("menuitem", { name: /^Status/ });
    expect(screen.queryByRole("menuitem", { name: /^From/ })).toBeNull();
  });

  it("filters to unread and counts the unread issues behind it", async () => {
    renderMenu({
      items: [
        item("unread", "todo", "high"),
        item("read", "done", "low", { read: true }),
      ],
    });

    await openSubmenu("Read state");
    fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: /Unread.*1 notification/ }));

    expect(storedFilters()?.unreadOnly).toBe(true);
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "1 active filter" })).toHaveTextContent("1"),
    );
  });

  it("filters by agent status without a count beside it", async () => {
    renderMenu({ activeIssueIds: new Set(["issue-todo-high"]) });

    await openSubmenu("Agent status");
    fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: /^Idle$/ }));

    expect(storedFilters()?.agent).toBe("idle");
  });

  it("hides agent status in the archive and ignores a carried-over selection", async () => {
    useInboxFilterStore.getState().setAgent("ws-1", "idle");
    renderMenu({ archived: true });

    // Not counted on the trigger, and not offered in the menu.
    openMenu();
    await screen.findByRole("menuitem", { name: /^Status/ });
    expect(screen.queryByRole("menuitem", { name: /^Agent status/ })).toBeNull();
  });

  it("hides priority and clears its filter for a legacy response", async () => {
    useInboxFilterStore.getState().togglePriorityFilter("ws-1", "high");
    renderMenu({
      items: [item("legacy", "todo", undefined)],
      priorityFilterSupport: "unsupported",
    });

    openMenu();
    await screen.findByRole("menuitem", { name: /^Status/ });
    expect(screen.queryByRole("menuitem", { name: /^Priority/ })).toBeNull();
    await waitFor(() => expect(storedFilters()).toBeUndefined());
  });
});
