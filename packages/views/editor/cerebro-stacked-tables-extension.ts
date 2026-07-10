// CEREBRO-PATCH(stacked-tables-editor): FIR-2482 — the editable Tiptap counterpart
// of the readonly rehype plugin in @multica/ui/markdown/cerebro-stacked-tables. That
// plugin only reaches rendered markdown (comments/chat/read docs); the EDITABLE editor
// (notes, issue descriptions, editable documents) renders real ProseMirror table nodes
// that never carry the `.data-table` class, so on mobile they fell back to the
// column-cram rule and shredded wide tables (Jesper's note screenshot).
//
// This extension mirrors the rehype logic as display-only ProseMirror decorations
// (no doc mutation, so the stored markdown is untouched): every table whose first row
// is a header row is tagged `.data-table`, and each body cell gets a `data-label`
// attribute copied from its column header. The existing card CSS in
// styles/cerebro-overrides.css then renders those as one card per row on mobile.

import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import type { Node as PMNode } from "@tiptap/pm/model";

export interface TableTag {
  from: number;
  to: number;
}

export interface CellLabel {
  from: number;
  to: number;
  label: string;
}

export interface StackedTableSpecs {
  tables: TableTag[];
  cells: CellLabel[];
}

/**
 * Pure, testable core: walk the doc and return the decoration positions for every
 * DATA table (one whose first row contains header cells). Layout tables (no header
 * row) are left untouched, matching the readonly rehype plugin.
 */
export function computeStackedTableSpecs(doc: PMNode): StackedTableSpecs {
  const tables: TableTag[] = [];
  const cells: CellLabel[] = [];

  doc.descendants((node, pos) => {
    if (node.type.name !== "table") return true;

    const headerRow = node.firstChild;
    if (!headerRow) return false;

    let hasHeader = false;
    const headers: string[] = [];
    headerRow.forEach((cell) => {
      if (cell.type.name === "tableHeader") hasHeader = true;
      headers.push(cell.textContent.trim());
    });
    // No header row → layout table, leave it (same rule as the rehype plugin).
    if (!hasHeader) return false;

    tables.push({ from: pos, to: pos + node.nodeSize });

    const tableContentStart = pos + 1;
    node.forEach((row, rowOffset, rowIndex) => {
      if (rowIndex === 0) return; // header row itself is hidden on mobile via CSS
      const rowContentStart = tableContentStart + rowOffset + 1;
      let colIndex = 0;
      row.forEach((cell, cellOffset) => {
        const label = headers[colIndex];
        // Only body cells (td) carry a label; ignore colspan like the rehype plugin.
        if (cell.type.name === "tableCell" && label) {
          const cellPos = rowContentStart + cellOffset;
          cells.push({ from: cellPos, to: cellPos + cell.nodeSize, label });
        }
        colIndex += 1;
      });
    });

    // Tables don't nest in this schema; stop descending into this one.
    return false;
  });

  return { tables, cells };
}

function buildDecorations(doc: PMNode): DecorationSet {
  const { tables, cells } = computeStackedTableSpecs(doc);
  if (tables.length === 0) return DecorationSet.empty;
  const decos: Decoration[] = [
    ...tables.map((t) => Decoration.node(t.from, t.to, { class: "data-table" })),
    ...cells.map((c) =>
      Decoration.node(c.from, c.to, { "data-label": c.label }),
    ),
  ];
  return DecorationSet.create(doc, decos);
}

const stackedTablesKey = new PluginKey("cerebroStackedTables");

/**
 * Tiptap extension: tags data tables + injects per-cell data-labels as decorations
 * so the mobile card CSS applies to the editable editor. Recomputed on every doc
 * change so freshly typed / pasted tables get the treatment immediately.
 */
export const StackedTablesExtension = Extension.create({
  name: "cerebroStackedTables",

  addProseMirrorPlugins() {
    return [
      new Plugin({
        key: stackedTablesKey,
        props: {
          decorations(state) {
            return buildDecorations(state.doc);
          },
        },
      }),
    ];
  },
});
