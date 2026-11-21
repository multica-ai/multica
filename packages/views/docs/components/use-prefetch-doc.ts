"use client";
import { useCallback, useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { docCommentsOptions, docDetailOptions } from "@multica/core/docs";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useWS } from "@multica/core/realtime";
import { useNavigation } from "../../navigation";
import { findDocSession, liveEditingOff, prefetchDocLive } from "./doc-live-session";

/** A pointer passing over a list is not heading for every row in it. */
const HOVER_INTENT_MS = 80;

let docPageWarmed = false;

/**
 * Prefetches one doc's route once per session when the browser is idle. On
 * web the doc page's code is shared by every doc but only requested after a
 * doc's route payload arrives, which put a whole round trip in front of the
 * first doc opened; warming it here leaves each open with just its payload.
 * Desktop bundles its routes and has no route prefetch, so this is a no-op.
 */
export function useWarmDocPage(docId: string | undefined) {
  const { prefetch } = useNavigation();
  const p = useWorkspacePaths();
  const href = docId ? p.docDetail(docId) : null;
  useEffect(() => {
    if (!href || !prefetch || docPageWarmed) return;
    const warm = () => {
      docPageWarmed = true;
      prefetch(href);
    };
    if (typeof window.requestIdleCallback === "function") {
      const id = window.requestIdleCallback(warm, { timeout: 3000 });
      return () => window.cancelIdleCallback(id);
    }
    const timer = window.setTimeout(warm, 1500);
    return () => window.clearTimeout(timer);
  }, [href, prefetch]);
}

/**
 * Hover and focus handlers for a link to a doc: they fetch what the doc page
 * opens with (the doc, its discussions, its live session's ticket and saved
 * state), so the page usually paints in full on the click.
 */
export function usePrefetchDoc() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { collabUrl } = useWS();
  const userId = useAuthStore((s) => s.user?.id ?? "");
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => {
    if (timer.current) clearTimeout(timer.current);
  }, []);

  const prefetch = useCallback(
    (docId: string) => {
      void qc.prefetchQuery(docDetailOptions(wsId, docId));
      void qc.prefetchQuery(docCommentsOptions(wsId, docId));
      // An open or kept session already holds the doc and is kept current.
      if (liveEditingOff.has(collabUrl) || findDocSession(collabUrl, docId, userId)) return;
      prefetchDocLive(collabUrl, docId, userId).catch(() => undefined);
    },
    [qc, wsId, collabUrl, userId],
  );

  return useCallback(
    (docId: string) => ({
      onMouseEnter: () => {
        if (timer.current) clearTimeout(timer.current);
        timer.current = setTimeout(() => prefetch(docId), HOVER_INTENT_MS);
      },
      onMouseLeave: () => {
        if (timer.current) clearTimeout(timer.current);
        timer.current = null;
      },
      onFocus: () => prefetch(docId),
    }),
    [prefetch],
  );
}
