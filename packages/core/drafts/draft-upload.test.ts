import { describe, expect, it } from "vitest";
import type { Attachment } from "../types";
import {
  type DraftUpload,
  attachmentToDraftUpload,
  hasUploadingDraft,
  normalizeStoredUploads,
  uploadedAttachments,
} from "./draft-upload";

function makeAttachment(id: string): Attachment {
  return {
    id,
    workspace_id: "ws-1",
    issue_id: "issue-1",
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "alice",
    filename: `${id}.png`,
    url: `https://cdn.example.test/${id}.png`,
    download_url: `https://cdn.example.test/${id}.png`,
    markdown_url: `https://app.example.test/api/attachments/${id}/download`,
    content_type: "image/png",
    size_bytes: 10,
    created_at: "2026-06-12T00:00:00Z",
  };
}

const pending = (id: string): DraftUpload => ({
  clientUploadId: id,
  status: "uploading",
  filename: `${id}.png`,
  size: 5,
  contentType: "image/png",
});

describe("draft-upload helpers", () => {
  it("uploadedAttachments returns only completed rows, in order", () => {
    const uploads: DraftUpload[] = [
      pending("a"),
      attachmentToDraftUpload(makeAttachment("att-1")),
      { clientUploadId: "c", status: "failed", filename: "x", size: 1 },
      attachmentToDraftUpload(makeAttachment("att-2")),
    ];
    expect(uploadedAttachments(uploads).map((a) => a.id)).toEqual(["att-1", "att-2"]);
  });

  it("hasUploadingDraft is true only while a placeholder is in flight", () => {
    expect(hasUploadingDraft([pending("a")])).toBe(true);
    expect(
      hasUploadingDraft([
        attachmentToDraftUpload(makeAttachment("att-1")),
        { clientUploadId: "c", status: "failed", filename: "x", size: 1 },
      ]),
    ).toBe(false);
  });

  it("wraps a bare persisted Attachment as an uploaded placeholder (pre-L2 migration)", () => {
    const normalized = normalizeStoredUploads([makeAttachment("att-1")]);
    expect(normalized).toHaveLength(1);
    expect(normalized[0]?.status).toBe("uploaded");
    expect(uploadedAttachments(normalized).map((a) => a.id)).toEqual(["att-1"]);
  });

  it("coerces an `uploading` placeholder to `interrupted` on load", () => {
    const normalized = normalizeStoredUploads([pending("a")]);
    expect(normalized).toHaveLength(1);
    expect(normalized[0]?.status).toBe("interrupted");
    // The bytes are gone, so it is NOT a bindable attachment.
    expect(uploadedAttachments(normalized)).toEqual([]);
  });

  it("keeps failed/interrupted/uploaded placeholders across load", () => {
    const stored: DraftUpload[] = [
      { clientUploadId: "f", status: "failed", filename: "f", size: 1 },
      { clientUploadId: "i", status: "interrupted", filename: "i", size: 1 },
      attachmentToDraftUpload(makeAttachment("att-1")),
    ];
    const normalized = normalizeStoredUploads(stored);
    expect(normalized.map((u) => u.status)).toEqual(["failed", "interrupted", "uploaded"]);
  });

  it("drops junk entries and non-arrays", () => {
    expect(normalizeStoredUploads(undefined)).toEqual([]);
    expect(normalizeStoredUploads([null, 42, { foo: "bar" }])).toEqual([]);
    // An `uploaded` entry with a broken attachment is not trusted.
    expect(
      normalizeStoredUploads([{ clientUploadId: "x", status: "uploaded", filename: "x", size: 1 }]),
    ).toEqual([]);
  });
});
