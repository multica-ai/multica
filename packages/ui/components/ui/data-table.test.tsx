import { render } from "@testing-library/react";
import {
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DataTable } from "./data-table";

type Row = { title: string };

const columns: ColumnDef<Row>[] = [{ accessorKey: "title", header: "Issue" }];

function Table({
  rowCount,
  scrollContainerRef,
  initialScrollTop,
}: {
  rowCount: number;
  scrollContainerRef?: (element: HTMLDivElement | null) => void;
  initialScrollTop?: number;
}) {
  const table = useReactTable({
    data: Array.from({ length: rowCount }, (_, i) => ({ title: `Row ${i}` })),
    columns,
    getCoreRowModel: getCoreRowModel(),
  });
  return (
    <DataTable
      table={table}
      virtualizeRows
      virtualRowHeight={40}
      scrollContainerRef={scrollContainerRef}
      initialScrollTop={initialScrollTop}
      scrollContainerAttributes={{ "data-tab-scroll-root": "table" }}
    />
  );
}

describe("DataTable scroll container", () => {
  // jsdom has no Element.scrollTo, which the row virtualizer calls on the
  // scroller when it attaches; without it that call would silently vanish.
  beforeEach(() => {
    HTMLElement.prototype.scrollTo = function (this: HTMLElement, options?: ScrollToOptions | number) {
      if (typeof options === "object" && options.top !== undefined) this.scrollTop = options.top;
    } as HTMLElement["scrollTo"];
  });
  afterEach(() => {
    delete (HTMLElement.prototype as Partial<HTMLElement>).scrollTo;
  });

  // The virtualizer scrolls its element to its initial offset when it
  // attaches, after the caller's ref has assigned the restored one.
  it("opens at initialScrollTop instead of being scrolled back to the top", () => {
    const { container } = render(
      <Table
        rowCount={500}
        initialScrollTop={2400}
        scrollContainerRef={(el) => {
          if (el) el.scrollTop = 2400;
        }}
      />,
    );

    const scroller = container.querySelector<HTMLElement>('[data-tab-scroll-root="table"]');
    expect(scroller?.scrollTop).toBe(2400);
  });

  it("hands the scroller to the caller and carries its data attributes", () => {
    const scrollContainerRef = vi.fn();
    const { container } = render(
      <Table rowCount={3} scrollContainerRef={scrollContainerRef} />,
    );

    const scroller = container.querySelector('[data-tab-scroll-root="table"]');
    expect(scroller).not.toBeNull();
    expect(scroller).toContainElement(container.querySelector("table"));
    expect(scrollContainerRef).toHaveBeenCalledWith(scroller);
  });

  // A restored offset is assigned when the scroller attaches, before the
  // virtualizer has measured the viewport (jsdom never gives it one). The
  // rows it has not windowed yet still have to take up their height then, or
  // the browser clamps the offset back to the top.
  it("spans every row before the virtualizer has measured the viewport", () => {
    const { container } = render(<Table rowCount={500} />);

    const spacer = container.querySelector<HTMLElement>(
      "tbody tr[aria-hidden] td",
    );
    expect(spacer?.style.height).toBe(`${500 * 40}px`);
  });
});
