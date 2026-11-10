"use client";
import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { getCurrentWsId } from "@multica/core/platform";
import {
  docKeys,
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
export function useDocDraft(doc: Doc) {
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
        if (!alive.current && statusRef.current === "saved")
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
  const value = draft ?? fromDoc(doc);
  const dirty =
    value.title !== value.baseTitle || value.content !== value.baseContent;
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
      if (isDocRevisionConflict(error))
        void qc.invalidateQueries({ queryKey: docKeys.detail(wsId, doc.id) });
    } finally {
      saving.current = false;
      // Edits made during the request still need saving after navigation.
      if (saved && !alive.current) void save();
    }
  };
  saveRef.current = save;
  useEffect(() => {
    if (!dirty || status !== "saved") return;
    const timer = setTimeout(() => {
      void save();
    }, 1500);
    return () => clearTimeout(timer);
  });
  const change = (patch: Partial<Pick<DocDraft, "title" | "content">>) => {
    if (!inScope()) return;
    const current =
      useDocDraftStore.getState().draft.entries[key] ?? fromDoc(doc);
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
  return { draft: value, status, dirty, change, retry: save, reload };
}
