import { describe, expect, it } from "vitest";
import {
  safeParseNoteComments,
  buildThreads,
  type NoteComment,
} from "./comments";
import { safeParseNoteVersions } from "./versions";
import { safeParseNoteLock } from "./lock";

// Wave 3 (TECH-3556) — defensive parsing + threading. The API Response
// Compatibility rule requires malformed responses to fail soft, never throw.

function comment(over: Partial<NoteComment>): NoteComment {
  return {
    id: "",
    note_id: "",
    thread_root_id: null,
    kind: "comment",
    body: "",
    anchor_quote: null,
    anchor_prefix: null,
    anchor_suffix: null,
    anchor_start: null,
    anchor_end: null,
    suggestion_text: null,
    suggestion_state: "pending",
    resolved: false,
    resolved_by: null,
    resolved_at: "",
    author_type: "member",
    author_id: "",
    created_at: "",
    updated_at: "",
    ...over,
  };
}

describe("safeParseNoteComments", () => {
  it("parses a well-formed list", () => {
    const out = safeParseNoteComments([
      { id: "a", kind: "comment", body: "hi" },
    ]);
    expect(out).toHaveLength(1);
    expect(out[0].id).toBe("a");
  });

  it("fails soft to [] on a non-array", () => {
    expect(safeParseNoteComments({ nope: true })).toEqual([]);
    expect(safeParseNoteComments(null)).toEqual([]);
  });

  it("downgrades an unknown kind/state enum instead of dropping the row", () => {
    const out = safeParseNoteComments([
      { id: "a", kind: "wild", suggestion_state: "??" },
    ]);
    expect(out[0].kind).toBe("comment");
    expect(out[0].suggestion_state).toBe("pending");
  });
});

describe("buildThreads", () => {
  it("groups replies under their root and drops orphans", () => {
    const threads = buildThreads([
      comment({ id: "root", body: "r" }),
      comment({ id: "r1", thread_root_id: "root", body: "reply" }),
      comment({ id: "orphan", thread_root_id: "missing", body: "x" }),
    ]);
    expect(threads).toHaveLength(1);
    expect(threads[0].root.id).toBe("root");
    expect(threads[0].replies.map((r) => r.id)).toEqual(["r1"]);
  });
});

describe("safeParseNoteVersions / safeParseNoteLock", () => {
  it("versions fail soft to []", () => {
    expect(safeParseNoteVersions("garbage")).toEqual([]);
    expect(safeParseNoteVersions([{ version_no: 3, reason: "weird" }])[0].reason).toBe("edit");
  });

  it("lock fails soft to a free lock", () => {
    const free = safeParseNoteLock(undefined);
    expect(free.live).toBe(false);
    expect(free.locked_by).toBeNull();
  });
});
