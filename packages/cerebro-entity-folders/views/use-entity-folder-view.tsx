"use client";

// FIR-1412: one hook the Skills / Autopilots list pages call to fold folders in
// with a minimal footprint. Returns whether the feature is on, a predicate to
// filter the list by the selected folder, and the ready-to-render sidebar node.

import { useCallback, useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Layers } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { Button } from "@multica/ui/components/ui/button";
import { Sheet, SheetContent } from "@multica/ui/components/ui/sheet";
import { entityFolderItemsOptions, entityFoldersOptions } from "../queries";
import {
  setEntityFolderDragData,
  type EntityFolderDragProps,
} from "../dnd";
import type { EntityFolderKind } from "../types";
import {
  EntityFolderSidebar,
  SELECT_ALL,
  SELECT_UNFILED,
} from "./entity-folder-sidebar";

export interface EntityFolderViewItem {
  id: string;
  label: string;
}

export interface EntityFolderView {
  /** Feature flag on for this surface. */
  enabled: boolean;
  /** True if the item should be visible under the current folder selection. */
  includes: (itemId: string) => boolean;
  /**
   * FIR-1530: name of the folder an item is filed under, or null when it is
   * unfiled or the feature is off. Lets the list view show a "Folder" column
   * without each cell re-querying the folder tree.
   */
  folderName: (itemId: string) => string | null;
  /** The folder sidebar, ready to render beside the list. Hidden under `md`. */
  sidebar: ReactNode;
  /**
   * FIR-1772: mobile-only trigger button + Sheet drawer (md:hidden). Render it
   * in the page toolbar/header; the same folder tree opens in a drawer so the
   * list keeps full width on phones. Collapses to nothing on desktop.
   */
  mobileTrigger: ReactNode;
  /**
   * Props to spread on a list row so it can be dragged onto a folder in the
   * sidebar to file it. Returns no-op (non-draggable) props when the feature
   * is off, so the host page can spread unconditionally.
   */
  getDragProps: (itemId: string) => EntityFolderDragProps;
}

const FLAG_BY_KIND = {
  skill: "cerebro_skill_folders",
  autopilot: "cerebro_autopilot_folders",
} as const;

export function useEntityFolderView(opts: {
  kind: EntityFolderKind;
  items: EntityFolderViewItem[];
  /** Plain-language plural for UI copy, e.g. "skills". Defaults to "items". */
  itemNoun?: string;
}): EntityFolderView {
  const { kind, items } = opts;
  const itemNoun = opts.itemNoun ?? "items";
  const enabled = useFeatureFlag(FLAG_BY_KIND[kind]);
  const wsId = useWorkspaceId();

  const { data: folders = [] } = useQuery({
    ...entityFoldersOptions(wsId, kind),
    enabled: enabled && Boolean(wsId),
  });
  const { data: itemRows = [] } = useQuery({
    ...entityFolderItemsOptions(wsId, kind),
    enabled: enabled && Boolean(wsId),
  });

  const membership = useMemo(() => {
    const map = new Map<string, string>();
    for (const row of itemRows) map.set(row.item_id, row.folder_id);
    return map;
  }, [itemRows]);

  const [selected, setSelected] = useState<string>(SELECT_ALL);

  // Drop a selection that points at a folder that no longer exists.
  const selectedValid =
    selected === SELECT_ALL ||
    selected === SELECT_UNFILED ||
    folders.some((f) => f.id === selected);
  const effectiveSelected = selectedValid ? selected : SELECT_ALL;

  const includes = useMemo(() => {
    return (itemId: string) => {
      if (!enabled || effectiveSelected === SELECT_ALL) return true;
      const folderId = membership.get(itemId);
      if (effectiveSelected === SELECT_UNFILED) return folderId === undefined;
      return folderId === effectiveSelected;
    };
  }, [enabled, effectiveSelected, membership]);

  // FIR-1530: resolve an item's folder name for the list "Folder" column.
  const folderName = useMemo(() => {
    const names = new Map<string, string>();
    for (const f of folders) names.set(f.id, f.name);
    return (itemId: string): string | null => {
      if (!enabled) return null;
      const folderId = membership.get(itemId);
      return folderId ? names.get(folderId) ?? null : null;
    };
  }, [enabled, folders, membership]);

  const sidebar = useMemo(
    () =>
      enabled ? (
        <EntityFolderSidebar
          kind={kind}
          folders={folders}
          items={items}
          membership={membership}
          selected={effectiveSelected}
          onSelect={setSelected}
          itemNoun={itemNoun}
        />
      ) : null,
    [enabled, effectiveSelected, folders, itemNoun, items, kind, membership],
  );

  // FIR-1772: mobile drawer state + label for the current selection.
  const [mobileOpen, setMobileOpen] = useState(false);
  const selectedLabel = useMemo(() => {
    if (effectiveSelected === SELECT_ALL) return "Folders";
    if (effectiveSelected === SELECT_UNFILED) return "Unfiled";
    return folders.find((f) => f.id === effectiveSelected)?.name ?? "Folders";
  }, [effectiveSelected, folders]);

  const mobileTrigger = useMemo(
    () =>
      enabled ? (
        <>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="shrink-0 md:hidden"
            onClick={() => setMobileOpen(true)}
          >
            <Layers className="h-3.5 w-3.5" />
            <span className="max-w-32 truncate">{selectedLabel}</span>
          </Button>
          <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
            <SheetContent
              side="left"
              showCloseButton={false}
              className="w-[85vw] max-w-xs p-0"
            >
              <EntityFolderSidebar
                kind={kind}
                folders={folders}
                items={items}
                membership={membership}
                selected={effectiveSelected}
                onSelect={(sel) => {
                  setSelected(sel);
                  setMobileOpen(false);
                }}
                itemNoun={itemNoun}
                inSheet
              />
            </SheetContent>
          </Sheet>
        </>
      ) : null,
    [
      enabled,
      mobileOpen,
      selectedLabel,
      effectiveSelected,
      folders,
      itemNoun,
      items,
      kind,
      membership,
    ],
  );

  const getDragProps = useCallback(
    (itemId: string): EntityFolderDragProps =>
      enabled
        ? {
            draggable: true,
            onDragStart: (e) => setEntityFolderDragData(e, kind, itemId),
          }
        : {},
    [enabled, kind],
  );

  return useMemo(
    () => ({ enabled, includes, folderName, sidebar, mobileTrigger, getDragProps }),
    [enabled, folderName, getDragProps, includes, sidebar, mobileTrigger],
  );
}
