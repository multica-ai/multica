"use client";
import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { docKeys } from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Doc } from "@multica/core/types";

const SAVE_DELAY_MS = 800;

/**
 * The title of a doc being edited live. The body syncs through the shared
 * document; the title is saved on its own, last write wins, so a rename never
 * touches what people are typing.
 */
export function useLiveDocTitle(doc: Doc, enabled: boolean) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  // What was typed, kept until the field loses focus so a pause after a space
  // does not lose the space to the server's trim.
  const [typed, setTyped] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const saved = useRef<string | null>(null);

  useEffect(() => {
    if (!enabled || typed === null || typed.trim() === (saved.current ?? doc.title)) return;
    const timer = setTimeout(() => {
      api
        .updateDoc(doc.id, { baseRevision: doc.revision, title: typed })
        .then((updated) => {
          saved.current = updated.title;
          setFailed(false);
          qc.setQueryData(docKeys.detail(wsId, doc.id), updated);
        })
        .catch(() => setFailed(true));
    }, SAVE_DELAY_MS);
    return () => clearTimeout(timer);
  }, [enabled, typed, doc.id, doc.revision, doc.title, qc, wsId]);

  return {
    value: typed ?? doc.title,
    failed,
    change: (title: string) => setTyped(title),
    blur: () => {
      if (typed !== null && typed.trim() === (saved.current ?? doc.title)) {
        setTyped(null);
        saved.current = null;
      }
    },
  };
}
