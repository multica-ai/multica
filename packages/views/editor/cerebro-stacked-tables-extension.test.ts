import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Table } from "@tiptap/extension-table";
import TableRow from "@tiptap/extension-table-row";
import TableHeader from "@tiptap/extension-table-header";
import TableCell from "@tiptap/extension-table-cell";
import { computeStackedTableSpecs } from "./cerebro-stacked-tables-extension";

const editors: Editor[] = [];

function makeEditor(content: string): Editor {
  const element = document.createElement("div");
  document.body.appendChild(element);
  const editor = new Editor({
    element,
    extensions: [StarterKit, Table, TableRow, TableHeader, TableCell],
    content,
  });
  editors.push(editor);
  return editor;
}

/** Read back the labels the extension would inject, in document order. */
function cellLabels(editor: Editor): string[] {
  return computeStackedTableSpecs(editor.state.doc).cells.map((c) => c.label);
}

afterEach(() => {
  while (editors.length) editors.pop()?.destroy();
});

describe("computeStackedTableSpecs", () => {
  it("tags a data table and labels each body cell with its column header", () => {
    const editor = makeEditor(
      "<table><tbody>" +
        "<tr><th>Tema</th><th>Testet?</th></tr>" +
        "<tr><td>Opus for dyr</td><td>Ja</td></tr>" +
        "<tr><td>Tools</td><td>Nej</td></tr>" +
        "</tbody></table>",
    );

    const { tables } = computeStackedTableSpecs(editor.state.doc);
    expect(tables).toHaveLength(1);
    // Two body rows × two columns, labelled by the header text.
    expect(cellLabels(editor)).toEqual([
      "Tema",
      "Testet?",
      "Tema",
      "Testet?",
    ]);
  });

  it("leaves a layout table (no header row) untouched", () => {
    const editor = makeEditor(
      "<table><tbody>" +
        "<tr><td>left</td><td>right</td></tr>" +
        "</tbody></table>",
    );

    const specs = computeStackedTableSpecs(editor.state.doc);
    expect(specs.tables).toHaveLength(0);
    expect(specs.cells).toHaveLength(0);
  });

  it("points each decoration at a real cell node in the document", () => {
    const editor = makeEditor(
      "<table><tbody>" +
        "<tr><th>Name</th></tr>" +
        "<tr><td>Sara</td></tr>" +
        "</tbody></table>",
    );

    const { cells } = computeStackedTableSpecs(editor.state.doc);
    expect(cells).toHaveLength(1);
    const cell = editor.state.doc.nodeAt(cells[0]!.from);
    expect(cell?.type.name).toBe("tableCell");
    expect(cell?.textContent).toBe("Sara");
  });
});
