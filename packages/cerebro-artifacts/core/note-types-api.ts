// TECH-3511 — note-types REST client. Uses api.cerebroRequest so the cerebro
// endpoints stay out of the upstream core API client.

import { api, parseWithFallback } from "@multica/core/api";
import {
  EMPTY_NOTE_TYPES_LIST,
  noteTypeRunResultSchema,
  noteTypeSchema,
  noteTypesListSchema,
} from "./note-types-schemas";
import type { NoteType, NoteTypeRunResult, NoteTypeWriteInput } from "./note-types-types";

const BASE = "/api/cerebro/note-types";

export async function listNoteTypes(): Promise<NoteType[]> {
  const raw = await api.cerebroRequest<unknown>(BASE);
  return parseWithFallback(raw, noteTypesListSchema, EMPTY_NOTE_TYPES_LIST, {
    endpoint: "listNoteTypes",
  });
}

export async function createNoteType(payload: NoteTypeWriteInput): Promise<NoteType | null> {
  const raw = await api.cerebroRequest<unknown>(BASE, {
    method: "POST",
    body: JSON.stringify(payload),
  });
  return parseWithFallback(raw, noteTypeSchema, null, { endpoint: "createNoteType" });
}

export async function updateNoteType(
  id: string,
  payload: NoteTypeWriteInput,
): Promise<NoteType | null> {
  const raw = await api.cerebroRequest<unknown>(`${BASE}/${id}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  });
  return parseWithFallback(raw, noteTypeSchema, null, { endpoint: "updateNoteType" });
}

export async function deleteNoteType(id: string): Promise<void> {
  await api.cerebroRequest<void>(`${BASE}/${id}`, { method: "DELETE" });
}

export async function runNoteType(id: string): Promise<NoteTypeRunResult | null> {
  const raw = await api.cerebroRequest<unknown>(`${BASE}/${id}/run`, { method: "POST" });
  return parseWithFallback(raw, noteTypeRunResultSchema, null, { endpoint: "runNoteType" });
}
