import { describe, expect, it, beforeEach } from "vitest";
import { fireEvent, render } from "@testing-library/react";
import { useTabScrollRestore } from "./use-tab-scroll-restore";
import { useTabStore, getActiveTab } from "@/stores/tab-store";

function Harness({ tabId, path }: { tabId: string; path: string }) {
  const ref = useTabScrollRestore(tabId, path);
  return (
    <div ref={ref} style={{ display: "contents" }}>
      <div
        data-tab-scroll-root
        data-testid="scroller"
        style={{ height: 100, overflow: "auto" }}
      >
        <div style={{ height: 1000 }} />
      </div>
      <div
        data-tab-scroll-root="aside"
        data-testid="aside"
        style={{ height: 100, overflow: "auto" }}
      >
        <div style={{ height: 1000 }} />
      </div>
      <div data-testid="unmarked" style={{ height: 100, overflow: "auto" }}>
        <div style={{ height: 1000 }} />
      </div>
    </div>
  );
}

// The active tab is the only rendered subtree, so a tab switch is modeled as
// unmount (mounted=false) → remount (mounted=true). Offsets survive via the
// tab session in the store, not via the DOM.
function App({
  mounted,
  tabId,
  path,
}: {
  mounted: boolean;
  tabId: string;
  path: string;
}) {
  return mounted ? <Harness tabId={tabId} path={path} /> : null;
}

function setScroll(el: HTMLElement, top: number) {
  el.scrollTop = top;
  fireEvent.scroll(el);
}

let tabId: string;

beforeEach(() => {
  useTabStore.getState().reset();
  useTabStore.getState().switchWorkspace("acme");
  tabId = getActiveTab(useTabStore.getState())!.id;
});

describe("useTabScrollRestore", () => {
  it("restores scroll position across an unmount -> remount (tab switch)", () => {
    const { rerender, getByTestId } = render(
      <App mounted tabId={tabId} path="/acme/issues/1" />,
    );
    setScroll(getByTestId("scroller") as HTMLElement, 500);

    rerender(<App mounted={false} tabId={tabId} path="/acme/issues/1" />);
    rerender(<App mounted tabId={tabId} path="/acme/issues/1" />);

    expect((getByTestId("scroller") as HTMLElement).scrollTop).toBe(500);
  });

  it("restores multiple named scroll roots independently", () => {
    const { rerender, getByTestId } = render(
      <App mounted tabId={tabId} path="/acme/issues/1" />,
    );
    setScroll(getByTestId("scroller") as HTMLElement, 300);
    setScroll(getByTestId("aside") as HTMLElement, 150);

    rerender(<App mounted={false} tabId={tabId} path="/acme/issues/1" />);
    rerender(<App mounted tabId={tabId} path="/acme/issues/1" />);

    expect((getByTestId("scroller") as HTMLElement).scrollTop).toBe(300);
    expect((getByTestId("aside") as HTMLElement).scrollTop).toBe(150);
  });

  it("ignores scroll on elements without the data-tab-scroll-root marker", () => {
    const { rerender, getByTestId } = render(
      <App mounted tabId={tabId} path="/acme/issues/1" />,
    );
    setScroll(getByTestId("unmarked") as HTMLElement, 250);

    rerender(<App mounted={false} tabId={tabId} path="/acme/issues/1" />);
    rerender(<App mounted tabId={tabId} path="/acme/issues/1" />);

    expect((getByTestId("unmarked") as HTMLElement).scrollTop).toBe(0);
  });

  it("drops saved offsets when the tab path changes (intra-tab navigation)", () => {
    const { rerender, getByTestId } = render(
      <App mounted tabId={tabId} path="/acme/issues/1" />,
    );
    setScroll(getByTestId("scroller") as HTMLElement, 500);

    // Navigating within the tab swaps the active route — same marker key,
    // different page. The prior page's offset must not be restored.
    rerender(<App mounted tabId={tabId} path="/acme/issues/2" />);

    rerender(<App mounted={false} tabId={tabId} path="/acme/issues/2" />);
    rerender(<App mounted tabId={tabId} path="/acme/issues/2" />);

    expect((getByTestId("scroller") as HTMLElement).scrollTop).toBe(0);
  });
});
