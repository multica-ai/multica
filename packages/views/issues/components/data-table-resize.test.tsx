import { act, render, screen } from "@testing-library/react";
import {
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type ColumnSizingState,
} from "@tanstack/react-table";
import * as React from "react";
import { describe, expect, it, vi } from "vitest";
import { DataTable } from "@multica/ui/components/ui/data-table";

type Row = { status: string; owner: string };

const columns: ColumnDef<Row>[] = [
  { accessorKey: "status", header: "Status", size: 150 },
  { accessorKey: "owner", header: "Owner", size: 90 },
];

function ResizableTable({
  onSizingChange,
  pinFirstColumn = false,
}: {
  onSizingChange: (sizing: ColumnSizingState) => void;
  pinFirstColumn?: boolean;
}) {
  const [columnSizing, setColumnSizing] = React.useState<ColumnSizingState>({});
  const table = useReactTable({
    data: [{ status: "In progress", owner: "Ada" }],
    columns,
    getCoreRowModel: getCoreRowModel(),
    state: {
      columnSizing,
      columnPinning: { left: pinFirstColumn ? ["status"] : [], right: [] },
    },
    columnResizeMode: "onChange",
    onColumnSizingChange: (updater) => {
      const next =
        typeof updater === "function" ? updater(columnSizing) : updater;
      setColumnSizing(next);
      onSizingChange(next);
    },
  });

  return <DataTable table={table} />;
}

// jsdom has no layout, so every <th> would measure 0 and each committed width
// would clamp to minSize. Pin them to the widths fixed table-layout would have
// stretched the columns to — both sit well above their configured size.
function stubRenderedWidth(element: Element, width: number) {
  element.getBoundingClientRect = () =>
    ({
      width,
      height: 32,
      top: 0,
      left: 0,
      right: width,
      bottom: 32,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    }) as DOMRect;
}

// jsdom's PointerEvent support is inconsistent; MouseEvent carries clientX and
// React dispatches by event type, so the handlers receive what they need.
// act() flushes the render that toggling the drag state schedules.
function pointer(target: EventTarget, type: string, clientX: number) {
  act(() => {
    target.dispatchEvent(
      new MouseEvent(type, { bubbles: true, cancelable: true, clientX }),
    );
  });
}

function setup() {
  const onSizingChange = vi.fn();
  render(<ResizableTable onSizingChange={onSizingChange} />);
  const handle = screen.getByRole("separator", {
    name: "Resize Status column",
  });
  stubRenderedWidth(
    document.querySelector('th[data-column-id="status"]')!,
    200,
  );
  stubRenderedWidth(document.querySelector('th[data-column-id="owner"]')!, 120);
  return { onSizingChange, handle };
}

