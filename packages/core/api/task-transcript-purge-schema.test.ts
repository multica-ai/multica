import { describe, expect, it } from "vitest";
import { parseWithFallback } from "./schema";
import { EMPTY_TASK_TRANSCRIPT_PURGE, TaskTranscriptPurgeSchema } from "./schemas";

const parse = (raw: unknown) =>
  parseWithFallback(raw, TaskTranscriptPurgeSchema, EMPTY_TASK_TRANSCRIPT_PURGE, {
    endpoint: "DELETE /api/tasks/:id/messages",
  });

describe("TaskTranscriptPurgeSchema", () => {
  it("passes a well-formed response through", () => {
    expect(
      parse({ transcript_purged_at: "2026-11-26T09:29:33Z", transcript_purged_by: "user-1", messages_deleted: 6 }),
    ).toEqual({ transcript_purged_at: "2026-11-26T09:29:33Z", transcript_purged_by: "user-1", messages_deleted: 6 });
  });

  it("nulls a missing or malformed timestamp instead of passing it to the view", () => {
    expect(parse({ transcript_purged_by: "user-1", messages_deleted: 6 }).transcript_purged_at).toBeNull();
    expect(parse({ transcript_purged_at: 17, transcript_purged_by: 3, messages_deleted: "6" })).toEqual({
      transcript_purged_at: null,
      transcript_purged_by: "",
      messages_deleted: 0,
    });
  });

  it("falls back to an empty purge for a body that is not an object", () => {
    expect(parse(null)).toEqual(EMPTY_TASK_TRANSCRIPT_PURGE);
    expect(parse("ok")).toEqual(EMPTY_TASK_TRANSCRIPT_PURGE);
  });
});
