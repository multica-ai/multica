import { Activity } from "react";
import { describe, expect, it } from "vitest";
import { fireEvent, render } from "@testing-library/react";
import { useTabScrollRestore } from "./use-tab-scroll-restore";

function Harness({ path }: { path: string }) {
  const ref = useTabScrollRestore(path);
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
      <div
        data-testid="unmarked"
        style={{ height: 100, overflow: "auto" }}
      >
        <div style={{ height: 1000 }} />
      </div>
    </div>
  );
}

function App({ visible, path }: { visible: boolean; path: string }) {
  return (
    <Activity mode={visible ? "visible" : "hidden"}>
      <Harness path={path} />
    </Activity>
  );
}

function setScroll(el: HTMLElement, top: number) {
  el.scrollTop = top;
  fireEvent.scroll(el);
}

describe("useTabScrollRestore", () => {
  it("restores scroll position when a tab cycles through hidden -> visible", () => {
    const { rerender, getByTestId } = render(
      <App visible={true} path="/acme/issues/1" />,
    );
    const scroller = getByTestId("scroller") as HTMLElement;

    setScroll(scroller, 500);
    expect(scroller.scrollTop).toBe(500);

    // Simulate Activity hiding the subtree: layout would drop the offset.
    rerender(<App visible={false} path="/acme/issues/1" />);
    scroller.scrollTop = 0;

    rerender(<App visible={true} path="/acme/issues/1" />);
    expect(scroller.scrollTop).toBe(500);
  });

  it("restores multiple named scroll roots independently", () => {
    const { rerender, getByTestId } = render(
      <App visible={true} path="/acme/issues/1" />,
    );
    const main = getByTestId("scroller") as HTMLElement;
    const aside = getByTestId("aside") as HTMLElement;

    setScroll(main, 300);
    setScroll(aside, 150);

    rerender(<App visible={false} path="/acme/issues/1" />);
    main.scrollTop = 0;
    aside.scrollTop = 0;

    rerender(<App visible={true} path="/acme/issues/1" />);
    expect(main.scrollTop).toBe(300);
    expect(aside.scrollTop).toBe(150);
  });

  it("ignores scroll on elements without the data-tab-scroll-root marker", () => {
    const { rerender, getByTestId } = render(
      <App visible={true} path="/acme/issues/1" />,
    );
    const unmarked = getByTestId("unmarked") as HTMLElement;

    setScroll(unmarked, 250);

    rerender(<App visible={false} path="/acme/issues/1" />);
    unmarked.scrollTop = 0;

    rerender(<App visible={true} path="/acme/issues/1" />);
    expect(unmarked.scrollTop).toBe(0);
  });

  it("drops saved offsets when the tab path changes (intra-tab navigation)", () => {
    const { rerender, getByTestId } = render(
      <App visible={true} path="/acme/issues/1" />,
    );
    const scroller = getByTestId("scroller") as HTMLElement;

    setScroll(scroller, 500);

    // Navigating within the tab swaps the active route — same marker key,
    // different page. We should NOT restore the prior page's offset.
    rerender(<App visible={true} path="/acme/issues/2" />);
    scroller.scrollTop = 0;

    rerender(<App visible={false} path="/acme/issues/2" />);
    rerender(<App visible={true} path="/acme/issues/2" />);
    expect(scroller.scrollTop).toBe(0);
  });
});
