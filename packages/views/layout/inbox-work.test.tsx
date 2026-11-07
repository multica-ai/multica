import { useState, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { EMPTY_ISSUE_VIEW_PREFERENCE } from "@multica/core/api/schemas";
import { issueViewPreferenceOptions } from "@multica/core/issue-views/preferences";
import { issueViewEverywhereOptions } from "@multica/core/issue-views/queries";
import { issueUnreadOptions } from "@multica/core/issues/unread";
import { SIDEBAR_PROJECTS_SCOPE } from "@multica/core/issues/sidebar-project-prefs";
import { pinListOptions } from "@multica/core/pins/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../navigation";

const apiMock = vi.hoisted(() => ({
  listIssueTableRows: vi.fn(async () => ({ rows: [], total: 0, next_cursor: null })),
  getIssue: vi.fn(), listInbox: vi.fn(async () => []),
  getInboxUnreadSummary: vi.fn(async () => [{ workspace_id: "ws-1", count: 4 }]),
  getBaseUrl: () => "https://synthetic.invalid",
  listMembers: vi.fn(async () => []), listAgents: vi.fn(async () => []), listSquads: vi.fn(async () => []),
  listProjects: vi.fn(async () => ({ projects: [], total: 0 })),
  listPinnedItems: vi.fn(async () => []),
  listArchivedInboxPage: vi.fn(async () => ({ items: [{
    id: "notification-a", workspace_id: "ws-1", recipient_type: "member", recipient_id: "u1",
    actor_type: "agent", actor_id: "agent-a", type: "new_comment", severity: "info",
    issue_id: "issue-a", title: "Synthetic notification", body: null, issue_status: "todo",
    read: true, archived: true, created_at: "2026-06-15T08:00:00Z", details: null,
  }], hasMore: false, nextCursor: null })),
  listChatSessions: vi.fn(async () => []),
}));
const actions = vi.hoisted(() => ({
  sidebar: {
    items: [
      { id: "builtin:my_issues", kind: "builtin", key: "my_issues" },
      { id: "builtin:all_issues", kind: "builtin", key: "all_issues" },
    ],
    reorder: vi.fn(),
    unstar: vi.fn(),
  },
  canManage: () => true,
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: apiMock,
}));
vi.mock("../issue-views/use-view-actions", () => ({ useIssueViewActions: () => actions }));
vi.mock("../issue-views/delete-view-dialog", () => ({ DeleteViewDialog: () => null }));
vi.mock("../issues/components/status-icon", () => ({ StatusIcon: () => <span /> }));
vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children }: { children: ReactNode }) => <>{children}</>,
  PointerSensor: vi.fn(),
  closestCenter: vi.fn(),
  useSensor: vi.fn(),
  useSensors: vi.fn(),
}));
vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: { children: ReactNode }) => <>{children}</>,
  useSortable: () => ({ listeners: {}, setNodeRef: vi.fn(), setActivatorNodeRef: vi.fn() }),
  verticalListSortingStrategy: vi.fn(),
  arrayMove: vi.fn(),
}));
vi.mock("@multica/ui/components/common/tooltip-hint", () => ({
  TooltipHint: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace("acme"),
    useCurrentWorkspace: () => ({ id: "ws-1" }),
  };
});
vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/auth", async (importOriginal) => {
  const state = { user: { id: "u1" } };
  return {
    ...(await importOriginal<typeof import("@multica/core/auth")>()),
    useAuthStore: Object.assign((selector: (s: typeof state) => unknown) => selector(state), {
      getState: () => state,
    }),
  };
});
vi.mock("@multica/core/issue-statuses/hooks", async () => {
  const { buildIssueStatusCatalog } = await import("@multica/core/issue-statuses/queries");
  const catalog = buildIssueStatusCatalog([
    {
      id: "s1",
      workspace_id: "ws-1",
      key: "todo",
      name: "Todo",
      description: "",
      category: "unstarted",
      color: "",
      is_system: true,
      position: 1,
      archived_at: null,
      created_at: "",
      updated_at: "",
    },
  ]);
  return { useIssueStatuses: () => catalog };
});


import { SidebarProvider, useSidebar } from "@multica/ui/components/ui/sidebar";
import { AppRail } from "./app-rail";
import { AppShellProvider, useAppShell } from "./app-shell-context";
import { SpacePanel } from "./space-panel";
import { CompactNavSheet } from "./compact-nav-sheet";
import { InboxPage } from "../inbox/components/inbox-page";

