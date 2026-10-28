import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import { renderWithI18n } from "../test/i18n";
import { compareCells, TablePreview } from "./table-preview";

// The body virtualizes; render every row so DOM order reflects sort order.
vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        index,
        key: index,
        start: index * 37,
        end: (index + 1) * 37,
        size: 37,
      })),
    getTotalSize: () => count * 37,
    measureElement: () => {},
  }),
}));

function columnValues(columnId: string): string[] {
  return Array.from(
    document.querySelectorAll(`tbody td[data-column-id='${columnId}']`),
    (cell) => cell.textContent ?? "",
  );
}

function header(name: string): HTMLElement {
  return screen.getByRole("button", { name });
}

describe("TablePreview", () => {
  it("takes the first record as the header and numbers the rows", () => {
    renderWithI18n(<TablePreview text={"name,count\nalpha,3\nbeta,10\n"} delimiter="," />);

    expect(header("name")).toBeTruthy();
    expect(header("count")).toBeTruthy();
    expect(columnValues("c0")).toEqual(["alpha", "beta"]);
    expect(columnValues("__row")).toEqual(["1", "2"]);
    expect(screen.getByText(/2 rows/)).toHaveTextContent("2 rows · 2 columns");
  });

  it("sorts numbers as numbers, then descending, then back to file order", () => {
    renderWithI18n(
      <TablePreview text={"item,qty\na,10\nb,9\nc,\nd,100\n"} delimiter="," />,
    );
    const th = () => document.querySelector("th[data-column-id='c1']");

    fireEvent.click(header("qty"));
    expect(columnValues("c1")).toEqual(["9", "10", "100", ""]);
    expect(th()).toHaveAttribute("aria-sort", "ascending");

    fireEvent.click(header("qty"));
    // Empty cells stay last whichever way the column sorts.
    expect(columnValues("c1")).toEqual(["100", "10", "9", ""]);
    expect(th()).toHaveAttribute("aria-sort", "descending");

    fireEvent.click(header("qty"));
    expect(columnValues("c1")).toEqual(["10", "9", "", "100"]);
    expect(th()).not.toHaveAttribute("aria-sort");
  });

  it("keeps each row's file position in the row-number column while sorted", () => {
    renderWithI18n(<TablePreview text={"k\nb\na\n"} delimiter="," />);
    fireEvent.click(header("k"));
    expect(columnValues("c0")).toEqual(["a", "b"]);
    expect(columnValues("__row")).toEqual(["2", "1"]);
  });

  it("right-aligns an all-number column", () => {
    renderWithI18n(<TablePreview text={"name,qty\na,1\nb,2.5\n"} delimiter="," />);
    const qty = document.querySelector("tbody td[data-column-id='c1'] span");
    const name = document.querySelector("tbody td[data-column-id='c0'] span");
    expect(qty).toHaveClass("text-right");
    expect(name).not.toHaveClass("text-right");
  });

  it("reads a semicolon-separated CSV and a TSV", () => {
    const { unmount } = renderWithI18n(<TablePreview text={"a;b\n1;2\n"} delimiter="," />);
    expect(columnValues("c1")).toEqual(["2"]);
    unmount();

    renderWithI18n(<TablePreview text={"a\tb,c\n1\t2,3\n"} delimiter={"\t"} />);
    expect(header("b,c")).toBeTruthy();
    expect(columnValues("c1")).toEqual(["2,3"]);
  });

  it("shows the file's column names as written", () => {
    renderWithI18n(<TablePreview text={"userId,created_at\n1,2\n"} delimiter="," />);
    const title = within(header("userId")).getByText("userId");
    expect(title.closest("button")).toHaveClass("normal-case");
  });

  it("says so when there are no rows", () => {
    renderWithI18n(<TablePreview text={"only,a,header\n"} delimiter="," />);
    expect(screen.getByText("No rows")).toBeTruthy();
    expect(screen.getByText(/0 rows/)).toHaveTextContent("0 rows · 3 columns");
  });

  it("uses the localized counts", () => {
    renderWithI18n(<TablePreview text={"a,b\n1,2\n"} delimiter="," />, { locale: "zh-Hans" });
    expect(screen.getByText(/1 行/)).toHaveTextContent("1 行 · 2 列");
  });
});

describe("compareCells", () => {
  it("orders numbers numerically and ahead of text", () => {
    expect(["b", "10", "9", "a"].sort(compareCells)).toEqual(["9", "10", "a", "b"]);
  });

  it("orders text naturally and case-insensitively", () => {
    expect(["file10", "File2", "file1"].sort(compareCells)).toEqual([
      "file1",
      "File2",
      "file10",
    ]);
  });
});
