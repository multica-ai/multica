import type { ReactNode } from "react";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configStore } from "@multica/core/config";
import { DOCS_V1_FLAG } from "@multica/core/feature-flags";
import { useRailOrderStore } from "@multica/core/navigation";
import {
  DEFAULT_SIDEBAR_INBOX_SETTINGS,
  useSidebarTreeStore,
} from "@multica/core/issues/stores/sidebar-tree-store";
import { SidebarProvider, useSidebar } from "@multica/ui/components/ui/sidebar";
import { renderWithI18n } from "../test/i18n";
import { AppRail } from "./app-rail";
import { AppShellProvider, useAppShell } from "./app-shell-context";
import type { InboxItem } from "@multica/core/types";

const state = vi.hoisted(() => ({
  pathname: "/acme/issues",
  search: "",
  inboxItems: undefined as InboxItem[] | undefined,
  appForeground: true,
  inboxUnread: 0,
  chatSessions: [] as { id: string; unread_count?: number }[],
  chat: { activeSessionId: null as string | null, isOpen: false },
  stuck: [] as unknown[],
  desktop: false,
  markAllInboxRead: vi.fn(),
  markAllChatsRead: vi.fn(),
}));

vi.mock("../navigation", () => ({
  AppLink: ({
    children,
    href,
    newTabTitle: _newTabTitle,
    ...props
  }: { children: ReactNode; href: string; newTabTitle?: string } & React.ComponentProps<"a">) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  useNavigation: () => ({
    pathname: state.pathname,
    searchParams: new URLSearchParams(state.search),
    push: vi.fn(),
    openInNewTab: state.desktop ? vi.fn() : undefined,
    getShareableUrl: (href: string) => href,
  }),
  useIntentNavigate: () => vi.fn(),
}));
vi.mock("@multica/core/inbox/mutations", () => ({
  useMarkAllInboxRead: () => ({ mutate: state.markAllInboxRead }),
}));
vi.mock("@multica/core/chat/mutations", () => ({
  useMarkAllChatSessionsRead: () => ({ mutate: state.markAllChatsRead }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
    useWorkspacePaths: () => actual.paths.workspace("acme"),
  };
});
vi.mock("@multica/core/inbox/queries", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/inbox/queries")>();
  return {
    deduplicateInboxItems: actual.deduplicateInboxItems,
    inboxListOptions: actual.inboxListOptions,
    useInboxUnreadCount: () => state.inboxUnread,
  };
});
vi.mock("@multica/core/inbox/use-snooze-wake", () => ({ useInboxSnoozeWake: () => {} }));
vi.mock("@multica/core/chat/queries", () => ({
  chatSessionsOptions: (wsId: string) => ({
    queryKey: ["chat", wsId, "sessions"],
    queryFn: async () => state.chatSessions,
  }),
}));
// Callable-store shape (selectorFn + getState) per the repo testing rules.
vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (selector: (s: typeof state.chat) => unknown) => selector(state.chat),
    { getState: () => state.chat },
  ),
}));
vi.mock("@multica/core/shortcuts", () => ({ useShortcut: () => null }));
vi.mock("../common/use-app-foreground", () => ({
  useAppForeground: () => state.appForeground,
}));
vi.mock("./panels/team-status", () => ({
  useTeamAgentStatus: () => ({ stuck: state.stuck }),
}));
vi.mock("./rail-workspaces", () => ({ RailWorkspaces: () => null }));
vi.mock("./help-launcher", () => ({ HelpLauncher: () => null }));
vi.mock("./account-menu", () => ({ AccountMenu: () => null }));
vi.mock("@multica/ui/components/common/tooltip-hint", () => ({
  TooltipHint: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

function CollapseButton() {
  const { setOpen } = useSidebar();
  return (
    <button type="button" onClick={() => setOpen(false)}>
      collapse
    </button>
  );
}

function renderRail(children?: ReactNode, rail: ReactNode = <AppRail />) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  client.setQueryData(["chat", "ws-1", "sessions"], state.chatSessions);
  if (state.inboxItems) client.setQueryData(["inbox", "ws-1", "list"], state.inboxItems);
  const tree = () => (
    <QueryClientProvider client={client}>
      <SidebarProvider>
        <AppShellProvider>
          {rail}
          <CollapseButton />
          {children}
        </AppShellProvider>
      </SidebarProvider>
    </QueryClientProvider>
  );
  const result = renderWithI18n(tree());
  const visit = (pathname: string, search = "") => {
    state.pathname = pathname;
    state.search = search;
    result.rerender(tree());
  };
  return { ...result, client, visit };
}

const link = (href: string) => document.querySelector<HTMLAnchorElement>(`a[href="${href}"]`);
const countBadge = (href: string) => link(href)?.querySelector("number-flow-react") ?? null;

beforeEach(() => {
  state.pathname = "/acme/issues";
  state.search = "";
  state.inboxItems = undefined;
  state.appForeground = true;
  state.inboxUnread = 0;
  state.chatSessions = [];
  state.chat = { activeSessionId: null, isOpen: false };
  state.stuck = [];
  state.desktop = false;
  state.markAllInboxRead.mockReset();
  state.markAllChatsRead.mockReset();
  useRailOrderStore.setState({ order: null });
  configStore.getState().setFeatureFlags({ [DOCS_V1_FLAG]: true });
  useSidebarTreeStore.setState({ inboxSettings: DEFAULT_SIDEBAR_INBOX_SETTINGS });
});

const railOrder = () =>
  Array.from(document.querySelectorAll<HTMLElement>("[data-rail-item]"), (item) => item.dataset.railItem);

describe("AppRail global actions", () => {
  it("carries New issue and search", () => {
    renderRail();
    expect(screen.getByRole("button", { name: /^new issue$/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Search..." })).toBeTruthy();
  });
});

describe("AppRail order", () => {
  it("lists the spaces in the default order", () => {
    renderRail();
    expect(railOrder()).toEqual(["work", "docs", "team", "chat", "autopilots"]);
  });

  it("follows the saved order", () => {
    const order = ["chat", "work", "docs", "team", "autopilots"] as const;
    useRailOrderStore.getState().setOrder(order);
    renderRail();
    expect(railOrder()).toEqual(order);
  });

  it("leaves Docs out while docs_v1 is off, keeping its saved place", () => {
    const order = ["chat", "docs", "work", "team", "autopilots"] as const;
    useRailOrderStore.getState().setOrder(order);
    configStore.getState().setFeatureFlags({});
    renderRail();
    expect(railOrder()).toEqual(["chat", "work", "team", "autopilots"]);
    expect(useRailOrderStore.getState().order).toEqual(order);
  });

  // dnd-kit stops the release's click at the document without cancelling
  // it, so the link would navigate natively even though React never sees it.
  it("does not open the space released at the end of a drag", async () => {
    renderRail();
    const chat = link("/acme/chat")!;
    const pointer = { isPrimary: true, button: 0, clientX: 30, clientY: 300 };
    fireEvent.pointerDown(chat, pointer);
    fireEvent.pointerMove(document, { ...pointer, clientY: 310 });
    fireEvent.pointerUp(document, { ...pointer, clientY: 310 });

    expect(fireEvent.click(chat)).toBe(false);

    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(fireEvent.click(chat)).toBe(true);
  });
});

describe("AppRail spaces", () => {
  it("links each space and marks the current route's space", () => {
    state.pathname = "/acme/projects/p-1";
    renderRail();

    for (const href of ["/acme/issues", "/acme/agents", "/acme/chat", "/acme/autopilots"]) {
      expect(link(href)).not.toBeNull();
    }
    // Projects belong to the work space, which links to Issues.
    expect(link("/acme/issues")).toHaveAttribute("aria-current", "page");
    expect(link("/acme/agents")).not.toHaveAttribute("aria-current");
  });

  it("has no Inbox space: the full Inbox lights Work", () => {
    state.pathname = "/acme/inbox";
    renderRail();
    expect(document.querySelector('a[data-space="inbox"]')).toBeNull();
    expect(document.querySelector('a[data-space="work"]')).toHaveAttribute("aria-current", "page");
  });

  it("marks Settings in the footer while on a settings route", () => {
    state.pathname = "/acme/settings";
    renderRail();
    expect(screen.getByRole("link", { name: "Settings" })).toHaveAttribute("aria-current", "page");
  });

  it("lands Team on agents and keeps it lit across the team pages", () => {
    state.pathname = "/acme/runtimes";
    renderRail();
    expect(link("/acme/agents")).toHaveAttribute("aria-current", "page");
  });
});

describe("AppRail label disclosure", () => {
  const tooltip = () => document.querySelector("[data-slot=tooltip-content]");

  // jsdom does not lay out the rail: model a label too long for the item.
  function clipLabel(el: HTMLElement) {
    el.style.overflowX = "hidden";
    Object.defineProperties(el, {
      scrollWidth: { configurable: true, value: 90 },
      clientWidth: { configurable: true, value: 44 },
    });
  }

  it("names a space whose label is cut off even when it is already the full name", async () => {
    renderRail();
    const chat = link("/acme/chat")!;
    clipLabel(within(chat).getByText("Chat"));
    await act(async () => chat.focus());
    expect(tooltip()).toHaveTextContent("Chat");
  });

  it("stays quiet when the full-name label fits", async () => {
    renderRail();
    await act(async () => link("/acme/chat")!.focus());
    expect(tooltip()).toBeNull();
  });
});

function inboxItem(issueId: string, archived = false): InboxItem {
  return {
    id: `inbox-${issueId}`, workspace_id: "ws-1", recipient_type: "member",
    recipient_id: "member-1", actor_type: "agent", actor_id: "agent-1",
    type: "new_comment", severity: "info", issue_id: issueId,
    title: "Issue title", body: null, issue_status: "done", issue_priority: "none",
    read: true, archived, created_at: "2026-11-01T08:00:00Z", details: null,
  };
}

// Read by space: an href query string trips the attribute-selector lookup.
const workHref = () => document.querySelector('a[data-space="work"]')?.getAttribute("href");

describe("AppRail inbox return", () => {
  it("returns Work to the notification last opened while it is still in the inbox", () => {
    state.inboxItems = [inboxItem("issue-x")];
    const { visit } = renderRail();
    visit("/acme/inbox", "issue=issue-x");
    visit("/acme/agents");
    expect(workHref()).toBe("/acme/inbox?issue=issue-x");
  });

  // Regression: the inbox page replaces an unknown ?issue= with the issue
  // itself, so a remembered selection that was archived since turned the
  // rail item into a flash back to the issue on every click.
  it("drops a remembered notification that has left the inbox", async () => {
    state.inboxItems = [inboxItem("issue-x")];
    const { client, visit } = renderRail();
    visit("/acme/inbox", "issue=issue-x");
    visit("/acme/agents");
    act(() => {
      client.setQueryData(["inbox", "ws-1", "list"], [inboxItem("issue-x", true), inboxItem("issue-y")]);
    });
    await waitFor(() => expect(workHref()).toBe("/acme/inbox"));
  });

  it("leaves the selection to the inbox page when its list is not cached", () => {
    const { visit } = renderRail();
    visit("/acme/inbox", "issue=issue-x");
    visit("/acme/agents");
    expect(workHref()).toBe("/acme/inbox?issue=issue-x");
  });

  it("keeps an archived-view selection, which resolves against its own list", () => {
    state.inboxItems = [];
    const { visit } = renderRail();
    visit("/acme/inbox", "view=archived&issue=issue-x");
    visit("/acme/agents");
    expect(workHref()).toBe("/acme/inbox?view=archived&issue=issue-x");
  });

  it("leaves other Work pages alone", () => {
    state.inboxItems = [];
    const { visit } = renderRail();
    visit("/acme/issues", "issue=issue-x");
    visit("/acme/agents");
    expect(workHref()).toBe("/acme/issues?issue=issue-x");
  });
});

describe("AppRail badges", () => {
  it("counts inbox unread on Work with a static counter", () => {
    state.inboxUnread = 4;
    renderRail();
    const badge = countBadge("/acme/issues") as (HTMLElement & { animated?: boolean }) | null;
    expect(badge).toHaveAttribute("aria-label", "4");
    expect(badge?.animated).toBe(false);
  });

  it("leaves Work unbadged when the Inbox settings turn the badge off", () => {
    state.inboxUnread = 4;
    useSidebarTreeStore.setState({ inboxSettings: { ...DEFAULT_SIDEBAR_INBOX_SETTINGS, railBadge: false } });
    renderRail();
    expect(countBadge("/acme/issues")).toBeNull();
  });

  it("shows no attention dot when nothing needs a look", () => {
    renderRail();
    expect(link("/acme/agents")?.querySelector(".bg-warning")).toBeNull();
  });

  it("flags Team when an agent is stuck", () => {
    state.stuck = [{ id: "agent-1" }];
    renderRail();
    expect(link("/acme/agents")?.querySelector(".bg-warning")).not.toBeNull();
  });
});

describe("AppRail chat badge", () => {
  const chatBadge = () => countBadge("/acme/chat");

  it("sums unread messages across sessions", () => {
    state.chatSessions = [
      { id: "a", unread_count: 3 },
      { id: "b", unread_count: 2 },
      { id: "c", unread_count: 0 },
    ];
    renderRail();
    expect(chatBadge()).toHaveAttribute("aria-label", "5");
  });

  it("shows nothing when every session is read", () => {
    state.chatSessions = [{ id: "a", unread_count: 0 }, { id: "b" }];
    renderRail();
    expect(chatBadge()).toBeNull();
  });

  // The thread list zeroes the open session's row; the aggregate must
  // follow, or a reply landing there flashes a count with no matching row.
  it("excludes the session being read on the chat route", () => {
    state.chatSessions = [{ id: "a", unread_count: 2 }, { id: "b", unread_count: 3 }];
    state.pathname = "/acme/chat";
    state.chat = { activeSessionId: "a", isOpen: false };
    renderRail();
    expect(chatBadge()).toHaveAttribute("aria-label", "3");
  });

  it("excludes the session open in the floating window off-route", () => {
    state.chatSessions = [{ id: "a", unread_count: 2 }, { id: "b", unread_count: 3 }];
    state.chat = { activeSessionId: "a", isOpen: true };
    renderRail();
    expect(chatBadge()).toHaveAttribute("aria-label", "3");
  });

  it("still counts a remembered selection no chat surface is showing", () => {
    state.chatSessions = [{ id: "a", unread_count: 2 }, { id: "b", unread_count: 3 }];
    state.chat = { activeSessionId: "a", isOpen: false };
    renderRail();
    expect(chatBadge()).toHaveAttribute("aria-label", "5");
  });

  // Backgrounded, auto mark-read is suppressed (MUL-4485): the reply stays
  // unread and must badge, or the notification is eaten while away.
  it("counts the open session while the app is backgrounded", () => {
    state.chatSessions = [{ id: "a", unread_count: 2 }, { id: "b", unread_count: 3 }];
    state.pathname = "/acme/chat";
    state.chat = { activeSessionId: "a", isOpen: true };
    state.appForeground = false;
    renderRail();
    expect(chatBadge()).toHaveAttribute("aria-label", "5");
  });
});

describe("AppRail space menu", () => {
  const openMenu = async (href: string) => {
    fireEvent.contextMenu(link(href)!, { clientX: 30, clientY: 300 });
    return screen.findByRole("menu");
  };
  const markAllRead = (menu: HTMLElement) => within(menu).queryByRole("menuitem", { name: "Mark all as read" });

  beforeEach(() => {
    state.desktop = true;
  });

  it("marks every unread chat read from Chat", async () => {
    state.chatSessions = [
      { id: "a", unread_count: 3 },
      { id: "b", unread_count: 0 },
      { id: "c", unread_count: 22 },
    ];
    renderRail();
    fireEvent.click(markAllRead(await openMenu("/acme/chat"))!);
    expect(state.markAllChatsRead).toHaveBeenCalledWith(["a", "c"], expect.anything());
    expect(state.markAllInboxRead).not.toHaveBeenCalled();
  });

  it("offers no Mark all as read on Chat when every chat is read", async () => {
    state.chatSessions = [{ id: "a", unread_count: 0 }];
    renderRail();
    expect(markAllRead(await openMenu("/acme/chat"))).toBeNull();
  });

  it("marks the inbox read from Work", async () => {
    state.inboxUnread = 4;
    state.chatSessions = [{ id: "a", unread_count: 3 }];
    renderRail();
    fireEvent.click(markAllRead(await openMenu("/acme/issues"))!);
    expect(state.markAllInboxRead).toHaveBeenCalledTimes(1);
    expect(state.markAllChatsRead).not.toHaveBeenCalled();
  });
});

describe("AppRail panel peek", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  function hover(href: string) {
    fireEvent.pointerEnter(link(href)!, { pointerType: "mouse" });
  }

  it("asks the shell to preview any space only while the nav is collapsed", () => {
    vi.useFakeTimers();
    let peek: string | null = null;
    renderRail(<PeekReader onPeek={(next) => (peek = next)} />);

    hover("/acme/agents");
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(peek).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "collapse" }));
    hover("/acme/agents");
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(peek).toBe("team");

    // Chat keeps its list in its page but still previews.
    hover("/acme/chat");
    expect(peek).toBe("chat");
  });
});

function PeekReader({ onPeek }: { onPeek: (space: string | null) => void }) {
  onPeek(useAppShell()?.peekSpace ?? null);
  return null;
}
