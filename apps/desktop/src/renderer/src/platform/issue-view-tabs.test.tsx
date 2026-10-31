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
});
