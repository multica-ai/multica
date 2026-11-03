import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SidebarProvider, useSidebar } from "@multica/ui/components/ui/sidebar";
import {
  AppShellProvider,
  useAppShell,
  useListColumnCollapsed,
} from "./app-shell-context";

const navigation = vi.hoisted(() => ({ pathname: "/acme/issues", search: "" }));

vi.mock("../navigation", () => ({
  useNavigation: () => ({
    pathname: navigation.pathname,
    searchParams: new URLSearchParams(navigation.search),
  }),
}));

let shell: ReturnType<typeof useAppShell> = null;
function Reader() {
  shell = useAppShell();
  return null;
}

let sidebarState: string | null = null;
let listColumnCollapsed = false;
let setSidebarOpen: ((open: boolean) => void) | null = null;
function NavReader() {
  shell = useAppShell();
  const sidebar = useSidebar();
  sidebarState = sidebar.state;
  setSidebarOpen = sidebar.setOpen;
  listColumnCollapsed = useListColumnCollapsed();
  return null;
}

function renderNavShell() {
  return render(
    <SidebarProvider>
      <AppShellProvider>
        <NavReader />
      </AppShellProvider>
    </SidebarProvider>,
  );
}

function renderShell() {
  return render(
    <AppShellProvider>
      <Reader />
    </AppShellProvider>,
  );
}

beforeEach(() => {
  vi.useFakeTimers();
  navigation.pathname = "/acme/issues";
  navigation.search = "";
  shell = null;
});

afterEach(() => {
  vi.useRealTimers();
});

describe("AppShellProvider", () => {
  it("is absent outside the shell", () => {
    render(<Reader />);
    expect(shell).toBeNull();
  });

  it("gives panels to work, team and autopilots only", () => {
    const cases: [string, string | null, boolean][] = [
      ["/acme/my-issues", "work", true],
      ["/acme/squads/s-1", "team", true],
      ["/acme/autopilots", "autopilots", true],
      ["/acme/inbox", "inbox", false],
      ["/acme/chat", "chat", false],
      ["/acme/settings", "settings", false],
      ["/acme/attachments/a-1", null, false],
    ];
    for (const [pathname, space, hasPanel] of cases) {
      navigation.pathname = pathname;
      const { unmount } = renderShell();
      expect(shell?.space).toBe(space);
      expect(shell?.hasPanel).toBe(hasPanel);
      unmount();
    }
  });

  it("opens a peek after the pointer settles and switches instantly once open", () => {
    renderShell();
    act(() => shell!.requestPeek("team"));
    act(() => vi.advanceTimersByTime(100));
    expect(shell!.peekSpace).toBeNull();
    act(() => vi.advanceTimersByTime(100));
    expect(shell!.peekSpace).toBe("team");

    act(() => shell!.requestPeek("work"));
    expect(shell!.peekSpace).toBe("work");
  });

  it("closes after release unless the pointer reaches the preview", () => {
    renderShell();
    act(() => shell!.requestPeek("team"));
    act(() => vi.advanceTimersByTime(200));

    act(() => shell!.releasePeek());
    act(() => shell!.holdPeek());
    act(() => vi.advanceTimersByTime(500));
    expect(shell!.peekSpace).toBe("team");

    act(() => shell!.releasePeek());
    act(() => vi.advanceTimersByTime(250));
    expect(shell!.peekSpace).toBeNull();
  });

  it("closes the peek once navigation lands", () => {
    const { rerender } = renderShell();
    act(() => shell!.requestPeek("team"));
    act(() => vi.advanceTimersByTime(200));
    expect(shell!.peekSpace).toBe("team");

    navigation.pathname = "/acme/agents";
    rerender(
      <AppShellProvider>
        <Reader />
      </AppShellProvider>,
    );
    expect(shell!.peekSpace).toBeNull();
  });

  it("collapses the panel and the list columns as one sidebar across spaces", () => {
    const { rerender } = renderNavShell();
    const visit = (pathname: string) => {
      navigation.pathname = pathname;
      rerender(
        <SidebarProvider>
          <AppShellProvider>
            <NavReader />
          </AppShellProvider>
        </SidebarProvider>,
      );
    };

    act(() => shell!.toggleNav());
    expect(sidebarState).toBe("collapsed");
    visit("/acme/inbox");
    expect(listColumnCollapsed).toBe(true);

    act(() => shell!.toggleNav());
    expect(listColumnCollapsed).toBe(false);
    visit("/acme/issues");
    expect(sidebarState).toBe("expanded");
  });

  it("collapses a list column the md–lg auto-collapse left open", () => {
    navigation.pathname = "/acme/inbox";
    renderNavShell();
    act(() => setSidebarOpen!(false));
    expect(listColumnCollapsed).toBe(false);

    act(() => shell!.toggleNav());
    expect(listColumnCollapsed).toBe(true);
    expect(sidebarState).toBe("collapsed");
  });

  it("sends each rail space back to the last page visited in it", () => {
    const { rerender } = renderShell();
    const visit = (pathname: string, search = "") => {
      navigation.pathname = pathname;
      navigation.search = search;
      rerender(
        <AppShellProvider>
          <Reader />
        </AppShellProvider>,
      );
    };

    visit("/acme/projects/p-1", "tab=issues");
    visit("/acme/agents/a-1");
    expect(shell!.spaceHref("work", "/acme/issues")).toBe("/acme/projects/p-1?tab=issues");
    expect(shell!.spaceHref("chat", "/acme/chat")).toBe("/acme/chat");
    // The current space's item leads home, so clicking it again resets.
    expect(shell!.spaceHref("team", "/acme/agents")).toBe("/acme/agents");

    // Memory is per workspace.
    visit("/beta/inbox");
    expect(shell!.spaceHref("work", "/beta/issues")).toBe("/beta/issues");
  });

  it("lays out the page on screen while navigation runs ahead of it", () => {
    const shellAt = (renderedPathname: string) => (
      <AppShellProvider renderedPathname={renderedPathname}>
        <Reader />
      </AppShellProvider>
    );
    const { rerender } = render(shellAt("/acme/issues"));

    // Leaving Work for Inbox: the issues page is still mounted, so it keeps
    // its panel until the inbox page commits.
    navigation.pathname = "/acme/inbox";
    rerender(shellAt("/acme/issues"));
    expect(shell!.space).toBe("work");
    expect(shell!.hasPanel).toBe(true);
    // Space memory follows the navigation, not the lagging page.
    expect(shell!.spaceHref("inbox", "/acme/inbox")).toBe("/acme/inbox");

    rerender(shellAt("/acme/inbox"));
    expect(shell!.space).toBe("inbox");
    expect(shell!.hasPanel).toBe(false);
    expect(shell!.spaceHref("work", "/acme/issues")).toBe("/acme/issues");
  });
});
