"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  applyTheirs,
  docDetailOptions,
  isDocRevisionConflict,
  mergeText,
  useMarkDocRead,
  useUpdateDoc,
  type MergeConflict,
} from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Doc, DocAuthorType } from "@multica/core/types";
import { useT } from "../../i18n";

/** How the page reads and replaces what the editor shows. */
export interface DraftIO {
  /** The editor's current text, including keystrokes still being debounced. */
  read: () => string;
  /** Replace the editor's text without it counting as a local edit. */
  adopt: (text: string) => void;
}

export type DraftStatus = "saved" | "saving" | "error";

export type DraftNotice =
  | { kind: "updated" | "merged"; who: { type: DocAuthorType; id: string } }
  | { kind: "conflict"; who: { type: DocAuthorType; id: string }; conflicts: MergeConflict[] };

interface Base {
  revision: number;
  title: string;
  /** The content as the editor showed it when this revision was adopted. */
  content: string;
}

export type RemoteOutcome =
  | { kind: "stale" }
  | { kind: "adopt"; content: string }
  | { kind: "merge"; content: string; conflicts: MergeConflict[] };

/**
 * What to do with a newer server revision. Without local edits the editor
 * takes it as is; with them, both sides merge line by line and lines both
 * sides changed keep the local text and are reported.
 */
export function reconcileRemote(base: Base, mine: string, remote: Pick<Doc, "revision" | "content">): RemoteOutcome {
  if (remote.revision <= base.revision) return { kind: "stale" };
  if (mine === base.content) return { kind: "adopt", content: remote.content };
  const merged = mergeText(base.content, mine, remote.content);
  return { kind: "merge", content: merged.text, conflicts: merged.conflicts };
}

const TITLE_SAVE_DELAY_MS = 800;
const NOTICE_MS = 6000;

/**
 * The open doc's draft: saves every change against the revision it was based
 * on, and folds in other people's saves — when the server refuses a stale
 * save, and when a newer revision arrives over realtime — without losing what
 * the user typed.
 */
