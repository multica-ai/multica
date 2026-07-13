import { beforeEach, describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { useEffect } from "react";
import {
  useIssueViewStoreFactory,
  type IssueViewStoreFactory,
  type PersistedIssueViewState,
} from "@multica/core/issues/stores";
import { SurfaceViewStoreProvider } from "./surface-view-store-provider";
import { useTabStore, getActiveTab, getTabById } from "@/stores/tab-store";

const viewState = (viewMode: string) =>
  ({ viewMode }) as unknown as PersistedIssueViewState;

function FactoryProbe({
  onFactory,
}: {
  onFactory: (factory: IssueViewStoreFactory) => void;
}) {
  const factory = useIssueViewStoreFactory();
  useEffect(() => {
    if (factory) onFactory(factory);
  }, [factory, onFactory]);
  return null;
}

function mountFactory(tabId: string): IssueViewStoreFactory {
  let captured: IssueViewStoreFactory | null = null;
  render(
    <SurfaceViewStoreProvider tabId={tabId}>
      <FactoryProbe
        onFactory={(f) => {
          captured = f;
        }}
      />
    </SurfaceViewStoreProvider>,
  );
  if (!captured) throw new Error("view-store factory was not injected");
  return captured;
}

beforeEach(() => {
  useTabStore.getState().reset();
});

describe("SurfaceViewStoreProvider", () => {
  it("hydrates a session-backed store from tab.viewState", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const tabId = getActiveTab(useTabStore.getState())!.id;
    store.updateTabViewState(tabId, "s1", viewState("list"));

    const factory = mountFactory(tabId);
    expect(factory("s1").getState().viewMode).toBe("list");
  });

  it("writes store changes back into tab.viewState (owner = tab session)", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const tabId = getActiveTab(useTabStore.getState())!.id;

    const factory = mountFactory(tabId);
    factory("s1").getState().setViewMode("board");

    expect(
      getTabById(useTabStore.getState(), tabId)?.viewState?.s1?.viewMode,
    ).toBe("board");
  });

  it("returns the same store per surfaceKey after a write-back (no rebuild — risk 3)", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const tabId = getActiveTab(useTabStore.getState())!.id;

    const factory = mountFactory(tabId);
    const s1 = factory("s1");
    // A write-back re-renders the provider; the store must not be rebuilt/reset.
    s1.getState().setViewMode("gantt");
    expect(factory("s1")).toBe(s1);
    expect(s1.getState().viewMode).toBe("gantt");
  });

  it("keeps two tabs' view state independent for the same surfaceKey (risk 4)", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const tabA = getActiveTab(useTabStore.getState())!.id;
    const tabB = store.addTab("/acme/issues", "Issues", "ListTodo");

    const storeA = mountFactory(tabA)("workspace:all");
    const storeB = mountFactory(tabB)("workspace:all");

    storeA.getState().setViewMode("board");
    storeB.getState().setViewMode("gantt");

    expect(storeA).not.toBe(storeB);
    expect(
      getTabById(useTabStore.getState(), tabA)?.viewState?.["workspace:all"]
        ?.viewMode,
    ).toBe("board");
    expect(
      getTabById(useTabStore.getState(), tabB)?.viewState?.["workspace:all"]
        ?.viewMode,
    ).toBe("gantt");
  });
});
