import { createElement } from "react";
import { afterEach, describe, expect, it } from "vitest";
import {
  columnWidthKey,
  readColumnWidths,
  readTableModel,
  tableMarkdownFromSource,
  tableToCsv,
  writeColumnWidths,
} from "./rich-table-model";

const h = createElement;

describe("readTableModel", () => {
  it("reads header and body cells from react-markdown's table children", () => {
    const children = [
      h("thead", { key: "h" }, "\n", h("tr", null, h("th", null, "Name"), h("th", null, "Note"))),
      "\n",
      h(
        "tbody",
        { key: "b" },
        h("tr", null, h("td", null, "a"), h("td", null, "see ", h("code", null, "cursor-agent"))),
        h("tr", null, h("td", null, "b"), h("td", null)),
      ),
    ];
    const model = readTableModel(children);
    expect(model.headerText).toEqual(["Name", "Note"]);
    expect(model.rowText).toEqual([
      ["a", "see cursor-agent"],
      ["b", ""],
    ]);
    expect(model.rows).toHaveLength(2);
  });
});

describe("tableMarkdownFromSource", () => {
  const source = [
    "intro MUL-1",
    "",
    "| Issue | Link | Note |",
    "| --- | --- | --- |",
    "| MUL-12 | https://example.com | ==hot== |",
    "| [https://x.com](https://x.com) | `<mark>hello</mark>` | `[a](b)` |",
  ].join("\n");
  const rendered = [
    "intro [MUL-1](mention://issue/MUL-1)",
    "",
    "| Issue | Link | Note |",
    "| --- | --- | --- |",
    "| [MUL-12](mention://issue/MUL-12) | [https://example.com](https://example.com) | <mark>hot</mark> |",
    "| [https://x.com](https://x.com) | `<mark>hello</mark>` | `[a](b)` |",
  ].join("\n");

  it("copies the table from the source, not the preprocessed copy", () => {
    const start = rendered.indexOf("| Issue");
    expect(tableMarkdownFromSource(source, rendered, start, rendered.length)).toBe(
      source.slice(source.indexOf("| Issue")),
    );
  });

  it("finds the table by its place among the tables", () => {
    const second = "| A | B |\n| - | - |\n| 1 | 2 |";
    const withTwo = (text: string) => `${text}\n\n${second}`;
    const start = withTwo(rendered).lastIndexOf("| A");
    expect(tableMarkdownFromSource(withTwo(source), withTwo(rendered), start, withTwo(rendered).length)).toBe(second);
  });

  it("drops a blockquote's or list item's markers so the copy is a table", () => {
    const quoted = "> | A | B |\n> | - | - |\n> | 1 | 2 |";
    expect(tableMarkdownFromSource(quoted, quoted, 2, quoted.length)).toBe("| A | B |\n| - | - |\n| 1 | 2 |");
    const listed = "- item\n\n  | A | B |\n  | - | - |\n  | 1 | 2 |";
    const start = listed.indexOf("| A");
    expect(tableMarkdownFromSource(listed, listed, start, listed.length)).toBe("| A | B |\n| - | - |\n| 1 | 2 |");
  });

  it("returns nothing without a source position", () => {
    expect(tableMarkdownFromSource("| a |", "| a |", undefined, 5)).toBe("");
  });
});

describe("tableToCsv", () => {
  it("quotes fields that contain a separator, quote or newline", () => {
    expect(tableToCsv(["a", "b"], [["1,2", 'say "hi"'], ["x\ny", "plain"]])).toBe(
      'a,b\n"1,2","say ""hi"""\n"x\ny",plain',
    );
  });
});

describe("column width storage", () => {
  afterEach(() => window.localStorage.clear());

  const model = { headerText: ["a", "b"], rowText: [["1", "2"]] };

  it("round-trips widths and forgets them on reset", () => {
    const key = columnWidthKey(model);
    writeColumnWidths(key, [120, 240]);
    expect(readColumnWidths(key, 2)).toEqual([120, 240]);
    writeColumnWidths(key, null);
    expect(readColumnWidths(key, 2)).toBeNull();
  });

  it("ignores widths stored for a different column count", () => {
    const key = columnWidthKey(model);
    writeColumnWidths(key, [120, 240]);
    expect(readColumnWidths(key, 3)).toBeNull();
  });

  it("keys a table by its content", () => {
    expect(columnWidthKey(model)).toBe(columnWidthKey({ ...model }));
    expect(columnWidthKey(model)).not.toBe(
      columnWidthKey({ headerText: ["a", "b"], rowText: [["1", "3"]] }),
    );
  });

  it("keeps only the most recently used tables", () => {
    for (let i = 0; i < 205; i++) writeColumnWidths(`k${i}`, [100]);
    const stored = JSON.parse(window.localStorage.getItem("multica:rich-table-widths") ?? "{}");
    expect(Object.keys(stored)).toHaveLength(200);
  });

  it("survives a corrupt entry", () => {
    window.localStorage.setItem("multica:rich-table-widths", "{not json");
    expect(readColumnWidths("k", 1)).toBeNull();
    writeColumnWidths("k", [90]);
    expect(readColumnWidths("k", 1)).toEqual([90]);
  });
});
