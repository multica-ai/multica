import { describe, expect, it } from "vitest";
import type { Attachment } from "@multica/core/types";
import { standaloneAttachments, allIssueAttachments } from "./standalone";

function att(over: Partial<Attachment>): Attachment {
  return {
    id: "a1",
    workspace_id: "ws",
    issue_id: "i1",
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "u1",
    filename: "file.png",
    url: "https://cdn.test/file.png",
    download_url: "/api/attachments/a1/download",
    content_type: "image/png",
    size_bytes: 100,
    created_at: "2026-06-26T09:00:00Z",
    ...over,
  };
}

describe("standaloneAttachments", () => {
  it("returns [] for empty/undefined input", () => {
    expect(standaloneAttachments(undefined)).toEqual([]);
    expect(standaloneAttachments([])).toEqual([]);
  });

  it("returns everything when content is undefined/empty", () => {
    const list = [att({ id: "a1" }), att({ id: "a2", url: "https://cdn.test/b.png" })];
    expect(standaloneAttachments(list)).toHaveLength(2);
    expect(standaloneAttachments(list, "")).toHaveLength(2);
  });

  it("drops an attachment whose url is referenced inline", () => {
    const inline = att({ id: "a1", filename: "inline.png", url: "https://cdn.test/inline.png" });
    const other = att({ id: "a2", filename: "other.png", url: "https://cdn.test/other.png" });
    const result = standaloneAttachments([inline, other], `![x](${inline.url})`);
    expect(result.map((a) => a.id)).toEqual(["a2"]);
  });

  it("drops a duplicate upload when an identical sibling is inline", () => {
    const inline = att({ id: "a1", url: "https://cdn.test/dup-1.png" });
    const dup = att({ id: "a2", url: "https://cdn.test/dup-2.png" }); // same name/type/size
    const result = standaloneAttachments([inline, dup], `![x](${inline.url})`);
    expect(result).toHaveLength(0);
  });

  it("keeps a same-name file that differs in size", () => {
    const inline = att({ id: "a1", url: "https://cdn.test/dup-1.png" });
    const bigger = att({ id: "a2", url: "https://cdn.test/dup-2.png", size_bytes: 999 });
    const result = standaloneAttachments([inline, bigger], `![x](${inline.url})`);
    expect(result.map((a) => a.id)).toEqual(["a2"]);
  });
});

describe("allIssueAttachments (FIR-2710)", () => {
  it("returns [] for empty/undefined input", () => {
    expect(allIssueAttachments(undefined, undefined)).toEqual([]);
    expect(allIssueAttachments([], [])).toEqual([]);
  });

  it("unions issue + comment attachments, deduped by id", () => {
    const issue = att({ id: "a1" });
    const shared = att({ id: "a2" });
    const commentOnly = att({ id: "a3" });
    const result = allIssueAttachments([issue, shared], [shared, commentOnly]);
    expect(result.map((a) => a.id).sort()).toEqual(["a1", "a2", "a3"]);
  });

  it("keeps inline-referenced attachments (unlike standaloneAttachments)", () => {
    // No content filtering here at all — the tab is the complete file index.
    const inline = att({ id: "a1" });
    expect(allIssueAttachments([inline], []).map((a) => a.id)).toEqual(["a1"]);
  });
});
