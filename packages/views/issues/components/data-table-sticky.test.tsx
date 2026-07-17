import { render, screen } from "@testing-library/react";
import {
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from "@tanstack/react-table";
import { describe, expect, it } from "vitest";
import { DataTable } from "@multica/ui/components/ui/data-table";

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

describe("DataTable pinned columns", () => {
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
