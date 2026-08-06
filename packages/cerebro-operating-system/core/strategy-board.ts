import type { VisionPlanItem, VisionPlanSection } from "./types";

// Pure reordering helpers for the drag-and-drop Strategy canvas. They take the
// current sections and a drag result, and return only the rows whose stored
// position (or section) changed — so the caller dispatches the minimum number
// of persistence mutations. Kept free of React/dnd-kit so they are unit-tested
// directly (see strategy-board.test.ts).

export interface SectionMove {
  id: string;
  page_id: string;
  row_index: number;
  column_index: number;
  position: number;
}

export interface ItemMove {
  id: string;
  section_id: string;
  position: number;
}

const activeItems = (section: VisionPlanSection): VisionPlanItem[] =>
  section.items.filter((item) => item.state === "active").sort((a, b) => a.position - b.position);

/**
 * Blocks whose stored cell no longer exists — a row was removed, or a row lost
 * a column — fall back to the first cell instead of disappearing from the page.
 * The next drag renumbers them, so the fallback heals itself.
 */
export function clampSectionsToLayout(sections: VisionPlanSection[], pageId: string, rowColumnCounts: number[]): VisionPlanSection[] {
  return sections.map((section) => {
    if (section.page_id !== pageId) return section;
    const columns = rowColumnCounts[section.row_index];
    if (columns !== undefined && section.column_index < columns) return section;
    return { ...section, row_index: 0, column_index: 0 };
  });
}

/** The blocks of one cell — a column inside a row — in the order they are drawn. */
export function columnSections(sections: VisionPlanSection[], pageId: string, rowIndex: number, columnIndex: number): VisionPlanSection[] {
  return sections
    .filter((section) => section.page_id === pageId && section.row_index === rowIndex && section.column_index === columnIndex)
    .sort((a, b) => a.position - b.position);
}

/**
 * Move block `activeId` into the cell (`pageId`, `rowIndex`, `columnIndex`),
 * inserting it before `beforeSectionId` (or appending when that is undefined /
 * not found). Renumbers the source and destination cells and returns every
 * block whose page, cell, or position changed.
 */
export function moveSection(
  sections: VisionPlanSection[],
  activeId: string,
  pageId: string,
  rowIndex: number,
  columnIndex: number,
  beforeSectionId?: string,
): SectionMove[] {
  const moved = sections.find((section) => section.id === activeId);
  if (!moved || activeId === beforeSectionId) return [];

  const source = { pageId: moved.page_id, rowIndex: moved.row_index, columnIndex: moved.column_index };
  const dest = columnSections(sections, pageId, rowIndex, columnIndex).filter((section) => section.id !== activeId);
  let insertAt = dest.length;
  if (beforeSectionId) {
    const target = dest.findIndex((section) => section.id === beforeSectionId);
    if (target !== -1) insertAt = target;
  }
  dest.splice(insertAt, 0, moved);

  const cells = [{ pageId, rowIndex, columnIndex }];
  const sameCell = source.pageId === pageId && source.rowIndex === rowIndex && source.columnIndex === columnIndex;
  if (!sameCell) cells.push(source);

  const changes: SectionMove[] = [];
  for (const cell of cells) {
    const list = cell.pageId === pageId && cell.rowIndex === rowIndex && cell.columnIndex === columnIndex
      ? dest
      : columnSections(sections, cell.pageId, cell.rowIndex, cell.columnIndex).filter((section) => section.id !== activeId);
    list.forEach((section, index) => {
      if (section.position !== index || section.page_id !== cell.pageId || section.row_index !== cell.rowIndex || section.column_index !== cell.columnIndex) {
        changes.push({ id: section.id, page_id: cell.pageId, row_index: cell.rowIndex, column_index: cell.columnIndex, position: index });
      }
    });
  }
  return changes;
}

/**
 * Move item `activeItemId` into `destSectionId`, inserting it before
 * `beforeItemId` (or appending when that is undefined / not found). Renumbers
 * the source and destination columns and returns every item whose section or
 * position changed.
 */
export function moveItem(
  sections: VisionPlanSection[],
  activeItemId: string,
  destSectionId: string,
  beforeItemId?: string,
): ItemMove[] {
  const lists = new Map<string, VisionPlanItem[]>();
  for (const section of sections) lists.set(section.id, activeItems(section));
  if (!lists.has(destSectionId)) return [];

  let sourceId: string | undefined;
  let moved: VisionPlanItem | undefined;
  for (const [sectionId, list] of lists) {
    const index = list.findIndex((item) => item.id === activeItemId);
    if (index !== -1) {
      sourceId = sectionId;
      [moved] = list.splice(index, 1);
      break;
    }
  }
  if (!moved || !sourceId) return [];
  if (activeItemId === beforeItemId) return [];

  const dest = lists.get(destSectionId)!;
  let insertAt = dest.length;
  if (beforeItemId) {
    const target = dest.findIndex((item) => item.id === beforeItemId);
    if (target !== -1) insertAt = target;
  }
  dest.splice(insertAt, 0, moved);

  const changes: ItemMove[] = [];
  for (const sectionId of new Set([sourceId, destSectionId])) {
    lists.get(sectionId)!.forEach((item, index) => {
      if (item.position !== index || item.section_id !== sectionId) {
        changes.push({ id: item.id, section_id: sectionId, position: index });
      }
    });
  }
  return changes;
}