describe("DataTable column resize", () => {
  it("commits nothing when the handle is clicked without dragging", () => {
    const { onSizingChange, handle } = setup();

    pointer(handle, "pointerdown", 100);
    pointer(window, "pointerup", 100);

    // The pre-fix code wrote the stretched render width here, permanently
    // pinning a column the user only brushed past.
    expect(onSizingChange).not.toHaveBeenCalled();
  });

  it("ignores pointer travel below the drag threshold", () => {
    const { onSizingChange, handle } = setup();

    pointer(handle, "pointerdown", 100);
    pointer(window, "pointermove", 103);
    pointer(window, "pointerup", 103);

    expect(onSizingChange).not.toHaveBeenCalled();
  });

  it("commits the rendered width plus the delta once dragging starts", () => {
    const { onSizingChange, handle } = setup();

    pointer(handle, "pointerdown", 100);
    pointer(window, "pointermove", 110);
    pointer(window, "pointerup", 110);

    expect(onSizingChange).toHaveBeenCalledWith({ status: 210, owner: 120 });
  });

  it("pins every column to its rendered width on the first drag frame", () => {
    const { onSizingChange, handle } = setup();

    pointer(handle, "pointerdown", 100);
    pointer(window, "pointermove", 110);

    // Without this, committing `status` would change how much leftover space
    // fixed table-layout has to share out, resizing `owner` mid-drag and
    // pushing the dragged edge away from the pointer.
    expect(onSizingChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ owner: 120 }),
    );

    // Measured once: later frames must not re-read a width that has moved.
    pointer(window, "pointermove", 300);
    expect(onSizingChange).toHaveBeenLastCalledWith({ status: 400, owner: 120 });
  });

  it("ends the gesture when the window loses focus mid-drag", () => {
    const { onSizingChange, handle } = setup();

    pointer(handle, "pointerdown", 100);
    pointer(window, "pointermove", 110);
    expect(onSizingChange).toHaveBeenLastCalledWith({ status: 210, owner: 120 });

    // Switching apps or releasing outside the window never delivers pointerup.
    act(() => {
      window.dispatchEvent(new Event("blur"));
    });
    pointer(window, "pointermove", 400);

    expect(onSizingChange).toHaveBeenLastCalledWith({ status: 210, owner: 120 });
  });

  it("publishes column widths as custom properties the cells reference", () => {
    setup();

    const table = document.querySelector("table")!;
    expect(table.style.getPropertyValue("--col-status-size")).toBe("150px");
    expect(table.style.getPropertyValue("--col-owner-size")).toBe("90px");

    // Cells must not carry a resolved number, or a resize would have to
    // re-render each one to update it.
    const cell = document.querySelector<HTMLElement>("tbody td")!;
    expect(cell.style.width).toBe("var(--col-status-size)");
  });

  it("stops re-rendering body cells while a column is being dragged", () => {
    const { handle } = setup();
    const cellRenders = () =>
      document.querySelectorAll('[data-slot="table-cell"]').length;

    const before = cellRenders();
    expect(before).toBeGreaterThan(0);

    pointer(handle, "pointerdown", 100);
    pointer(window, "pointermove", 200);

    // The memoized body keeps the same cells mounted; only the <table>'s
    // custom property changes, which the browser applies without React.
    expect(cellRenders()).toBe(before);
    expect(
      document.querySelector("table")!.style.getPropertyValue("--col-status-size"),
    ).toBe("300px");
  });

  it("sizes a column to its widest rendered cell on double-click", () => {
    const { onSizingChange, handle } = setup();

    // Fixed table-layout ignores content and the cells truncate their own
    // text, so the measurement lifts both constraints before reading. Stand in
    // for the layout jsdom will not perform.
    for (const cell of document.querySelectorAll<HTMLElement>(
      '[data-column-id="status"]',
    )) {
      cell.getBoundingClientRect = () =>
        ({ width: cell.tagName === "TH" ? 96 : 268 }) as DOMRect;
    }

    act(() => {
      handle.dispatchEvent(new MouseEvent("dblclick", { bubbles: true }));
    });

    // The widest cell wins, not the header and not the current width.
    expect(onSizingChange).toHaveBeenCalledWith(
      expect.objectContaining({ status: 268 }),
    );
  });

  it("leaves the column alone when double-clicked with nothing rendered", () => {
    const { onSizingChange, handle } = setup();

    for (const cell of document.querySelectorAll<HTMLElement>(
      '[data-column-id="status"]',
    )) {
      cell.getBoundingClientRect = () => ({ width: 0 }) as DOMRect;
    }

    act(() => {
      handle.dispatchEvent(new MouseEvent("dblclick", { bubbles: true }));
    });

    // A zero measurement means the column was never laid out; committing it
    // would collapse the column to its minimum for no reason.
    expect(onSizingChange).not.toHaveBeenCalled();
  });

  it("casts a shadow past the frozen columns only once scrolled sideways", () => {
    render(<ResizableTable onSizingChange={vi.fn()} pinFirstColumn />);

    const scroller = document.querySelector<HTMLElement>(".overflow-auto")!;
    const shadow = () =>
      document.querySelector<HTMLElement>(
        '[data-slot="data-table-pinned-shadow"]',
      );

    // The border between frozen and scrolling columns is permanent and says
    // where the boundary is. The shadow says something is passing underneath
    // right now, so at rest there is nothing for it to report.
    expect(shadow()).toBeNull();

    scroller.scrollLeft = 120;
    act(() => {
      scroller.dispatchEvent(new Event("scroll"));
    });
    expect(shadow()).not.toBeNull();

    scroller.scrollLeft = 0;
    act(() => {
      scroller.dispatchEvent(new Event("scroll"));
    });
    expect(shadow()).toBeNull();
  });

  it("places the shadow at the frozen block's measured edge", () => {
    render(<ResizableTable onSizingChange={vi.fn()} pinFirstColumn />);

    const scroller = document.querySelector<HTMLElement>(".overflow-auto")!;
    // Rendered widths diverge from configured ones under fixed table-layout,
    // so the boundary is read off the DOM rather than summed from column
    // sizes — summing put the shadow in the middle of visible content.
    const edge = document.querySelector<HTMLElement>(
      "thead th[data-pinned-edge]",
    )!;
    edge.getBoundingClientRect = () =>
      ({ right: 240, left: 0, width: 240 }) as DOMRect;
    scroller.getBoundingClientRect = () =>
      ({ left: 40, right: 900, width: 860 }) as DOMRect;

    scroller.scrollLeft = 120;
    act(() => {
      scroller.dispatchEvent(new Event("scroll"));
    });

    expect(
      document.querySelector<HTMLElement>(
        '[data-slot="data-table-pinned-shadow"]',
      )!.style.left,
    ).toBe("200px");
  });

  it("covers the viewport with a cursor layer only while dragging", () => {
    const { handle } = setup();
    const overlay = () =>
      document.querySelector('[data-slot="data-table-resize-overlay"]');

    expect(overlay()).toBeNull();

    pointer(handle, "pointerdown", 100);
    expect(overlay()).toHaveClass("cursor-col-resize");

    pointer(window, "pointerup", 100);
    expect(overlay()).toBeNull();
  });
});
