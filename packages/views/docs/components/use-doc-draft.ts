"use client";
import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
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
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
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
      saving.current ||
      (sent.title === sent.baseTitle && sent.content === sent.baseContent)
    )
      return;
    saving.current = true;
    setStatus("saving");
    try {
      const updated = await api.updateDoc(doc.id, {
        baseRevision: sent.baseRevision,
        title: sent.title,
        content: sent.content,
      });
      const current = useDocDraftStore.getState().draft.entries[key];
      qc.setQueryData(docKeys.detail(wsId, doc.id), updated);
      if (current) {
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
      }
      void qc.invalidateQueries({ queryKey: docKeys.all(wsId) });
      if (alive.current) setStatus("saved");
    } catch (error) {
      if (alive.current)
        setStatus(isDocRevisionConflict(error) ? "conflict" : "error");
      if (isDocRevisionConflict(error))
        void qc.invalidateQueries({ queryKey: docKeys.detail(wsId, doc.id) });
    } finally {
      saving.current = false;
    }
  };
  useEffect(() => {
    if (!dirty || status !== "saved") return;
    const timer = setTimeout(() => {
      void save();
    }, 1500);
    return () => clearTimeout(timer);
  });
  const change = (patch: Partial<Pick<DocDraft, "title" | "content">>) => {
    const current =
      useDocDraftStore.getState().draft.entries[key] ?? fromDoc(doc);
    setDocDraft(key, { ...current, ...patch });
  };
  const reload = async () => {
    try {
      const latest = await api.getDoc(doc.id);
      const current = useDocDraftStore.getState().draft.entries[key];
      setDocDraft(key, {
        ...fromDoc(latest),
        backup: current
          ? { title: current.title, content: current.content }
          : undefined,
      });
      qc.setQueryData(docKeys.detail(wsId, doc.id), latest);
      setStatus("saved");
    } catch {
      setStatus("error");
    }
  };
  return { draft: value, status, dirty, change, retry: save, reload };
}
