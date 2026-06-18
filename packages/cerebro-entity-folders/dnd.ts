"use client";

// FIR-1461: drag-and-drop wiring so a skill/autopilot row in the list can be
// dragged onto a folder (or onto "Unfiled") in the sidebar to file/unfile it.
// The list row is the drag source; folder rows + the "Unfiled" row are drop
// targets. The payload is namespaced and kind-checked so a skill can never be
// dropped onto an autopilot folder and vice-versa.

import type { DragEvent } from "react";
import type { EntityFolderKind } from "./types";

export const ENTITY_FOLDER_DND_MIME = "application/x-cerebro-entity-folder-item";

/** Props spread onto a list row to make it draggable into a folder. */
export interface EntityFolderDragProps {
  draggable?: boolean;
  onDragStart?: (e: DragEvent) => void;
}

export function setEntityFolderDragData(
  e: DragEvent,
  kind: EntityFolderKind,
  itemId: string,
): void {
  // `kind:itemId` — a flat string keeps it readable in the DnD inspector and
  // avoids JSON.parse on the (untrusted-but-local) drop payload.
  e.dataTransfer.setData(ENTITY_FOLDER_DND_MIME, `${kind}:${itemId}`);
  e.dataTransfer.effectAllowed = "move";
}

// Browsers hide getData() during dragover for security, so accept-checks during
// dragover must use the types list (hasEntityFolderDragData); the actual id is
// only readable on drop.
export function hasEntityFolderDragData(e: DragEvent): boolean {
  return Array.from(e.dataTransfer.types).includes(ENTITY_FOLDER_DND_MIME);
}

/** Returns the dragged item id if it matches this surface's kind, else null. */
export function readEntityFolderDragData(
  e: DragEvent,
  kind: EntityFolderKind,
): string | null {
  const raw = e.dataTransfer.getData(ENTITY_FOLDER_DND_MIME);
  if (!raw) return null;
  const sep = raw.indexOf(":");
  if (sep < 0) return null;
  const dragKind = raw.slice(0, sep);
  const itemId = raw.slice(sep + 1);
  if (dragKind !== kind || !itemId) return null;
  return itemId;
}
