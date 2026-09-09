import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { projectKeys } from "./queries";
import type {
  AppendProjectNoteRequest,
  CreateProjectNoteRequest,
  ListProjectNotesResponse,
  ProjectNote,
  ProjectNoteSummary,
  UpdateProjectNoteRequest,
} from "../types";

export const projectNoteKeys = {
  list: (wsId: string, projectId: string) =>
    [...projectKeys.detail(wsId, projectId), "notes"] as const,
  detail: (wsId: string, projectId: string, noteId: string) =>
    [...projectKeys.detail(wsId, projectId), "notes", noteId] as const,
};

export function projectNotesOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: projectNoteKeys.list(wsId, projectId),
    queryFn: () => api.listProjectNotes(projectId),
    select: (data) => data.notes,
  });
}

// The body only comes from the detail endpoint, so opening a note is always a
// separate fetch from listing them.
export function projectNoteOptions(
  wsId: string,
  projectId: string,
  noteId: string,
) {
  return queryOptions({
    queryKey: projectNoteKeys.detail(wsId, projectId, noteId),
    queryFn: () => api.getProjectNote(projectId, noteId),
    enabled: noteId !== "",
  });
}

// utf8ByteLength mirrors the server's len(n.BodyMd), which counts bytes. A
// naive .length would count UTF-16 code units instead — "笔记内容" is 12 bytes
// server-side but .length 4 — so a patched cache would shrink the displayed
// size until the next refetch corrected it.
export function utf8ByteLength(s: string): number {
  if (typeof TextEncoder !== "undefined") {
    return new TextEncoder().encode(s).length;
  }
  // Fallback for any runtime without TextEncoder: count UTF-8 bytes directly.
  let bytes = 0;
  for (const ch of s) {
    const cp = ch.codePointAt(0) ?? 0;
    if (cp < 0x80) bytes += 1;
    else if (cp < 0x800) bytes += 2;
    else if (cp < 0x10000) bytes += 3;
    else bytes += 4;
  }
  return bytes;
}

// toSummary derives the list-shaped row from a detail response so a write can
// patch the list cache without refetching it.
function toSummary(note: ProjectNote): ProjectNoteSummary {
  return {
    id: note.id,
    project_id: note.project_id,
    workspace_id: note.workspace_id,
    title: note.title,
    body_size: utf8ByteLength(note.body_md),
    position: note.position,
    created_at: note.created_at,
    updated_at: note.updated_at,
    created_by: note.created_by,
  };
}

export function useCreateProjectNote(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateProjectNoteRequest) =>
      api.createProjectNote(projectId, data),
    onSuccess: (created) => {
      qc.setQueryData<ListProjectNotesResponse>(
        projectNoteKeys.list(wsId, projectId),
        (old) =>
          old && !old.notes.some((n) => n.id === created.id)
            ? {
                ...old,
                notes: [...old.notes, toSummary(created)],
                total: old.total + 1,
              }
            : old,
      );
      qc.setQueryData(
        projectNoteKeys.detail(wsId, projectId, created.id),
        created,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectNoteKeys.list(wsId, projectId) });
    },
  });
}

export function useUpdateProjectNote(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      noteId,
      data,
    }: {
      noteId: string;
      data: UpdateProjectNoteRequest;
    }) => api.updateProjectNote(projectId, noteId, data),
    onSuccess: (updated) => {
      qc.setQueryData<ListProjectNotesResponse>(
        projectNoteKeys.list(wsId, projectId),
        (old) =>
          old
            ? {
                ...old,
                notes: old.notes.map((n) =>
                  n.id === updated.id ? toSummary(updated) : n,
                ),
              }
            : old,
      );
      qc.setQueryData(
        projectNoteKeys.detail(wsId, projectId, updated.id),
        updated,
      );
    },
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: projectNoteKeys.list(wsId, projectId) });
      qc.invalidateQueries({
        queryKey: projectNoteKeys.detail(wsId, projectId, vars.noteId),
      });
    },
  });
}

// Append is never optimistic: the server decides how the new chunk joins the
// existing body, so predicting the result locally would show text that differs
// from what was stored.
export function useAppendProjectNote(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      noteId,
      data,
    }: {
      noteId: string;
      data: AppendProjectNoteRequest;
    }) => api.appendProjectNote(projectId, noteId, data),
    onSuccess: (updated) => {
      qc.setQueryData(
        projectNoteKeys.detail(wsId, projectId, updated.id),
        updated,
      );
      qc.setQueryData<ListProjectNotesResponse>(
        projectNoteKeys.list(wsId, projectId),
        (old) =>
          old
            ? {
                ...old,
                notes: old.notes.map((n) =>
                  n.id === updated.id ? toSummary(updated) : n,
                ),
              }
            : old,
      );
    },
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({
        queryKey: projectNoteKeys.detail(wsId, projectId, vars.noteId),
      });
      // Also invalidate the list: if the append landed server-side but the
      // response was lost, onSuccess never patched body_size, and staleTime is
      // Infinity — the row would show the pre-append size forever.
      qc.invalidateQueries({ queryKey: projectNoteKeys.list(wsId, projectId) });
    },
  });
}

export function useDeleteProjectNote(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (noteId: string) => api.deleteProjectNote(projectId, noteId),
    onMutate: async (noteId) => {
      await qc.cancelQueries({
        queryKey: projectNoteKeys.list(wsId, projectId),
      });
      const prev = qc.getQueryData<ListProjectNotesResponse>(
        projectNoteKeys.list(wsId, projectId),
      );
      qc.setQueryData<ListProjectNotesResponse>(
        projectNoteKeys.list(wsId, projectId),
        (old) =>
          old
            ? {
                ...old,
                notes: old.notes.filter((n) => n.id !== noteId),
                total: Math.max(0, old.total - 1),
              }
            : old,
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(projectNoteKeys.list(wsId, projectId), ctx.prev);
      }
    },
    onSettled: (_data, _err, noteId) => {
      qc.invalidateQueries({ queryKey: projectNoteKeys.list(wsId, projectId) });
      qc.removeQueries({
        queryKey: projectNoteKeys.detail(wsId, projectId, noteId),
      });
    },
  });
}