vi.mock("./rail-workspaces", () => ({ RailWorkspaces: () => null }));
vi.mock("./account-menu", () => ({ AccountMenu: () => null }));
vi.mock("./help-launcher", () => ({ HelpLauncher: () => null }));
vi.mock("./panels/team-status", () => ({ useTeamAgentStatus: () => ({ stuck: [] }) }));
vi.mock("../issues/components/issue-detail", () => ({
  IssueDetail: ({ issueId }: { issueId: string }) => <div data-testid="detail">{issueId}</div>,
  issueHighlightMementoKey: (id: string) => id,
}));
vi.mock("@multica/core/chat", () => {
  const state = { activeSessionId: null, isOpen: false };
  return { useChatStore: Object.assign((selector: (s: typeof state) => unknown) => selector(state), { getState: () => state }) };
});
import { GlobalShortcuts } from "./global-shortcuts";
import { createShortcutChord, useShortcutStore } from "@multica/core/shortcuts";
import { useModalStore } from "@multica/core/modals";
let shell: ReturnType<typeof useAppShell>;
function Controls() {
  shell = useAppShell();
  const sidebar = useSidebar();
  return <>
    <button onClick={shell?.toggleNav}>Toggle navigation</button>
    <button onClick={() => sidebar.setOpenMobile(true)}>Open sheet</button>
  </>;
}
function renderIntegration(initial = "/acme/inbox?view=archived&issue=issue-a#comment-a", renderedPathname?: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false }, mutations: { retry: false } } });
  qc.setQueryData(projectListOptions("ws-1").queryKey, { projects: [{
    workspace_id: "ws-1", description: null, icon: null, priority: "none", start_date: null, due_date: null, resource_count: 0,
    id: "project-a", title: "Launch", status: "in_progress", lead_type: null, lead_id: null,
    issue_count: 0, done_count: 0, created_at: "", updated_at: "",
  }], total: 1 });
  qc.setQueryData(pinListOptions("ws-1", "u1").queryKey, []);
  qc.setQueryData(issueViewEverywhereOptions("ws-1").queryKey, []);
  qc.setQueryData(issueViewPreferenceOptions("ws-1", SIDEBAR_PROJECTS_SCOPE).queryKey, EMPTY_ISSUE_VIEW_PREFERENCE);
  qc.setQueryData(agentTaskSnapshotOptions("ws-1").queryKey, []);
  qc.setQueryData(issueUnreadOptions("ws-1").queryKey, []);
  function Harness() {
    const [url, setUrl] = useState(initial);
    const parsed = new URL(url, "https://synthetic.invalid");
    const adapter: NavigationAdapter = { push: setUrl, replace: setUrl, back: vi.fn(), pathname: parsed.pathname,
      searchParams: parsed.searchParams, hash: parsed.hash, getShareableUrl: (path) => path };
    return <NavigationProvider value={adapter}><SidebarProvider><AppShellProvider renderedPathname={renderedPathname}>
      <Controls /><GlobalShortcuts /><AppRail globalActions={false} /><SpacePanel /><CompactNavSheet globalActions={false} />
      {parsed.pathname.endsWith("/inbox") ? <InboxPage /> : <div>Work page</div>}
      <output data-testid="url">{url}</output>
    </AppShellProvider></SidebarProvider></NavigationProvider>;
  }
  const result = renderWithI18n(<QueryClientProvider client={qc}><Harness /></QueryClientProvider>);
  return { ...result, qc };
}

beforeEach(() => {
  localStorage.clear();
  useShortcutStore.getState().resetAll();
  useModalStore.getState().close();
  apiMock.listInbox.mockClear();
  apiMock.getInboxUnreadSummary.mockClear();
});

it("keeps real Work navigation beside Inbox and gives only navigation the collapse edge", async () => {
  const { container } = renderIntegration("/acme/inbox");
  const nav = container.querySelector("[data-slot=space-panel]");
  expect(nav).not.toBeNull();
  expect(within(nav as HTMLElement).getByRole("link", { name: /^Inbox/ })).toHaveAttribute("aria-current", "page");
  expect(within(nav as HTMLElement).getByRole("link", { name: "All issues" })).toBeInTheDocument();
  expect(container.querySelector("[data-slot=app-rail] a[data-space=inbox]")).toBeNull();
  expect(container.querySelector("[data-space=work]")).toHaveAttribute("aria-current", "page");
  await waitFor(() => expect(container.querySelector("[data-space=work] number-flow-react")).toHaveAttribute("aria-label", "4"));
  fireEvent.click(screen.getByRole("button", { name: "Toggle navigation" }));
  expect(nav).toHaveAttribute("data-state", "collapsed");
  expect(container.querySelector("[data-slot=list-column]")).toHaveAttribute("data-state", "expanded");
  const resize = screen.getByRole("button", { name: "Resize list column" });
  fireEvent.click(resize);
  expect(nav).toHaveAttribute("data-state", "collapsed");
  expect(container.querySelector("[data-slot=list-column]")).toHaveAttribute("data-state", "expanded");
  expect(apiMock.listInbox.mock.calls.length).toBeLessThanOrEqual(1);
});

