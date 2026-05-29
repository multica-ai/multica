"use client";

import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@multica/core/api";
import { buildDefaultViewRequests, viewKeys } from "@multica/core/views";
import type { CreateViewRequest, SavedView, ViewPage } from "@multica/core/types";

type SeedablePage = Exclude<ViewPage, "project">;

/**
 * Seed a page's default views the first time it's visited with zero saved
 * views. Race-safe: the backend enforces a unique (workspace, page, project,
 * name) constraint, so two concurrent visitors POSTing the same defaults is
 * harmless — a 409 conflict means someone else already seeded that name, which
 * we treat as success. After seeding we invalidate the views list so the
 * freshly created tabs appear.
 *
 * Runs once per (wsId, page, projectId) per mount via a ref guard, and only
 * when the views list has loaded and is empty. `resolveName` maps a default's
 * i18n key to a display name.
 */
export function useSeedDefaultViews(
  wsId: string,
  page: SeedablePage,
  views: SavedView[] | undefined,
  isLoading: boolean,
  resolveName: (nameKey: string) => string,
  projectId?: string,
) {
  const qc = useQueryClient();
  const seededRef = useRef<string | null>(null);

  useEffect(() => {
    if (!wsId || isLoading || views === undefined) return;
    if (views.length > 0) return;
    const guardKey = `${wsId}:${page}:${projectId ?? ""}`;
    if (seededRef.current === guardKey) return;
    seededRef.current = guardKey;

    const requests = buildDefaultViewRequests(page, projectId ?? null, resolveName);
    void seedDefaults(requests).then(() => {
      qc.invalidateQueries({ queryKey: viewKeys.list(wsId, page, projectId) });
    });
  }, [wsId, page, projectId, views, isLoading, resolveName, qc]);
}

async function seedDefaults(requests: CreateViewRequest[]): Promise<void> {
  await Promise.all(
    requests.map((req) =>
      api.createView(req).catch((err) => {
        // 409 = another visitor seeded this name first; that's the success
        // path for the race, not an error. Anything else bubbles to the
        // logger via the api client and we swallow it here so one failed
        // default doesn't block the others.
        if (err instanceof ApiError && err.status === 409) return;
      }),
    ),
  );
}
