import { describe, expect, it, vi } from "vitest";
import {
  formatNoteCommentTitle,
  resolveNoteCommentUrl,
} from "./note-comment";
import { getObjectRenderer } from "../registry";
import type { IssueReference } from "../types";

function makeRef(overrides: Partial<IssueReference> = {}): IssueReference {
  return {
    id: "ref-1",
    issue_id: "issue-1",
    object: "note_comment",
    type: "source",
    ref_id: "comment-1",
    label: null,
    url: null,
    metadata: { note_id: "note-1", comment_id: "comment-1" },
    created_by_type: "member",
    created_by_id: "user-1",
    created_at: "2026-07-13T00:00:00Z",
    updated_at: "2026-07-13T00:00:00Z",
    ...overrides,
  };
}

describe("note_comment renderer", () => {
  it("uses the stored label, with a readable fallback", () => {
    expect(formatNoteCommentTitle(makeRef({ label: "Original comment" }))).toBe(
      "Original comment",
    );
    expect(formatNoteCommentTitle(makeRef())).toBe("Source note comment");
  });

  it("builds a workspace-local URL to the exact note comment", () => {
    vi.stubGlobal("window", { location: { pathname: "/acme/issues/issue-1" } });
    expect(resolveNoteCommentUrl(makeRef())).toBe(
      "/acme/notes?note=note-1&comment=comment-1",
    );
    vi.unstubAllGlobals();
  });

  it("falls back to ref_id when metadata.comment_id is missing", () => {
    vi.stubGlobal("window", { location: { pathname: "/acme/inbox" } });
    expect(
      resolveNoteCommentUrl(makeRef({ metadata: { note_id: "note-2" }, ref_id: "comment-2" })),
    ).toBe("/acme/notes?note=note-2&comment=comment-2");
    vi.unstubAllGlobals();
  });

  it("is registered against the public renderer registry", () => {
    const renderer = getObjectRenderer("note_comment");
    vi.stubGlobal("window", { location: { pathname: "/acme/issues/issue-1" } });
    expect(renderer.formatTitle(makeRef())).toBe("Source note comment");
    expect(renderer.formatBadge(makeRef())).toBe("Note comment");
    expect(renderer.resolveUrl(makeRef())).toBe(
      "/acme/notes?note=note-1&comment=comment-1",
    );
    vi.unstubAllGlobals();
  });
});
