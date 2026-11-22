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
 * touches what people are typing. Until `enabled`, typing is only held, so a
 * title typed while the page is still connecting is not lost.
 */
export function useLiveDocTitle(doc: Doc, enabled: boolean) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  // What was typed, kept until the field loses focus so a pause after a space
  // does not lose the space to the server's trim.
  const [typed, setTyped] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const saved = useRef<string | null>(null);
  // Read when sending, not watched: live body edits bump the revision every
  // few seconds and must not keep restarting the title's debounce.
  const latest = useRef({ doc, enabled, typed });
  latest.current = { doc, enabled, typed };
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const sequence = useRef(0);

  const pending = () => {
    const { doc: current, typed: title } = latest.current;
    return title !== null && title.trim() !== (saved.current ?? current.title) ? title : null;
  };
  const send = () => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
    const title = pending();
    if (!latest.current.enabled || title === null) return;
    const { doc: current } = latest.current;
    const attempt = ++sequence.current;
    api
      .updateDoc(current.id, { baseRevision: current.revision, title })
      .then((updated) => {
        if (attempt !== sequence.current) return;
        saved.current = updated.title;
        setFailed(false);
        qc.setQueryData(docKeys.detail(wsId, current.id), updated);
      })
      .catch(() => {
        if (attempt === sequence.current) setFailed(true);
      });
  };

  useEffect(() => {
    if (!enabled || pending() === null) return;
    timer.current = setTimeout(send, SAVE_DELAY_MS);
    return () => {
      if (timer.current) clearTimeout(timer.current);
      timer.current = null;
    };
    // `send` reads everything else through `latest`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, typed, doc.title]);

  // Leaving the page within the debounce still saves the title.
  useEffect(() => () => send(), []); // eslint-disable-line react-hooks/exhaustive-deps

  return {
    value: typed ?? doc.title,
    failed,
    change: (title: string) => setTyped(title),
    blur: () => {
      send();
      if (typed !== null && typed.trim() === (saved.current ?? doc.title)) {
        setTyped(null);
        saved.current = null;
      }
    },
    retry: send,
    /** Hands over a title typed before live editing turned out to be off. */
    take: (): string | null => {
      const title = typed;
      setTyped(null);
      saved.current = null;
      return title;
    },
  };
}
