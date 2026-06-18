"use client";

// FIR-1412: one hook the Skills / Autopilots list pages call to fold folders in
// with a minimal footprint. Returns whether the feature is on, a predicate to
// filter the list by the selected folder, and the ready-to-render sidebar node.

import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
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
  /** The folder sidebar, ready to render beside the list. */
  sidebar: ReactNode;
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

  const sidebar = enabled ? (
    <EntityFolderSidebar
      kind={kind}
      folders={folders}
      items={items}
      membership={membership}
      selected={effectiveSelected}
      onSelect={setSelected}
      itemNoun={itemNoun}
    />
  ) : null;

  const getDragProps = (itemId: string): EntityFolderDragProps =>
    enabled
      ? {
          draggable: true,
          onDragStart: (e) => setEntityFolderDragData(e, kind, itemId),
        }
      : {};

  return { enabled, includes, sidebar, getDragProps };
}
