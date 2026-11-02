import type { ReactNode } from "react";
import { act, fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SidebarProvider, useSidebar } from "@multica/ui/components/ui/sidebar";
import { renderWithI18n } from "../test/i18n";
import { AppRail } from "./app-rail";
import { AppShellProvider, useAppShell } from "./app-shell-context";

const state = vi.hoisted(() => ({
  pathname: "/acme/issues",
  appForeground: true,
  inboxUnread: 0,
  chatSessions: [] as { id: string; unread_count?: number }[],
  chat: { activeSessionId: null as string | null, isOpen: false },
  stuck: [] as unknown[],
  failingAutopilots: [] as unknown[],
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
  useNavigation: () => ({ pathname: state.pathname, push: vi.fn() }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
    useWorkspacePaths: () => actual.paths.workspace("acme"),
  };
});
vi.mock("@multica/core/inbox/queries", () => ({
  useInboxUnreadCount: () => state.inboxUnread,
}));
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
vi.mock("./panels/autopilots-panel", () => ({
  useFailingAutopilots: () => state.failingAutopilots,
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
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <SidebarProvider>
        <AppShellProvider>
          {rail}
          <CollapseButton />
          {children}
        </AppShellProvider>
      </SidebarProvider>
    </QueryClientProvider>,
  );
}

const link = (href: string) => document.querySelector<HTMLAnchorElement>(`a[href="${href}"]`);
const countBadge = (href: string) => link(href)?.querySelector("number-flow-react") ?? null;

beforeEach(() => {
  state.pathname = "/acme/issues";
  state.appForeground = true;
  state.inboxUnread = 0;
  state.chatSessions = [];
  state.chat = { activeSessionId: null, isOpen: false };
  state.stuck = [];
  state.failingAutopilots = [];
});

describe("AppRail global actions", () => {
  it("carries New issue and search by default", () => {
    renderRail();
    expect(screen.getByRole("button", { name: /^new issue$/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Search..." })).toBeTruthy();
  });

  it("leaves them to the desktop title bar when turned off", () => {
    renderRail(undefined, <AppRail globalActions={false} />);
    expect(screen.queryByRole("button", { name: /^new issue$/i })).toBeNull();
    expect(screen.queryByRole("button", { name: "Search..." })).toBeNull();
  });
});

describe("AppRail spaces", () => {
  it("links each space and marks the current route's space", () => {
    state.pathname = "/acme/projects/p-1";
    renderRail();

    for (const href of ["/acme/inbox", "/acme/issues", "/acme/agents", "/acme/chat", "/acme/autopilots"]) {
      expect(link(href)).not.toBeNull();
    }
    // Projects belong to the work space, which links to Issues.
    expect(link("/acme/issues")).toHaveAttribute("aria-current", "page");
    expect(link("/acme/agents")).not.toHaveAttribute("aria-current");
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

describe("AppRail badges", () => {
  it("counts inbox unread with a static counter", () => {
    state.inboxUnread = 4;
    renderRail();
    const badge = countBadge("/acme/inbox") as (HTMLElement & { animated?: boolean }) | null;
    expect(badge).toHaveAttribute("aria-label", "4");
    expect(badge?.animated).toBe(false);
  });

  it("shows no attention dots when nothing needs a look", () => {
    renderRail();
    expect(link("/acme/agents")?.querySelector(".bg-warning")).toBeNull();
    expect(link("/acme/autopilots")?.querySelector(".bg-destructive")).toBeNull();
  });

  it("flags Team when an agent is stuck and Autopilot when a run failed", () => {
    state.stuck = [{ id: "agent-1" }];
    state.failingAutopilots = [{ id: "ap-1" }];
    renderRail();
    expect(link("/acme/agents")?.querySelector(".bg-warning")).not.toBeNull();
    expect(link("/acme/autopilots")?.querySelector(".bg-destructive")).not.toBeNull();
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

    // Inbox and Chat keep their lists in their pages but still preview.
    hover("/acme/inbox");
    expect(peek).toBe("inbox");
  });
});

function PeekReader({ onPeek }: { onPeek: (space: string | null) => void }) {
  onPeek(useAppShell()?.peekSpace ?? null);
  return null;
}
