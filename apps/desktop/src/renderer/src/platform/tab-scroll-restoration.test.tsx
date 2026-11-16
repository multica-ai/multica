import { act, cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  ScrollRestorationProvider,
  useRestoredScrollRef,
} from "@multica/views/platform";
import { getActiveTab, useTabStore } from "@/stores/tab-store";
import {
  __resetTabCoordinatorForTests,
  createScrollRestorationAdapter,
  initTabCoordinator,
  registerActiveHostElement,
} from "./tab-coordinator";

function ScrollRoot({ name, marked = true }: { name: string; marked?: boolean }) {
  const ref = useRestoredScrollRef(name);
  return <div ref={ref} data-tab-scroll-root={marked ? name : undefined} />;
}

describe("same-route scroll restoration", () => {
  beforeEach(() => {
    __resetTabCoordinatorForTests();
    useTabStore.getState().reset();
    useTabStore.getState().switchWorkspace("acme");
    initTabCoordinator();
  });

  afterEach(() => {
    cleanup();
    __resetTabCoordinatorForTests();
  });

  it("does not capture a peek scroller that omits the marker", () => {
    const tabId = getActiveTab(useTabStore.getState())!.id;
    const rendered = render(
      <ScrollRestorationProvider adapter={createScrollRestorationAdapter(tabId)}>
        <ScrollRoot name="main" marked={false} />
      </ScrollRestorationProvider>,
    );
    rendered.container.firstElementChild!.scrollTop = 500;
    rendered.unmount();

    expect(getActiveTab(useTabStore.getState())!.memento.scroll).toEqual({});
  });

  it.each([
    { left: 420, top: 120, columnTop: 240 },
    { left: 0, top: 120, columnTop: 240 },
    { left: 0, top: 0, columnTop: 0 },
  ])("captures the latest offsets on ref detach: %o", ({ left, top, columnTop }) => {
    const store = useTabStore.getState();
    const tabId = getActiveTab(store)!.id;
    const adapter = createScrollRestorationAdapter(tabId);
    store.commitScrollMemento(tabId, "/acme/issues", {
      board: { top: 80, left: 640, height: 1000 },
      "board:status:todo": { top: 600, height: 2000 },
    });
    const view = (mode: string) => (
      <ScrollRestorationProvider adapter={adapter}>
        {mode === "board" ? (
          <div key="board">
            <ScrollRoot name="board" />
            <ScrollRoot name="board:status:todo" />
          </div>
        ) : <ScrollRoot key="list" name="list" />}
      </ScrollRestorationProvider>
    );
    const rendered = render(view("board"));
    const board = rendered.container.querySelector<HTMLElement>('[data-tab-scroll-root="board"]')!;
    const column = rendered.container.querySelector<HTMLElement>('[data-tab-scroll-root="board:status:todo"]')!;
    board.scrollLeft = left;
    board.scrollTop = top;
    column.scrollTop = columnTop;

    rendered.rerender(view("list"));
    rendered.rerender(view("board"));

    const restoredBoard = rendered.container.querySelector<HTMLElement>('[data-tab-scroll-root="board"]')!;
    const restoredColumn = rendered.container.querySelector<HTMLElement>('[data-tab-scroll-root="board:status:todo"]')!;
    expect(restoredBoard.scrollLeft).toBe(left);
    expect(restoredBoard.scrollTop).toBe(top);
    expect(restoredColumn.scrollTop).toBe(columnTop);
  });

  it("keeps unmounted views when capturing a different view on tab switch", () => {
    const store = useTabStore.getState();
    const tabId = getActiveTab(store)!.id;
    store.commitScrollMemento(tabId, "/acme/issues", {
      board: { top: 0, left: 640, height: 1000 },
      swimlane: { top: 300, height: 2000 },
    });
    const adapter = createScrollRestorationAdapter(tabId);
    const rendered = render(
      <ScrollRestorationProvider adapter={adapter}>
        <ScrollRoot name="list" />
      </ScrollRestorationProvider>,
    );
    registerActiveHostElement(rendered.container);
    rendered.container.querySelector<HTMLElement>("[data-tab-scroll-root]")!.scrollTop = 150;

    act(() => { store.openTab("/acme/projects", "Projects", { activate: true }); });

    const saved = useTabStore.getState().byWorkspace.acme.tabs.find(t => t.id === tabId)!.memento.scroll;
    expect(saved["/acme/issues::board"]?.left).toBe(640);
    expect(saved["/acme/issues::swimlane"]?.top).toBe(300);
    expect(saved["/acme/issues::list"]?.top).toBe(150);
  });

  it.each(["?view=saved-a", "#comment-a"])(
    "keeps capturing after same-path URL synchronization from %s",
    (suffix) => {
      const store = useTabStore.getState();
      store.navigateActiveSession(`/acme/issues${suffix}`);
      const tabId = getActiveTab(useTabStore.getState())!.id;
      const adapter = createScrollRestorationAdapter(tabId);
      const rendered = render(
        <ScrollRestorationProvider adapter={adapter}>
          <ScrollRoot name="board" />
        </ScrollRestorationProvider>,
      );
      registerActiveHostElement(rendered.container);
      const board = rendered.container.firstElementChild as HTMLElement;
      board.scrollLeft = 640;

      act(() => store.navigateActiveSession("/acme/issues", { replace: true }));
      expect(adapter.get("board")?.left).toBe(640);
      board.scrollLeft = 0;
      rendered.unmount();

      expect(adapter.get("board")).toBeUndefined();
    },
  );

  it.each(["tab", "route", "reload"])("keeps the pre-unmount capture when %s changes before detach", (change) => {
    const store = useTabStore.getState();
    const tabId = getActiveTab(store)!.id;
    const adapter = createScrollRestorationAdapter(tabId);
    const rendered = render(
      <ScrollRestorationProvider adapter={adapter}>
        <ScrollRoot name="list" />
      </ScrollRestorationProvider>,
    );
    registerActiveHostElement(rendered.container);
    const list = rendered.container.querySelector<HTMLElement>("[data-tab-scroll-root]")!;
    list.scrollTop = 450;

    act(() => {
      if (change === "tab") store.openTab("/acme/projects", "Projects", { activate: true });
      else if (change === "route") store.navigateActiveSession("/acme/projects");
      else store.reloadActiveTab();
    });
    // A detached virtualized scroller can collapse during teardown. The
    // Coordinator already captured its outgoing DOM before the store changed.
    list.scrollTop = 0;
    rendered.unmount();

    const saved = useTabStore.getState().byWorkspace.acme.tabs.find(t => t.id === tabId)!.memento.scroll;
    expect(saved["/acme/issues::list"]?.top).toBe(450);
    expect(saved["/acme/projects::list"]).toBeUndefined();
    if (change === "tab") expect(getActiveTab(useTabStore.getState())!.memento.scroll).toEqual({});
  });
});
