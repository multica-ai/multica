import { render, screen } from "@testing-library/react";
import {
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table";
import { describe, expect, it, vi } from "vitest";
import { DataTable } from "@multica/ui/components/ui/data-table";

// jsdom has no layout, so the real virtualizer sees a zero-height viewport and
// renders nothing. The stand-in keeps measureElement pointed at a spy, which is
// the hook rows have to reach for their height to be corrected.
const measured = vi.hoisted(() => [] as (HTMLElement | null)[]);
vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        index,
        key: index,
        start: index * 41,
        end: (index + 1) * 41,
        size: 41,
      })),
    getTotalSize: () => count * 41,
    measureElement: (element: HTMLElement | null) => measured.push(element),
  }),
}));

type Row = {
  title: string;
  status: string;
};

const columns: ColumnDef<Row>[] = [
  { accessorKey: "title", header: "Issue" },
  { accessorKey: "status", header: "Status" },
];

function PinnedTable() {
  const table = useReactTable({
    data: [{ title: "Pinned title", status: "In progress" }],
    columns,
    getCoreRowModel: getCoreRowModel(),
    state: { columnPinning: { left: ["title"], right: [] } },
  });

  return <DataTable table={table} />;
}

function VirtualizedTable() {
  const table = useReactTable({
    data: [
      { title: "First", status: "todo" },
      { title: "Second", status: "done" },
    ],
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

  return (
    <DataTable
      table={table}
      virtualizeRows
      // Exercises both paths: a standard row and one the caller builds.
      renderRow={(row) =>
        row.index === 1 ? (
          <tr data-slot="custom-row">
            <td colSpan={2}>Group header</td>
          </tr>
        ) : null
      }
    />
  );
}

describe("DataTable pinned columns", () => {
  it("tags every virtualized row so it reports its own height", () => {
    render(<VirtualizedTable />);

    // Group headers and end-of-column footers are shorter than a data row, so
    // one estimate for all of them drifts from the real layout as they
    // accumulate. Both the standard rows and the ones renderRow builds have to
    // carry data-index and the measuring ref for the virtualizer to correct it.
    const rows = document.querySelectorAll("tbody tr[data-index]");
    expect(rows.length).toBe(2);
    expect([...rows].map((row) => row.getAttribute("data-index"))).toEqual([
      "0",
      "1",
    ]);
    expect(document.querySelector('tbody tr[data-slot="custom-row"]')).toHaveAttribute(
      "data-index",
    );
    expect(measured.filter(Boolean).length).toBe(2);
  });

  it("keeps the header strip off the compositor's blur path", () => {
    render(<PinnedTable />);

    const strip = document.querySelector("thead")!;
    // A backdrop-filter on a sticky element with content scrolling beneath it
    // recomputes its backdrop every frame while virtualisation churns the rows
    // under it, and the strip flickers black (electron#12906). The mix resolves
    // to the same colour bg-muted/30 composited to.
    expect(strip).toHaveClass(
      "bg-[color-mix(in_oklab,var(--muted)_30%,var(--background))]",
    );
    expect(strip).not.toHaveClass("backdrop-blur");
  });

  it("uses opaque backgrounds so scrolled columns cannot show through", () => {
    render(<PinnedTable />);

    const titleHeader = screen.getByRole("columnheader", { name: /^Issue/ });
    const titleCell = screen.getByRole("cell", { name: "Pinned title" });
    const statusCell = screen.getByRole("cell", { name: "In progress" });

    expect(titleHeader).toHaveClass(
      "bg-[color-mix(in_oklab,var(--muted)_30%,var(--background))]",
    );
    expect(titleHeader).not.toHaveClass("bg-muted/30", "backdrop-blur");
    expect(titleCell).toHaveClass(
      "bg-background",
      "group-hover:bg-[color-mix(in_oklab,var(--muted)_50%,var(--background))]",
    );
    expect(titleCell).not.toHaveClass("group-hover:bg-muted/50");
    expect(titleCell).toHaveStyle({ position: "sticky", zIndex: 1 });
    expect(statusCell).not.toHaveStyle({ position: "sticky" });
  });
});
