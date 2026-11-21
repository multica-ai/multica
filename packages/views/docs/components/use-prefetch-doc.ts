"use client";
import { useCallback, useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { docCommentsOptions, docDetailOptions } from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWS } from "@multica/core/realtime";
import { liveEditingOff, prefetchDocLive } from "./doc-live-session";

/** A pointer passing over a list is not heading for every row in it. */
const HOVER_INTENT_MS = 80;

/**
 * Hover and focus handlers for a link to a doc: they fetch what the doc page
 * opens with (the doc, its discussions, its live session's ticket and saved
 * state), so the page usually paints in full on the click.
 */
export function usePrefetchDoc() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { collabUrl } = useWS();
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => {
    if (timer.current) clearTimeout(timer.current);
  }, []);

  const prefetch = useCallback(
    (docId: string) => {
      void qc.prefetchQuery(docDetailOptions(wsId, docId));
      void qc.prefetchQuery(docCommentsOptions(wsId, docId));
      if (!liveEditingOff.has(collabUrl)) prefetchDocLive(collabUrl, docId).catch(() => undefined);
    },
    [qc, wsId, collabUrl],
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
