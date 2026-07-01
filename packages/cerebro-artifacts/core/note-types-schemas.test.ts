import { describe, expect, it } from "vitest";

import { noteTypeSchema } from "./note-types-schemas";

const baseNoteType = {
  id: "nt-1",
  workspace_id: "ws-1",
  name: "Business Review",
  icon: "",
  template_body: "",
  recurrence_mode: "new_note",
  cadence_unit: "week",
  cadence_count: 2,
  target_folder_id: null,
  running_doc_artifact_id: null,
  enabled: true,
  numbering_enabled: false,
  next_number: 1,
  created_at: "2026-06-16T08:00:00Z",
  updated_at: "2026-06-16T08:00:00Z",
};

describe("noteTypeSchema", () => {
  it("defaults anchor_weekday to null for older API responses", () => {
    expect(noteTypeSchema.parse(baseNoteType).anchor_weekday).toBeNull();
  });

  it("preserves a selected anchor weekday", () => {
    expect(noteTypeSchema.parse({ ...baseNoteType, anchor_weekday: 1 }).anchor_weekday).toBe(1);
  });
});
