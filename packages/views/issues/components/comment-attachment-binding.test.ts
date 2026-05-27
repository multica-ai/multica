// @vitest-environment node
//
// FIR-2394 — regression guard for comment-edit attachment binding (data-loss
// class). When a comment is edited, the client recomputes which attachments
// stay bound and the server replaces the binding set with exactly that list
// (UpdateComment → ReplaceCommentAttachments). If a still-embedded image is
// dropped from the recomputed set, its binding is removed and the image is
// orphaned — silent data loss. These tests pin the matcher so an embedded
// attachment is never dropped, even when the stored URL form diverges from
// `attachment.url`.
import { describe, it, expect } from "vitest";
import type { Attachment } from "@multica/core/types";
import {
  attachmentReferencedInContent,
  collectActiveAttachmentIds,
} from "./comment-attachment-binding";

function att(id: string, url: string): Attachment {
  return {
    id,
    workspace_id: "ws",
    issue_id: "iss",
    comment_id: "c",
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "u",
    filename: "shot.png",
    url,
    download_url: url,
    content_type: "image/png",
    size_bytes: 1,
    created_at: "2026-05-27T00:00:00Z",
  };
}

describe("attachmentReferencedInContent", () => {
  const url = "/uploads/workspaces/abc/019e69a0-737b-7e57-852b-cee2390b8f9d.png";
  const a = att("att-1", url);

  it("matches when the exact URL is present (common case)", () => {
    expect(attachmentReferencedInContent(`![](${url})`, a)).toBe(true);
  });

  it("matches when the stored URL diverges but the storage key survives (CDN rewrite)", () => {
    const rewritten = `![](https://multica-static.copilothub.ai/abc/019e69a0-737b-7e57-852b-cee2390b8f9d.png)`;
    expect(attachmentReferencedInContent(rewritten, a)).toBe(true);
  });

  it("matches when a signed query string is appended to the URL", () => {
    const signed = `![](${url}?Expires=123&Signature=xyz)`;
    expect(attachmentReferencedInContent(signed, a)).toBe(true);
  });

  it("does not match when the attachment is no longer referenced at all", () => {
    expect(attachmentReferencedInContent("just text, no image", a)).toBe(false);
  });
});

describe("collectActiveAttachmentIds", () => {
  const embeddedUrl = "/uploads/workspaces/abc/019e69a0-737b-7e57-852b-cee2390b8f9d.png";
  const embedded = att("embedded-1", embeddedUrl);
  const standalone = att("standalone-1", "/uploads/workspaces/abc/aaaa1111-2222-3333-4444-555566667777.pdf");

  it("keeps an embedded image whose URL is still in the content", () => {
    const ids = collectActiveAttachmentIds(`see ![](${embeddedUrl})`, [embedded], new Set());
    expect(ids).toEqual(["embedded-1"]);
  });

  it("keeps an embedded image even when the content carries a CDN-rewritten URL", () => {
    const content = `see ![](https://cdn.example.com/abc/019e69a0-737b-7e57-852b-cee2390b8f9d.png)`;
    const ids = collectActiveAttachmentIds(content, [embedded], new Set());
    expect(ids).toEqual(["embedded-1"]);
  });

  it("drops an embedded image that the user actually removed", () => {
    const ids = collectActiveAttachmentIds("text only now", [embedded], new Set());
    expect(ids).toEqual([]);
  });

  it("retains standalone attachments via retainedStandaloneIds", () => {
    const ids = collectActiveAttachmentIds("text only", [standalone], new Set(["standalone-1"]));
    expect(ids).toEqual(["standalone-1"]);
  });

  it("keeps both an embedded image and a retained standalone file", () => {
    const ids = collectActiveAttachmentIds(
      `body ![](${embeddedUrl})`,
      [embedded, standalone],
      new Set(["standalone-1"]),
    );
    expect(new Set(ids)).toEqual(new Set(["embedded-1", "standalone-1"]));
  });
});