export function useDocDraft(doc: Doc, io: DraftIO) {
  const { t } = useT("docs");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const update = useUpdateDoc();
  const markRead = useMarkDocRead();

  const base = useRef<Base>({ revision: doc.revision, title: doc.title, content: doc.content });
  const mine = useRef({ title: doc.title, content: doc.content });
  const inflight = useRef(false);
  const again = useRef(false);
  const heldRemote = useRef<Doc | null>(null);
  const titleTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const ioRef = useRef(io);
  ioRef.current = io;

  const [title, setTitleState] = useState(doc.title);
  const [status, setStatus] = useState<DraftStatus>("saved");
  const [notice, setNotice] = useState<DraftNotice | null>(null);

  useEffect(() => {
    if (!notice || notice.kind === "conflict") return;
    const timer = setTimeout(() => setNotice(null), NOTICE_MS);
    return () => clearTimeout(timer);
  }, [notice]);

  /** Take the editor's rendering of the loaded content as the merge base. */
  const captureBase = useCallback(() => {
    const shown = ioRef.current.read();
    if (mine.current.content === base.current.content) {
      base.current = { ...base.current, content: shown };
      mine.current = { ...mine.current, content: shown };
    }
  }, []);

  const absorb = useCallback(
    (remote: Doc) => {
      const live = ioRef.current.read();
      mine.current = { ...mine.current, content: live };
      const outcome = reconcileRemote(base.current, live, remote);
      if (outcome.kind === "stale") return;

      const who = { type: remote.updated_by_type, id: remote.updated_by_id };
      let baseContent = remote.content;
      if (outcome.kind === "adopt") {
        ioRef.current.adopt(outcome.content);
        baseContent = ioRef.current.read();
        mine.current = { ...mine.current, content: baseContent };
        setNotice({ kind: "updated", who });
      } else {
        ioRef.current.adopt(outcome.content);
        mine.current = { ...mine.current, content: ioRef.current.read() };
        setNotice(outcome.conflicts.length ? { kind: "conflict", who, conflicts: outcome.conflicts } : { kind: "merged", who });
      }
      if (mine.current.title.trim() === base.current.title) {
        mine.current = { ...mine.current, title: remote.title };
        setTitleState(remote.title);
      }
      base.current = { revision: remote.revision, title: remote.title, content: baseContent };
      markRead.mutate(remote.id);
    },
    [markRead],
  );

  const save = useCallback(async (): Promise<void> => {
    if (inflight.current) {
      again.current = true;
      return;
    }
    const draft = { ...mine.current };
    const b = base.current;
    const nextTitle = draft.title.trim();
    const contentChanged = draft.content !== b.content;
    const titleChanged = nextTitle !== "" && nextTitle !== b.title;
    if (!contentChanged && !titleChanged) {
      setStatus("saved");
      return;
    }

    inflight.current = true;
    setStatus("saving");
    try {
      const saved = await update.mutateAsync({
        id: doc.id,
        base_revision: b.revision,
        ...(contentChanged ? { content: draft.content } : {}),
        ...(titleChanged ? { title: nextTitle } : {}),
      });
      base.current = {
        revision: saved.revision,
        title: saved.title,
        content: contentChanged ? draft.content : b.content,
      };
      setStatus("saved");
    } catch (error) {
      if (!isDocRevisionConflict(error)) {
        setStatus("error");
        inflight.current = false;
        return;
      }
      try {
        const latest = await qc.fetchQuery({ ...docDetailOptions(wsId, doc.id), staleTime: 0 });
        absorb(latest);
        again.current = true;
      } catch {
        setStatus("error");
        inflight.current = false;
        return;
      }
    }
    inflight.current = false;
    const held = heldRemote.current;
    heldRemote.current = null;
    if (held && held.revision > base.current.revision) {
      absorb(held);
      void qc.invalidateQueries({ queryKey: docDetailOptions(wsId, doc.id).queryKey });
    }
    if (again.current || mine.current.content !== base.current.content || mine.current.title.trim() !== base.current.title) {
      again.current = false;
      void save();
    }
  }, [absorb, doc.id, qc, update, wsId]);

  const onContentChange = useCallback(
    (content: string) => {
      mine.current = { ...mine.current, content };
      void save();
    },
    [save],
  );

  const setTitle = useCallback(
    (value: string) => {
      setTitleState(value);
      mine.current = { ...mine.current, title: value };
      clearTimeout(titleTimer.current);
      titleTimer.current = setTimeout(() => void save(), TITLE_SAVE_DELAY_MS);
    },
    [save],
  );

  const flush = useCallback(() => {
    clearTimeout(titleTimer.current);
    mine.current = { ...mine.current, content: ioRef.current.read() };
    void save();
  }, [save]);

  // Flush a pending title on unmount; content flushes through the editor.
  const saveRef = useRef(save);
  saveRef.current = save;
  useEffect(
    () => () => {
      if (titleTimer.current) {
        clearTimeout(titleTimer.current);
        void saveRef.current();
      }
    },
    [],
  );

  // A newer revision from someone else (realtime refetch). While a save is in
  // flight the server arbitrates instead: that save conflicts and merges.
  useEffect(() => {
    if (doc.revision <= base.current.revision) return;
    if (inflight.current) {
      heldRemote.current = doc;
      return;
    }
    absorb(doc);
    if (mine.current.content !== base.current.content) void save();
  }, [doc, absorb, save]);

  const acceptTheirs = useCallback(() => {
    if (notice?.kind !== "conflict") return;
    const text = applyTheirs(ioRef.current.read(), notice.conflicts);
    if (text === null) {
      toast.error(t(($) => $.editor.use_theirs_failed));
      return;
    }
    ioRef.current.adopt(text);
    mine.current = { ...mine.current, content: ioRef.current.read() };
    setNotice(null);
    void save();
  }, [notice, save, t]);

  return {
    title,
    setTitle,
    onContentChange,
    captureBase,
    flush,
    status,
    retry: flush,
    notice,
    dismissNotice: () => setNotice(null),
    acceptTheirs,
  };
}
