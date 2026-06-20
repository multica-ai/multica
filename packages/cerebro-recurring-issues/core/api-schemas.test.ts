import { describe, expect, it } from "vitest";

import { issueRecurrenceSchema } from "./api-schemas";

// Regression: the read schema must accept every frequency the backend can
// persist. "every_weekday" (FIR-1611) was added to the DB, the API and the
// Frequency type but was missing from this Zod enum, so reading back an
// every_weekday recurrence failed validation and the UI fell back to
// "Not recurring" even though the value was saved correctly.
const baseRecurrence = {
  id: "rec-1",
  workspace_id: "ws-1",
  source_issue_id: "issue-1",
  interval_count: 1,
  weekdays: [],
  days_after: 7,
  trigger_status: "done",
  anchor: "completion",
  create_new_issue: true,
  new_status: "todo",
  recur_forever: true,
  occurrence_count: 0,
  armed: true,
  enabled: true,
  created_at: "2026-06-20T00:00:00Z",
  updated_at: "2026-06-20T00:00:00Z",
};

describe("issueRecurrenceSchema", () => {
  it("accepts every supported frequency", () => {
    for (const frequency of [
      "daily",
      "weekly",
      "monthly",
      "yearly",
      "days_after",
      "custom",
      "every_weekday",
    ]) {
      const parsed = issueRecurrenceSchema.safeParse({ ...baseRecurrence, frequency });
      expect(parsed.success, `frequency ${frequency} should parse`).toBe(true);
    }
  });
});
