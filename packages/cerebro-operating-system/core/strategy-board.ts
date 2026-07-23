import type { VisionPlanItem, VisionPlanSection } from "./types";

// Pure reordering helpers for the drag-and-drop Strategy canvas. They take the
// current sections and a drag result, and return only the rows whose stored
// position (or section) changed — so the caller dispatches the minimum number
// of persistence mutations. Kept free of React/dnd-kit so they are unit-tested
// directly (see strategy-board.test.ts).

export interface SectionMove {
  id: string;
  position: number;
}

export interface ItemMove {
  id: string;
  section_id: string;
  position: number;
}

const orderedSections = (sections: VisionPlanSection[]): VisionPlanSection[] =>
  [...sections].sort((a, b) => a.position - b.position);

const activeItems = (section: VisionPlanSection): VisionPlanItem[] =>
  section.items.filter((item) => item.state === "active").sort((a, b) => a.position - b.position);

/** Reorder columns: move section `activeId` to the slot of section `overId`. */
export function moveSection(sections: VisionPlanSection[], activeId: string, overId: string): SectionMove[] {
  if (activeId === overId) return [];
  const ordered = orderedSections(sections);
  const from = ordered.findIndex((section) => section.id === activeId);
  const to = ordered.findIndex((section) => section.id === overId);
  if (from === -1 || to === -1) return [];
  const next = [...ordered];
  const [moved] = next.splice(from, 1);
  if (!moved) return [];
  next.splice(to, 0, moved);
  const changes: SectionMove[] = [];
  next.forEach((section, index) => {
    if (section.position !== index) changes.push({ id: section.id, position: index });
  });
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
