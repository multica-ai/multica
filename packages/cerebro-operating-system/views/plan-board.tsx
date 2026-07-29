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

export const columnId = (pageId: string, columnIndex: number) => `${COLUMN_PREFIX}${pageId}:${columnIndex}`;

export function parseColumnId(id: string): { pageId: string; columnIndex: number } | null {
  if (!id.startsWith(COLUMN_PREFIX)) return null;
  const separator = id.lastIndexOf(":");
  if (separator <= COLUMN_PREFIX.length) return null;
  const columnIndex = Number(id.slice(separator + 1));
  if (!Number.isInteger(columnIndex)) return null;
  return { pageId: id.slice(COLUMN_PREFIX.length, separator), columnIndex };
}

// Tailwind needs the whole class name in the source, so the column counts are
// spelled out rather than interpolated.
const COLUMN_GRID: Record<number, string> = {
  1: "md:grid-cols-1",
  2: "md:grid-cols-2",
  3: "md:grid-cols-3",
};

function BoardColumn({ pageId, columnIndex, children }: { pageId: string; columnIndex: number; children: ReactNode }) {
  const { setNodeRef } = useDroppable({ id: columnId(pageId, columnIndex) });
  return (
    <div ref={setNodeRef} aria-label={`Column ${columnIndex + 1}`} className="flex min-w-0 flex-col gap-4">
      {children}
    </div>
  );
}

export function PageBoard({ page, sections, renderSection, renderColumnFooter }: {
  page: VisionPlanPage;
  sections: VisionPlanSection[];
  renderSection: (section: VisionPlanSection) => ReactNode;
  renderColumnFooter: (columnIndex: number) => ReactNode;
}) {
  const columns = Array.from({ length: page.column_count }, (_, index) => index);
  return (
    <div className={`grid items-start gap-4 ${COLUMN_GRID[page.column_count] ?? COLUMN_GRID[3]}`}>
      {columns.map((columnIndex) => {
        const blocks = columnSections(sections, page.id, columnIndex);
        return (
          <BoardColumn key={columnIndex} pageId={page.id} columnIndex={columnIndex}>
            <SortableContext items={blocks.map((section) => `${SECTION_PREFIX}${section.id}`)} strategy={verticalListSortingStrategy}>
              {blocks.map(renderSection)}
            </SortableContext>
            {renderColumnFooter(columnIndex)}
          </BoardColumn>
        );
      })}
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
