"use client";

import { useEffect, useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { viewListOptions } from "@multica/core/views";
import { useWorkspaceId } from "@multica/core/hooks";
import type { SavedView, ViewPage } from "@multica/core/types";
import { Tabs, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { useNavigation } from "../navigation";
import { useSeedDefaultViews } from "./use-seed-default-views";

type SeedablePage = Exclude<ViewPage, "project">;

/**
 * Shared saved-view tab strip for the Issues and My Issues headers. ONE
 * component for both pages — store coupling is injected via props
 * (`currentViewId` + `onSelectView`) so the component stays store-agnostic.
 *
 * Behaviour:
 *   - Reads saved views from the views query cache (viewListOptions).
 *   - Lazily seeds the page's default views on first visit (see
 *     useSeedDefaultViews); the page passes `resolveDefaultName` so each page
 *     resolves its own `views.defaults` i18n namespace statically (avoids a
 *     union-namespace dynamic lookup here).
 *   - URL is the source of truth for the active view: `?view=<id>` is read on
 *     mount → onSelectView, and written on tab click. Falls back to the first
 *     view (the seeded "All") when no ?view= is present, so a refresh restores
 *     the active tab without component useState.
 */
export function ViewTabs({
  page,
  projectId,
  currentViewId,
  onSelectView,
  resolveDefaultName,
}: {
  page: SeedablePage;
  projectId?: string;
  currentViewId: string | null;
  onSelectView: (view: SavedView | null) => void;
  /** Maps a default view's i18n key to a display name (page-owned namespace). */
  resolveDefaultName: (nameKey: string) => string;
}) {
  const wsId = useWorkspaceId();
  const navigation = useNavigation();

  const { data: views, isLoading } = useQuery(viewListOptions(wsId, page, projectId));

  useSeedDefaultViews(wsId, page, views, isLoading, resolveDefaultName, projectId);

  const urlViewId = navigation.searchParams.get("view");
  const sorted = useMemo(
    () => (views ? [...views].sort((a, b) => a.position - b.position) : []),
    [views],
  );

  // Resolve the active view: URL param wins, else the pinned store id, else the
  // first (default) view. Sync the store + URL to it on mount / when views
  // load. Guarded by a ref so we only auto-select once per resolved id.
  const lastSyncedRef = useRef<string | null>(null);
  useEffect(() => {
    if (sorted.length === 0) return;
    const target =
      sorted.find((v) => v.id === urlViewId) ??
      sorted.find((v) => v.id === currentViewId) ??
      sorted[0]!;
    if (lastSyncedRef.current === target.id && currentViewId === target.id) return;
    lastSyncedRef.current = target.id;
    if (currentViewId !== target.id) onSelectView(target);
    if (urlViewId !== target.id) writeViewParam(navigation, target.id);
    // navigation is stable enough; onSelectView identity comes from the store.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sorted, urlViewId, currentViewId]);

  if (sorted.length === 0) return null;

  return (
    <Tabs value={currentViewId ?? sorted[0]!.id}>
      <TabsList variant="line">
        {sorted.map((view) => (
          <TabsTrigger
            key={view.id}
            value={view.id}
            onClick={() => {
              onSelectView(view);
              writeViewParam(navigation, view.id);
            }}
          >
            {view.name}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  );
}

/** Replace the `?view=` query param without touching the rest of the URL. */
function writeViewParam(
  navigation: ReturnType<typeof useNavigation>,
  viewId: string,
) {
  const params = new URLSearchParams(navigation.searchParams);
  params.set("view", viewId);
  navigation.replace(`${navigation.pathname}?${params.toString()}`);
}
