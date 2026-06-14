// TECH-3511 — zod schemas for the note-types API responses. Per the API
// Response Compatibility rule, every consumed response is parsed (not cast)
// with parseWithFallback against these schemas.

import { z } from "zod";
import type { NoteType } from "./note-types-types";

export const noteTypeSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  icon: z.string().default(""),
  template_body: z.string().default(""),
  recurrence_mode: z.enum(["running_doc", "new_note"]).catch("running_doc"),
  cadence_unit: z.enum(["manual", "day", "week", "month", "quarter"]).catch("manual"),
  cadence_count: z.number().catch(1),
  target_folder_id: z.string().nullable().default(null),
  running_doc_artifact_id: z.string().nullable().default(null),
  enabled: z.boolean().catch(true),
  numbering_enabled: z.boolean().catch(false),
  next_number: z.number().catch(1),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
});

export const noteTypesListSchema = z.array(noteTypeSchema);

export const noteTypeRunResultSchema = z.object({
  created: z.boolean().catch(false),
  artifact_id: z.string().default(""),
});

export const EMPTY_NOTE_TYPES_LIST: NoteType[] = [];
