import { useMemo } from "react";
import { act, cleanup, render } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
import { ScrollRestorationProvider, useRestoredScrollRef } from "@multica/views/platform";
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
import { __resetTabCoordinatorForTests, createScrollRestorationAdapter, initTabCoordinator, registerActiveHostElement } from "./tab-coordinator";

function ScrollRoot({ name }: { name: string }) {
  return <div ref={useRestoredScrollRef(name)} data-tab-scroll-root={name} />;
}

function Surface({ mode }: { mode: string }) {
  const view = useActiveIssueViewStore(s => s.active["ws-1:workspace"] ?? "default");
  // Match IssueSurface's remount on saved-view identity changes.
  return <div key={view}><ScrollRoot key={mode} name={mode} /></div>;
}

function Page({ mode }: { mode: string }) {
  const tabId = useTabStore(s => getActiveTab(s)!.id);
  const adapter = useMemo(() => createScrollRestorationAdapter(tabId), [tabId]);
  return <DesktopNavigationProvider>
    <div ref={registerActiveHostElement}>
      <ScrollRestorationProvider adapter={adapter}>
        <IssueViewUrlSync scope={{ scope_type: "workspace" }}>
          <Surface mode={mode} />
        </IssueViewUrlSync>
      </ScrollRestorationProvider>
    </div>
  </DesktopNavigationProvider>;
}

afterEach(() => {
  cleanup();
  __resetIssueViewTabsForTests();
  __resetTabCoordinatorForTests();
});

it("captures layout switches after returning from a saved view to All issues", () => {
  Object.defineProperty(window, "desktopAPI", {
    configurable: true,
    value: { runtimeConfig: { ok: true, config: { appUrl: "https://app.example" } } },
  });
  __resetTabCoordinatorForTests();
  __resetIssueViewTabsForTests();
  useActiveIssueViewStore.setState({ active: { "ws-1:workspace": "saved-a" } });
  useTabStore.getState().reset();
  useTabStore.getState().switchWorkspace("acme");
  useTabStore.getState().navigateActiveSession("/acme/issues?view=saved-a");
  initTabCoordinator();
  initIssueViewTabs();

  const rendered = render(<Page mode="board" />);
  const board = () => rendered.container.querySelector<HTMLElement>('[data-tab-scroll-root="board"]')!;
  board().scrollLeft = 640;
  // The saved-view title menu's Back to All issues action only clears the
  // active view. IssueViewUrlSync removes ?view= in a later passive effect.
  act(() => useActiveIssueViewStore.getState().setActive("ws-1:workspace", null));
  expect(getActiveTab(useTabStore.getState())!.url).toBe("/acme/issues");
  expect(board().scrollLeft).toBe(640);
  board().scrollLeft = 0;

  rendered.rerender(<Page mode="list" />);
  rendered.rerender(<Page mode="board" />);
  expect(board().scrollLeft).toBe(0);
});
