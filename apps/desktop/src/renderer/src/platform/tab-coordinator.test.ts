import { beforeEach, describe, expect, it } from "vitest";
import {
  __resetTabCoordinatorForTests,
  createScrollRestorationAdapter,
  initTabCoordinator,
  registerActiveHostElement,
} from "./tab-coordinator";
import { getActiveTab, useTabStore } from "@/stores/tab-store";

describe("tab-coordinator external scroll sources", () => {
  beforeEach(() => {
    __resetTabCoordinatorForTests();
    useTabStore.getState().reset();
    useTabStore.getState().switchWorkspace("acme");
    initTabCoordinator();
  });

  function seedActiveTab(path: string, title: string): string {
    return useTabStore.getState().openTab(path, title, { activate: true });
  }

  it("captures external sources registered by the outgoing tab alongside DOM scroll roots", () => {
    const tabId = seedActiveTab("/acme/attachments/abc/preview", "Report");
    const adapter = createScrollRestorationAdapter(tabId);

    // Set up an outer DOM scroll root to confirm both paths merge.
    const host = document.createElement("div");
    const list = document.createElement("div");
    list.setAttribute("data-tab-scroll-root", "list");
    Object.defineProperty(list, "scrollTop", { value: 250, configurable: true });
    Object.defineProperty(list, "scrollHeight", { value: 1000, configurable: true });
    host.appendChild(list);
    registerActiveHostElement(host);

    // Register an iframe-style external source with a contentKey.
    adapter.registerExternalSource?.("html-iframe", {
      capture: () => ({ top: 512, height: 4000, contentKey: "v1hash" }),
    });

    // Switch to another tab — the Coordinator's store subscription fires
    // synchronously during the set() and captures the outgoing tab's state.
    const otherId = useTabStore
      .getState()
      .openTab("/acme/issues", "Issues", { activate: true });
    void otherId;

    const outgoing = useTabStore
      .getState()
      .byWorkspace.acme.tabs.find((t) => t.id === tabId)!;
    const scroll = outgoing.memento.scroll;
    // DOM root was captured (top > 0 rule).
    expect(scroll["/acme/attachments/abc/preview::list"]).toEqual({
      top: 250,
      height: 1000,
    });
    // External source was captured with its contentKey.
    expect(scroll["/acme/attachments/abc/preview::html-iframe"]).toEqual({
      top: 512,
      height: 4000,
      contentKey: "v1hash",
    });

    registerActiveHostElement(null);
  });

  it("writes external-source entries even at top:0 so a contentKey change clears stale offsets", () => {
    const tabId = seedActiveTab("/acme/attachments/abc/preview", "Report");
    const adapter = createScrollRestorationAdapter(tabId);
    registerActiveHostElement(document.createElement("div"));

    // Positive offset first.
    adapter.registerExternalSource?.("html-iframe", {
      capture: () => ({ top: 800, height: 5000, contentKey: "old" }),
    });
    useTabStore
      .getState()
      .openTab("/acme/issues", "Issues", { activate: true });

    // Re-activate the original tab, then switch again with new content but
    // no scroll yet (top:0). capture() should produce top:0 with the new
    // contentKey and the old offset must be replaced, not left behind.
    useTabStore.getState().setActiveTab(tabId);
    const adapter2 = createScrollRestorationAdapter(tabId);
    adapter2.registerExternalSource?.("html-iframe", {
      capture: () => ({ top: 0, height: 0, contentKey: "new" }),
    });
    useTabStore
      .getState()
      .openTab("/acme/issues", "Issues", { activate: true });

    const entry = useTabStore
      .getState()
      .byWorkspace.acme.tabs.find((t) => t.id === tabId)!.memento.scroll[
      "/acme/attachments/abc/preview::html-iframe"
    ];
    expect(entry).toEqual({ top: 0, height: 0, contentKey: "new" });

    registerActiveHostElement(null);
  });

  it("isolates external sources per tab: an incoming tab cannot read another tab's source", () => {
    const tabA = seedActiveTab("/acme/attachments/a/preview", "A");
    const adapterA = createScrollRestorationAdapter(tabA);
    adapterA.registerExternalSource?.("html-iframe", {
      capture: () => ({ top: 111, height: 1000, contentKey: "a" }),
    });

    const tabB = useTabStore
      .getState()
      .openTab("/acme/attachments/b/preview", "B", { activate: true });
    // tab B is now incoming; its adapter registers its own source. tab A's
    // capture must NOT be attributed to B when B leaves.
    const adapterB = createScrollRestorationAdapter(tabB);
    adapterB.registerExternalSource?.("html-iframe", {
      capture: () => ({ top: 222, height: 2000, contentKey: "b" }),
    });

    useTabStore
      .getState()
      .openTab("/acme/issues", "Issues", { activate: true });

    const tabs = useTabStore.getState().byWorkspace.acme.tabs;
    const aEntry = tabs.find((t) => t.id === tabA)!.memento.scroll[
      "/acme/attachments/a/preview::html-iframe"
    ];
    const bEntry = tabs.find((t) => t.id === tabB)!.memento.scroll[
      "/acme/attachments/b/preview::html-iframe"
    ];
    expect(aEntry).toEqual({ top: 111, height: 1000, contentKey: "a" });
    expect(bEntry).toEqual({ top: 222, height: 2000, contentKey: "b" });
  });

  it("get() on the per-tab adapter only returns entries when that tab is active", () => {
    const tabId = seedActiveTab("/acme/attachments/abc/preview", "Report");
    const adapter = createScrollRestorationAdapter(tabId);
    registerActiveHostElement(document.createElement("div"));
    adapter.registerExternalSource?.("html-iframe", {
      capture: () => ({ top: 321, height: 900, contentKey: "v" }),
    });
    useTabStore
      .getState()
      .openTab("/acme/issues", "Issues", { activate: true });

    // While another tab is active, get() returns undefined even though the
    // entry exists in the memento (prevents a stale view pulling another
    // tab's offset).
    expect(adapter.get("html-iframe")).toBeUndefined();

    useTabStore.getState().setActiveTab(tabId);
    expect(getActiveTab(useTabStore.getState())?.id).toBe(tabId);
    // A fresh adapter for the now-active tab sees the memento.
    const adapterAgain = createScrollRestorationAdapter(tabId);
    expect(adapterAgain.get("html-iframe")).toEqual({
      top: 321,
      height: 900,
      contentKey: "v",
    });
    registerActiveHostElement(null);
  });
});
