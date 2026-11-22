"use client";
import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { getCurrentWsId } from "@multica/core/platform";
import {
  docKeys,
  isDocNotFound,
  isDocRevisionConflict,
  setDocDraft,
  removeDocDraft,
  useDocDraftStore,
  type DocDraft,
} from "@multica/core/docs";
import type { Doc } from "@multica/core/types";
const fromDoc = (doc: Doc): DocDraft => ({
  title: doc.title,
  content: doc.content,
  baseTitle: doc.title,
  baseContent: doc.content,
  baseRevision: doc.revision,
});
/**
 * The doc as edited through the API: a local draft, saved after a pause and
 * when the page goes away. `enabled` is false while the doc is (or may be)
 * edited live, when the shared document saves the body: nothing is sent then.
 */
export function useDocDraft(doc: Doc, enabled = true) {
  const wsId = useWorkspaceId();
  const key = `${wsId}:${doc.id}`;
  const draft = useDocDraftStore((s) => s.draft.entries[key]);
  const qc = useQueryClient();
  const [status, setStatus] = useState<
    "saved" | "saving" | "error" | "conflict"
  >("saved");
  const saving = useRef(false);
  const alive = useRef(true);
  const ownerId = useRef(useAuthStore.getState().user?.id).current;
  const statusRef = useRef(status);
  const saveRef = useRef<() => Promise<void>>(async () => {});
  // The response of our last save. Clearing the draft re-renders this hook
  // before the parent passes the refreshed `doc`, so falling back to the prop
  // would briefly hand the editor the pre-save content and reset its caret.
  const savedDoc = useRef<Doc | null>(null);
  const enabledRef = useRef(enabled);
  enabledRef.current = enabled;
  // The server trims titles. Keep what was typed in the field, so a pause
  // after a space does not join the next word to the previous one.
  const typedTitle = useRef<{ typed: string; saved: string } | null>(null);
  const latestDoc =
    savedDoc.current && savedDoc.current.revision > doc.revision
      ? savedDoc.current
      : doc;
  const inScope = () =>
    getCurrentWsId() === wsId && useAuthStore.getState().user?.id === ownerId;
  const updateStatus = (next: typeof status) => {
    statusRef.current = next;
    if (alive.current) setStatus(next);
  };
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      // Child editor cleanup can still publish its final pending update.
      // StrictMode's effect replay remounts before this microtask runs.
      queueMicrotask(() => {
        if (!alive.current && enabledRef.current && statusRef.current === "saved")
          void saveRef.current();
      });
    };
  }, []);
  useEffect(() => {
    const current = useDocDraftStore.getState().draft.entries[key];
    if (
      current &&
      !saving.current &&
      current.title === current.baseTitle &&
      current.content === current.baseContent
    ) {
      if (!current.backup) removeDocDraft(key);
      else if (current.baseRevision !== doc.revision)
        setDocDraft(key, { ...fromDoc(doc), backup: current.backup });
    }
  }, [doc, key]);
  const stored = draft ?? fromDoc(latestDoc);
  const dirty =
    stored.title !== stored.baseTitle || stored.content !== stored.baseContent;
  const value =
    typedTitle.current?.saved === stored.title
      ? { ...stored, title: typedTitle.current.typed }
      : stored;
  const save = async () => {
    const sent = useDocDraftStore.getState().draft.entries[key];
    if (
      !sent ||
      !inScope() ||
      saving.current ||
      (sent.title === sent.baseTitle && sent.content === sent.baseContent)
    )
      return;
    saving.current = true;
    updateStatus("saving");
    let saved = false;
    try {
      const updated = await api.updateDoc(doc.id, {
        baseRevision: sent.baseRevision,
        title: sent.title,
        content: sent.content,
      });
      const current = useDocDraftStore.getState().draft.entries[key];
      // Session cleanup must stay final even if an older request completes.
      if (!inScope() || !current) {
        updateStatus("saved");
        return;
      }
      qc.setQueryData(docKeys.detail(wsId, doc.id), updated);
      savedDoc.current = updated;
      if (current.title === sent.title && updated.title !== sent.title)
        typedTitle.current = { typed: sent.title, saved: updated.title };
      const next = {
        ...current,
        title: current.title === sent.title ? updated.title : current.title,
        baseRevision: updated.revision,
        baseTitle: updated.title,
        baseContent: updated.content,
      };
      if (
        next.title === next.baseTitle &&
        next.content === next.baseContent &&
        !next.backup
      )
        removeDocDraft(key);
      else setDocDraft(key, next);
      void qc.invalidateQueries({ queryKey: docKeys.all(wsId) });
      saved = true;
      updateStatus("saved");
    } catch (error) {
      if (!inScope()) {
        updateStatus("saved");
        return;
      }
      updateStatus(isDocRevisionConflict(error) ? "conflict" : "error");
      // A doc deleted elsewhere answers 404: refetch so the page says so.
      if (isDocRevisionConflict(error) || isDocNotFound(error))
        void qc.invalidateQueries({ queryKey: docKeys.detail(wsId, doc.id) });
    } finally {
      saving.current = false;
      // Edits made during the request still need saving after navigation.
      if (saved && !alive.current && enabledRef.current) void save();
    }
  };
  saveRef.current = save;
  useEffect(() => {
    if (!enabled || !draft || !dirty || status !== "saved") return;
    const timer = setTimeout(() => {
      void saveRef.current();
    }, 1500);
    return () => clearTimeout(timer);
  }, [enabled, draft, dirty, status]);
  const change = (patch: Partial<Pick<DocDraft, "title" | "content">>) => {
    if (!inScope()) return;
    const current =
      useDocDraftStore.getState().draft.entries[key] ?? fromDoc(latestDoc);
    if (patch.title !== undefined) typedTitle.current = null;
    setDocDraft(key, { ...current, ...patch });
  };
  const reload = async () => {
    try {
      const latest = await api.getDoc(doc.id);
      if (!inScope()) return;
      const current = useDocDraftStore.getState().draft.entries[key];
      setDocDraft(key, {
        ...fromDoc(latest),
        backup: current
          ? { title: current.title, content: current.content }
          : undefined,
      });
      qc.setQueryData(docKeys.detail(wsId, doc.id), latest);
      updateStatus("saved");
    } catch {
      if (inScope()) updateStatus("error");
    }
  };
  /** Keeps unsaved edits as a read-only backup instead of saving them: they
   *  were made against an older copy, and the live document has moved on. */
  const setAside = () => {
    const current = useDocDraftStore.getState().draft.entries[key];
    if (!current || !inScope()) return;
    if (current.title === current.baseTitle && current.content === current.baseContent) return;
    setDocDraft(key, {
      ...fromDoc(latestDoc),
      backup: { title: current.title, content: current.content },
    });
  };
  return { draft: value, status, dirty, change, retry: save, reload, setAside };
}
