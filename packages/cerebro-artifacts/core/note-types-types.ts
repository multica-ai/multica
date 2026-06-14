// TECH-3511 note types (reusable note templates with recurrence) — shared types.

export type RecurrenceMode = "running_doc" | "new_note";
export const RECURRENCE_MODES: RecurrenceMode[] = ["running_doc", "new_note"];

export type CadenceUnit = "manual" | "week" | "month" | "quarter";
export const CADENCE_UNITS: CadenceUnit[] = ["manual", "week", "month", "quarter"];

export interface NoteType {
  id: string;
  workspace_id: string;
  name: string;
  icon: string;
  template_body: string;
  recurrence_mode: RecurrenceMode;
  cadence_unit: CadenceUnit;
  cadence_count: number;
  target_folder_id: string | null;
  running_doc_artifact_id: string | null;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface NoteTypeWriteInput {
  name: string;
  icon?: string;
  template_body?: string;
  recurrence_mode?: RecurrenceMode;
  cadence_unit?: CadenceUnit;
  cadence_count?: number;
  target_folder_id?: string | null;
  enabled?: boolean;
}

export interface NoteTypeRunResult {
  created: boolean;
  artifact_id: string;
}
