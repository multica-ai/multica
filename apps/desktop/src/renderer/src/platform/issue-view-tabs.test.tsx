import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
import { getActiveTab, useTabStore } from "@/stores/tab-store";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@/stores/window-overlay-store", () => ({
  useWindowOverlayStore: Object.assign(() => null, { getState: () => ({ overlay: null, open: vi.fn(), close: vi.fn() }) }),
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(() => null, { getState: () => ({ logout: vi.fn() }) }),
}));

import { DesktopNavigationProvider } from "./navigation";
import { IssueViewUrlSync, __resetIssueViewTabsForTests, initIssueViewTabs } from "./issue-view-tabs";

const KEY = "ws-1:workspace";
const WORKSPACE = { scope_type: "workspace" } as const;
let rendered: (string | null)[] = [];

function Probe() {
  const view = useActiveIssueViewStore((s) => s.active[KEY] ?? null);
  rendered.push(view);
  return <span data-testid="view">{view ?? "none"}</span>;
}

/** The active tab's page, remounted per tab the way TabContent does. */
function ActiveIssuesPage() {
  const tabId = useTabStore((s) => getActiveTab(s)?.id);
  return (
    <DesktopNavigationProvider>
      <IssueViewUrlSync key={tabId} scope={WORKSPACE}>
        <Probe />
      </IssueViewUrlSync>
    </DesktopNavigationProvider>
  );
}

const tabs = () => useTabStore.getState().byWorkspace.acme.tabs;
const activeUrl = () => getActiveTab(useTabStore.getState())!.url;
const shown = () => screen.getByTestId("view").textContent;

beforeEach(() => {
  Object.defineProperty(window, "desktopAPI", {
    configurable: true,
    value: { runtimeConfig: { ok: true, config: { appUrl: "https://app.example" } } },
  });
  __resetIssueViewTabsForTests();
  useActiveIssueViewStore.setState({ active: {} });
  useTabStore.getState().reset();
  useTabStore.getState().switchWorkspace("acme");
  initIssueViewTabs();
  rendered = [];
});

afterEach(() => __resetIssueViewTabsForTests());

describe("desktop issue views per tab", () => {
  it("opens a cmd+clicked view in its own tab, and each tab keeps its view", () => {
    useTabStore.getState().navigateActiveSession("/acme/issues?view=a");
    render(<ActiveIssuesPage />);
    expect(shown()).toBe("a");
    const tabA = getActiveTab(useTabStore.getState())!.id;

    act(() => {
      useTabStore.getState().openTab("/acme/issues?view=b", "B");
    });
    expect(tabs()).toHaveLength(2);
    expect(shown()).toBe("a");

    const tabB = tabs().find((t) => t.id !== tabA)!.id;
    rendered = [];
    act(() => useTabStore.getState().setActiveTab(tabB));
    expect(shown()).toBe("b");
    expect(rendered).not.toContain("a");

    act(() => useTabStore.getState().setActiveTab(tabA));
    expect(shown()).toBe("a");
    act(() => useTabStore.getState().setActiveTab(tabB));
    expect(shown()).toBe("b");
  });

  it("shows All issues in a tab opened without a view, whatever another tab has open", () => {
    useTabStore.getState().navigateActiveSession("/acme/issues?view=a");
    render(<ActiveIssuesPage />);

    act(() => {
      useTabStore.getState().openTab("/acme/issues", "Issues", { activate: true });
    });

    expect(activeUrl()).toBe("/acme/issues");
    expect(shown()).toBe("none");
  });

  it("writes view changes back to the tab's URL without adding history", () => {
    render(<ActiveIssuesPage />);
    const history = () => getActiveTab(useTabStore.getState())!.history.stack;

    act(() => useActiveIssueViewStore.getState().setActive(KEY, "c"));
    expect(activeUrl()).toBe("/acme/issues?view=c");
    act(() => useActiveIssueViewStore.getState().setActive(KEY, null));
    expect(activeUrl()).toBe("/acme/issues");
    expect(history()).toEqual(["/acme/issues"]);
  });

  it("keeps the tab's open view when its page is reached without one", () => {
    useTabStore.getState().navigateActiveSession("/acme/issues/MUL-1");
    useActiveIssueViewStore.getState().setActive(KEY, "a");
    useTabStore.getState().navigateActiveSession("/acme/issues");

    render(<ActiveIssuesPage />);

    expect(shown()).toBe("a");
    expect(activeUrl()).toBe("/acme/issues?view=a");
  });

  it("follows the URL back to the entry before a view was opened", () => {
    render(<ActiveIssuesPage />);
    act(() => {
      useActiveIssueViewStore.getState().setActive(KEY, "a");
      useTabStore.getState().navigateActiveSession("/acme/issues?view=a");
    });
    expect(shown()).toBe("a");

    act(() => useTabStore.getState().goBack());

    expect(activeUrl()).toBe("/acme/issues");
    expect(shown()).toBe("none");
  });

  it("leaves the outgoing page's view alone while the tab moves to another page", () => {
    // The tab's URL changes before the router swaps pages, so My issues is
    // still mounted when the URL already names the Issues page's view.
    useTabStore.getState().navigateActiveSession("/acme/my-issues?view=m");
    render(
      <DesktopNavigationProvider>
        <IssueViewUrlSync scope={{ scope_type: "my" }}>
          <span />
        </IssueViewUrlSync>
      </DesktopNavigationProvider>,
    );
    expect(useActiveIssueViewStore.getState().active["ws-1:my"]).toBe("m");

    act(() => useTabStore.getState().navigateActiveSession("/acme/issues?view=w"));

    expect(useActiveIssueViewStore.getState().active["ws-1:my"]).toBe("m");
    expect(activeUrl()).toBe("/acme/issues?view=w");
  });

  it("adopts the next project's view once the reused page renders that project", () => {
    const sync = (id: string) => (
      <DesktopNavigationProvider>
        <IssueViewUrlSync scope={{ scope_type: "project", scope_id: id }}>
          <span />
        </IssueViewUrlSync>
      </DesktopNavigationProvider>
    );
    const active = () => useActiveIssueViewStore.getState().active;
    useTabStore.getState().navigateActiveSession("/acme/projects/p1?view=a");
    const { rerender } = render(sync("p1"));

    act(() => useTabStore.getState().navigateActiveSession("/acme/projects/p2?view=b"));
    expect(active()["ws-1:project:p1"]).toBe("a");

    rerender(sync("p2"));
    expect(active()["ws-1:project:p1"]).toBe("a");
    expect(active()["ws-1:project:p2"]).toBe("b");
    expect(activeUrl()).toBe("/acme/projects/p2?view=b");
  });
});
