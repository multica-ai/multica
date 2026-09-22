import { useCallback, useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import type { Workflow, WorkflowDraft } from "../types/workflow";
import { workflowKeys } from "./queries";
import { flushWorkflowDraft, useWorkflowDraftStore, workflowDraftKey, workflowToDraft } from "./store";

export function useWorkflowDraft(wsId: string, workflow: Workflow | undefined) {
  const qc = useQueryClient();
  const id = workflow?.id ?? "";
  const key = workflowDraftKey(wsId, id);
  const entry = useWorkflowDraftStore((state) => state.entries[key]);
  const state = useWorkflowDraftStore((store) => store.saves[key]);
  const currentRef = useRef(workflow);
  currentRef.current = workflow;
  const save = useCallback(async (): Promise<Workflow> => {
    const current = qc.getQueryData<Workflow>(workflowKeys.detail(wsId, id)) ?? currentRef.current;
    if (!current || !id) throw new Error("Workflow is not loaded");
    return flushWorkflowDraft(key, current, async (draft, expectedRevision) => {
      const updated = await api.updateWorkflow(wsId, id, { ...draft, expectedRevision });
      qc.setQueryData(workflowKeys.detail(wsId, id), updated);
      void qc.invalidateQueries({ queryKey: workflowKeys.list(wsId) });
      return updated;
    });
  }, [id, key, qc, wsId]);
  const update = useCallback((patch: Partial<WorkflowDraft>) => {
    const current = currentRef.current;
    if (current) useWorkflowDraftStore.getState().edit(key, current, patch);
  }, [key]);
  const reset = useCallback(() => {
    if (!useWorkflowDraftStore.getState().saves[key]?.saving) {
      useWorkflowDraftStore.getState().discard(key);
      void qc.invalidateQueries({ queryKey: workflowKeys.detail(wsId, id) });
    }
  }, [id, key, qc, wsId]);
  const conflict = !!entry && !!workflow && entry.baseRevision !== workflow.revision && !state?.saving;
  useEffect(() => {
    if (!entry || state?.error || state?.saving || conflict || !id) return;
    const timer = setTimeout(() => { void save().catch(() => {}); }, 700);
    return () => clearTimeout(timer);
  }, [entry, state?.error, state?.saving, conflict, id, save]);
  // Navigation may precede the debounce. Pending drafts survive failed writes.
  useEffect(() => () => {
    if (id && useWorkflowDraftStore.getState().entries[key] && !useWorkflowDraftStore.getState().saves[key]?.error) {
      void save().catch(() => {});
    }
  }, [id, key, save]);
  const error = state?.error ?? null;
  const saveStatus: "saved" | "unsaved" | "saving" | "error" | "conflict" =
    state?.saving ? "saving" : conflict || (error instanceof ApiError && error.status === 409) ? "conflict" : error ? "error" : entry ? "unsaved" : "saved";
  return {
    draft: entry?.draft ?? (workflow ? workflowToDraft(workflow) : undefined),
    update, save, reset, isDirty: !!entry, isSaving: state?.saving ?? false, error, saveStatus,
  };
}
