// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Attachment } from "../types/attachment";
import { attachmentDownloadPath } from "../types/attachment-url";
import { collectIssueAttachmentFiles } from "./issue-attachments";

const BRIEF_ID = "11111111-2222-3333-4444-555555555555";

function attachment(over: Partial<Attachment> & { id: string }): Attachment {
  return {
    workspace_id: "ws",
    issue_id: "issue",
    comment_id: "c1",
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "user-1",
    filename: "brief.pdf",
    url: `https://cdn.example.com/${over.id}`,
    download_url: `/api/attachments/${over.id}/download`,
    markdown_url: `https://api.example.com/api/attachments/${over.id}/download`,
    content_type: "application/pdf",
    size_bytes: 100,
    created_at: "2026-09-20T10:00:00Z",
    ...over,
  };
}

describe("collectIssueAttachmentFiles", () => {
  it("includes a member file referenced by the description", () => {
    const brief = attachment({ id: BRIEF_ID, comment_id: null });
    const files = collectIssueAttachmentFiles({
      description: `see [brief.pdf](${attachmentDownloadPath(BRIEF_ID)})`,
      attachments: [brief],
    });
    expect(files.map((f) => f.latest.id)).toEqual([BRIEF_ID]);
  });

  it("includes pending description uploads even before the markdown references them", () => {
    const pending = attachment({ id: "pending-1", comment_id: null, filename: "notes.md" });
    const files = collectIssueAttachmentFiles({
      description: "still typing",
      attachments: [],
      pendingDescriptionAttachments: [pending],
    });
    expect(files.map((f) => f.latest.id)).toEqual(["pending-1"]);
  });

  it("skips unbound comment-composer files that are not description refs or pending", () => {
    const orphan = attachment({
      id: "composer-1",
      comment_id: null,
      filename: "draft.zip",
      content_type: "application/zip",
    });
    const files = collectIssueAttachmentFiles({
      description: "no files here",
      attachments: [orphan],
    });
    expect(files).toHaveLength(0);
  });

  it("includes member comment files and skips agent uploads", () => {
    const member = attachment({ id: "m1", filename: "spec.pdf" });
    const agent = attachment({
      id: "a1",
      uploader_type: "agent",
      filename: "report.md",
      content_type: "text/markdown",
    });
    const agentBrief = attachment({
      id: BRIEF_ID,
      comment_id: null,
      uploader_type: "agent",
    });
    const files = collectIssueAttachmentFiles({
      description: `see [brief.pdf](${attachmentDownloadPath(BRIEF_ID)})`,
      attachments: [agentBrief],
      comments: [
        { id: "c1", type: "comment", attachments: [member, agent] },
      ],
    });
    expect(files.map((f) => f.latest.id)).toEqual(["m1"]);
  });

  it("skips activities, tombstoned comments and repeated ids", () => {
    const a = attachment({ id: "a1" });
    const files = collectIssueAttachmentFiles({
      comments: [
        { id: "act", type: "activity", attachments: [attachment({ id: "x" })] },
        {
          id: "gone",
          type: "comment",
          deleted_at: "2026-09-21T00:00:00Z",
          attachments: [attachment({ id: "y", filename: "y.md" })],
        },
        { id: "c1", type: "comment", attachments: [a] },
        { id: "c1-copy", type: "comment", attachments: [a] },
        null,
      ],
    });
    expect(files).toHaveLength(1);
    expect(files[0]!.versions.map((v) => v.id)).toEqual(["a1"]);
  });

  it("merges description and comment re-uploads of the same file into versions", () => {
    const v1 = attachment({
      id: BRIEF_ID,
      comment_id: null,
      created_at: "2026-09-20T10:00:00Z",
    });
    const v2 = attachment({
      id: "a2",
      comment_id: "c2",
      created_at: "2026-09-21T10:00:00Z",
    });
    const files = collectIssueAttachmentFiles({
      description: `[brief.pdf](${attachmentDownloadPath(BRIEF_ID)})`,
      attachments: [v1],
      comments: [{ id: "c2", type: "comment", attachments: [v2] }],
    });
    expect(files).toHaveLength(1);
    expect(files[0]!.versions.map((a) => a.id)).toEqual([BRIEF_ID, "a2"]);
    expect(files[0]!.latest.id).toBe("a2");
  });

  it("lists files newest first by their latest version", () => {
    const shot = attachment({
      id: "s1",
      filename: "shot.png",
      content_type: "image/png",
      created_at: "2026-09-20T09:00:00Z",
    });
    const reportV1 = attachment({
      id: "r1",
      filename: "report.md",
      content_type: "text/markdown",
      created_at: "2026-09-20T08:00:00Z",
    });
    const reportV2 = attachment({
      id: "r2",
      filename: "report.md",
      content_type: "text/markdown",
      created_at: "2026-09-22T08:00:00Z",
    });
    const files = collectIssueAttachmentFiles({
      comments: [
        { id: "c1", type: "comment", attachments: [reportV1, shot] },
        { id: "c2", type: "comment", attachments: [reportV2] },
      ],
    });
    expect(files.map((f) => f.latest.id)).toEqual(["r2", "s1"]);
  });
});