it("returns from Inbox to default Work home without changing the Inbox memory slot", async () => {
  renderIntegration("/acme/inbox?view=archived");
  expect(shell?.space).toBe("inbox");
  expect(shell?.spaceHref("work", "/acme/issues")).toBe("/acme/issues");
  fireEvent.click(document.querySelector("[data-space=work]")!);
  expect(screen.getByTestId("url")).toHaveTextContent("/acme/issues");
  fireEvent.click(screen.getByRole("link", { name: /^Inbox/ }));
  expect(screen.getByTestId("url")).toHaveTextContent("/acme/inbox?view=archived");
});


it("keeps selected archived Inbox, query and comment anchor through navigation collapse and resize", async () => {
  const { container } = renderIntegration();
  expect(await screen.findByTestId("detail")).toHaveTextContent("issue-a");
  expect(screen.getByTestId("url")).toHaveTextContent("?view=archived&issue=issue-a#comment-a");
  const list = container.querySelector<HTMLElement>("[data-slot=list-column]")!;
  const resize = screen.getByRole("button", { name: "Resize list column" });
  vi.spyOn(list, "getBoundingClientRect").mockReturnValue({ width: 320 } as DOMRect);
  fireEvent.pointerDown(resize, { button: 0, isPrimary: true, pointerId: 1, clientX: 320 });
  fireEvent.pointerMove(document, { pointerId: 1, clientX: 380 });
  fireEvent.pointerUp(document, { pointerId: 1, clientX: 380 });
  expect(localStorage.getItem("list_column_width")).toBe("380");
  expect(localStorage.getItem("sidebar_width")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Toggle navigation" }));
  expect(await screen.findByTestId("detail")).toHaveTextContent("issue-a");
  expect(screen.getByTestId("url")).toHaveTextContent("#comment-a");
  expect(apiMock.listInbox).not.toHaveBeenCalled();
});

it("shows just the Work preview on hovering the Work rail over an expanded Inbox list", async () => {
  const { container } = renderIntegration("/acme/inbox");
  fireEvent.click(screen.getByRole("button", { name: "Toggle navigation" }));
  fireEvent.pointerEnter(container.querySelector("[data-space=work]")!, { pointerType: "mouse" });
  await waitFor(() => expect(container.querySelectorAll("[data-slot=space-panel-peek]")).toHaveLength(1));
  expect(container.querySelector("[data-slot=space-panel-peek]")).toHaveTextContent("Work");
  expect(container.querySelector("[data-slot=list-column-peek]")).toBeNull();
  expect(container.querySelector("[data-slot=list-column]")).toHaveAttribute("data-state", "expanded");
  act(() => shell!.closePeek());
});

it("keeps the goInbox keyboard destination and project issue creation accessible from Inbox", async () => {
  renderIntegration("/acme/issues");
  act(() => useShortcutStore.getState().setShortcut("goInbox", createShortcutChord("I", { alt: true })));
  fireEvent.keyDown(document, { key: "i", code: "KeyI", altKey: true });
  await waitFor(() => expect(screen.getByTestId("url")).toHaveTextContent("/acme/inbox"));
  expect(shell?.space).toBe("inbox");
  fireEvent.click(screen.getByRole("button", { name: "New issue in Launch" }));
  expect(useModalStore.getState().modal).toMatch(/create-issue/);
  expect(useModalStore.getState().data?.project_id).toBe("project-a");
});

it("uses the rendered desktop route for the panel while navigation runs ahead", () => {
  const { container } = renderIntegration("/acme/chat?session=a", "/acme/inbox");
  expect(shell?.space).toBe("inbox");
  expect(container.querySelector("[data-slot=space-panel]")).toHaveTextContent("Work");
  expect(container.querySelector("[data-space=work]")).toHaveAttribute("aria-current", "page");
  expect(container.querySelector("[data-space=chat]")).not.toHaveAttribute("aria-current");
  expect(within(container.querySelector("[data-slot=space-panel]") as HTMLElement).getByRole("link", { name: /^Inbox/ })).toHaveAttribute("aria-current", "page");
});


it("shares one summary query for the Work and Inbox counts without fetching notification bodies on Work", async () => {
  const { container } = renderIntegration("/acme/issues");
  await waitFor(() => expect(container.querySelector("[data-space=work] number-flow-react")).toHaveAttribute("aria-label", "4"));
  expect(screen.getByRole("link", { name: /^Inbox/ })).toHaveTextContent(/^Inbox4$/);
  expect(apiMock.getInboxUnreadSummary).toHaveBeenCalledTimes(1);
  expect(apiMock.listInbox).not.toHaveBeenCalled();
});

it("places Work navigation in the compact sheet and dismisses same-route Inbox actions", async () => {
  const original = window.innerWidth;
  Object.defineProperty(window, "innerWidth", { configurable: true, value: 390 });
  try {
    const { container } = renderIntegration("/acme/inbox");
    expect(container.querySelector("[data-slot=space-panel]")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Open sheet" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading", { name: "Work" })).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("link", { name: /^Inbox/ }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.getByTestId("url")).toHaveTextContent("/acme/inbox");
  } finally {
    Object.defineProperty(window, "innerWidth", { configurable: true, value: original });
  }
});
