// @vitest-environment node
import { describe, expect, it } from "vitest";
import { locateInColumns, resolveIssueClick } from "./peek-context";

describe("locateInColumns", () => {
  const columns = [["a", "b", "c"], [], ["d"]];

  it("reports the 1-based position and neighbours inside the issue's column", () => {
    expect(locateInColumns(columns, "b")).toMatchObject({
      index: 2,
      total: 3,
      prevId: "a",
      nextId: "c",
    });
  });

  it("has no neighbour past either end of the column", () => {
    expect(locateInColumns(columns, "a")).toMatchObject({ prevId: null, nextId: "b" });
    expect(locateInColumns(columns, "c")).toMatchObject({ prevId: "b", nextId: null });
    expect(locateInColumns(columns, "d")).toMatchObject({ index: 1, total: 1, prevId: null, nextId: null });
  });

  it("never steps across columns with J / K", () => {
    expect(locateInColumns(columns, "c")?.nextId).toBeNull();
  });

  it("steps sideways to the nearest non-empty column, at the same row or its last card", () => {
    // columns: [a b c] [] [d]
    expect(locateInColumns(columns, "c")).toMatchObject({ leftId: null, rightId: "d" });
    expect(locateInColumns(columns, "d")).toMatchObject({ leftId: "a", rightId: null });
    const wide = [["x1", "x2"], ["y1", "y2", "y3"]];
    expect(locateInColumns(wide, "y3")?.leftId).toBe("x2");
    expect(locateInColumns(wide, "x2")?.rightId).toBe("y2");
  });

  it("is null for an issue outside every column or without published columns", () => {
    expect(locateInColumns(columns, "zzz")).toBeNull();
    expect(locateInColumns(null, "a")).toBeNull();
    expect(locateInColumns(columns, null)).toBeNull();
  });
});

describe("resolveIssueClick", () => {
  const plain = { button: 0, shiftKey: false, metaKey: false, ctrlKey: false, altKey: false };
  it("opens the preferred target on a plain click and the other one on Shift", () => {
    expect(resolveIssueClick(plain, "page")).toBe("page");
    expect(resolveIssueClick(plain, "peek")).toBe("peek");
    expect(resolveIssueClick({ ...plain, shiftKey: true }, "page")).toBe("peek");
    expect(resolveIssueClick({ ...plain, shiftKey: true }, "peek")).toBe("page");
  });
  it.each([{ metaKey: true }, { ctrlKey: true }, { altKey: true }, { button: 1 }, { button: 2 }])(
    "leaves modified clicks native: %j", (modifier) => {
      for (const openMode of ["page", "peek"] as const) {
        expect(resolveIssueClick({ ...plain, ...modifier }, openMode)).toBeNull();
        expect(resolveIssueClick({ ...plain, shiftKey: true, ...modifier }, openMode)).toBeNull();
      }
    },
  );
});
