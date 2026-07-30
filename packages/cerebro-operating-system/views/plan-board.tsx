"use client";

import type { ReactNode } from "react";
import { useDroppable } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";

import { columnSections } from "../core/strategy-board";
import type { Rock, VisionPlanPage, VisionPlanSection } from "../core/types";

// A page is drawn as up to three columns of blocks. Which blocks sit where is
// workspace data, not code — Vision and Traction are just the seeded pages.
export const SECTION_PREFIX = "section-";
export const ITEM_PREFIX = "item-";
export const ITEM_ZONE_PREFIX = "itemzone-";
export const COLUMN_PREFIX = "column-";

export const columnId = (pageId: string, rowIndex: number, columnIndex: number) => `${COLUMN_PREFIX}${pageId}:${rowIndex}:${columnIndex}`;

export function parseColumnId(id: string): { pageId: string; rowIndex: number; columnIndex: number } | null {
  if (!id.startsWith(COLUMN_PREFIX)) return null;
  const body = id.slice(COLUMN_PREFIX.length);
  const columnSeparator = body.lastIndexOf(":");
  if (columnSeparator <= 0) return null;
  const rowSeparator = body.lastIndexOf(":", columnSeparator - 1);
  if (rowSeparator <= 0) return null;
  const rowIndex = Number(body.slice(rowSeparator + 1, columnSeparator));
  const columnIndex = Number(body.slice(columnSeparator + 1));
  if (!Number.isInteger(rowIndex) || !Number.isInteger(columnIndex)) return null;
  return { pageId: body.slice(0, rowSeparator), rowIndex, columnIndex };
}

// Tailwind needs the whole class name in the source, so the column counts are
// spelled out rather than interpolated.
const COLUMN_GRID: Record<number, string> = {
  1: "md:grid-cols-1",
  2: "md:grid-cols-2",
  3: "md:grid-cols-3",
  4: "md:grid-cols-4",
};

function BoardColumn({ pageId, rowIndex, columnIndex, children }: { pageId: string; rowIndex: number; columnIndex: number; children: ReactNode }) {
  const { setNodeRef } = useDroppable({ id: columnId(pageId, rowIndex, columnIndex) });
  return (
    <div ref={setNodeRef} aria-label={`Row ${rowIndex + 1} column ${columnIndex + 1}`} className="flex min-w-0 flex-col gap-4">
      {children}
    </div>
  );
}

/**
 * A page is drawn row by row, and each row decides how many columns it has
 * (FIR-3589 item 4) — the Vision/Traction organiser puts a wide row over a
 * narrow one, which a single per-page column count could never express.
 */
export function PageBoard({ page, sections, renderSection, renderColumnFooter }: {
  page: VisionPlanPage;
  sections: VisionPlanSection[];
  renderSection: (section: VisionPlanSection) => ReactNode;
  renderColumnFooter: (rowIndex: number, columnIndex: number) => ReactNode;
}) {
  return (
    <div className="grid gap-4">
      {page.row_column_counts.map((columnCount, rowIndex) => (
        <div key={rowIndex} role="group" aria-label={`Row ${rowIndex + 1}`} className={`grid items-start gap-4 ${COLUMN_GRID[columnCount] ?? COLUMN_GRID[3]}`}>
          {Array.from({ length: columnCount }, (_, columnIndex) => {
            const blocks = columnSections(sections, page.id, rowIndex, columnIndex);
            return (
              <BoardColumn key={columnIndex} pageId={page.id} rowIndex={rowIndex} columnIndex={columnIndex}>
                <SortableContext items={blocks.map((section) => `${SECTION_PREFIX}${section.id}`)} strategy={verticalListSortingStrategy}>
                  {blocks.map(renderSection)}
                </SortableContext>
                {renderColumnFooter(rowIndex, columnIndex)}
              </BoardColumn>
            );
          })}
        </div>
      ))}
    </div>
  );
}

// The Goals block reads the current period's goals instead of holding items, so
// a page can show them next to the plan they come from.
export function GoalsBlockBody({ rocks, rocksLabel, onOpenRock }: {
  rocks: Rock[];
  rocksLabel: string;
  onOpenRock: (rockId: string) => void;
}) {
  if (rocks.length === 0) {
    return <p className="px-1 py-2 text-sm text-muted-foreground">No {rocksLabel.toLowerCase()} in the current period yet.</p>;
  }
  return (
    <table className="w-full table-fixed text-sm">
      <thead>
        <tr className="text-left text-xs uppercase tracking-wide text-muted-foreground">
          <th scope="col" className="w-2/3 px-1 pb-2 font-medium">{rocksLabel}</th>
          <th scope="col" className="px-1 pb-2 font-medium">Who</th>
        </tr>
      </thead>
      <tbody>
        {rocks.map((rock) => (
          <tr key={rock.id} className="border-t align-top">
            <td className="px-1 py-2">
              <button type="button" onClick={() => onOpenRock(rock.id)} className="text-left hover:underline">{rock.title}</button>
            </td>
            <td className="truncate px-1 py-2 text-muted-foreground">{rock.owner_name || "Unassigned"}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
