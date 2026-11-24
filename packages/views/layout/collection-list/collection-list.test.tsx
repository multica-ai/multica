import { screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import {
  ScrollRestorationProvider,
  type ScrollRestorationAdapter,
} from "../../platform";
import { renderWithI18n } from "../../test/i18n";
import { CollectionList, type CollectionColumn } from "./collection-list";

type Row = { id: string; name: string };

const columns: CollectionColumn<Row>[] = [
  { key: "name", header: "Name", cell: (row) => row.name },
];

const navigation: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/agents",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => path,
};

function renderList({
  rowCount,
  adapter,
  scrollKey,
}: {
  rowCount: number;
  adapter: ScrollRestorationAdapter;
  scrollKey?: string;
}) {
  const rows = Array.from({ length: rowCount }, (_, i) => ({
    id: `row-${i}`,
    name: `Agent ${i}`,
  }));
  return renderWithI18n(
    <NavigationProvider value={navigation}>
      <ScrollRestorationProvider adapter={adapter}>
        <CollectionList
          label="Agents"
          columns={columns}
          rows={rows}
          getRowId={(row) => row.id}
          scrollKey={scrollKey}
        />
      </ScrollRestorationProvider>
    </NavigationProvider>,
  );
}

describe("CollectionList scroll restoration", () => {
  // jsdom has no Element.scrollTo, which the list's virtualizer calls on the
  // scroller when it attaches; without it that call would silently vanish.
  beforeEach(() => {
    HTMLElement.prototype.scrollTo = function (this: HTMLElement, options?: ScrollToOptions | number) {
      if (typeof options === "object" && options.top !== undefined) this.scrollTop = options.top;
    } as HTMLElement["scrollTo"];
  });
  afterEach(() => {
    delete (HTMLElement.prototype as Partial<HTMLElement>).scrollTo;
  });

  it("brings the list back to where it was scrolled when it mounts again", () => {
    const get = vi.fn((key: string) =>
      key === "collection" ? { top: 640, height: 2000 } : undefined,
    );
    renderList({ rowCount: 3, adapter: { get } });

    const grid = screen.getByRole("grid", { name: "Agents" });
    const scroller = grid.parentElement!;
    // Marked so the tab coordinator captures it on the way out …
    expect(scroller.getAttribute("data-tab-scroll-root")).toBe("collection");
    // … and given the saved offset back when it attaches.
    expect(scroller.scrollTop).toBe(640);
  });

  it("keys the offset by scrollKey when a route swaps between lists", () => {
    const get = vi.fn((key: string) =>
      key === "wakeups" ? { top: 320, height: 2000 } : undefined,
    );
    renderList({ rowCount: 3, adapter: { get }, scrollKey: "wakeups" });

    const scroller = screen.getByRole("grid", { name: "Agents" }).parentElement!;
    expect(scroller.getAttribute("data-tab-scroll-root")).toBe("wakeups");
    expect(scroller.scrollTop).toBe(320);
  });

  // Long lists virtualize, and the saved offset is assigned before the
  // virtualizer has measured the viewport (jsdom never gives it one). The rows
  // it has not windowed yet must already take up their height, or the browser
  // clamps the restored offset back to the top.
  it("spans every row of a long list before the viewport is measured", () => {
    renderList({ rowCount: 200, adapter: { get: () => undefined } });

    const grid = screen.getByRole("grid", { name: "Agents" });
    const spacers = grid.querySelectorAll<HTMLElement>(":scope > [aria-hidden]");
    const height = [...spacers].reduce(
      (sum, el) => sum + Number.parseFloat(el.style.height || "0"),
      0,
    );
    expect(height).toBe(200 * 36);
  });
});
