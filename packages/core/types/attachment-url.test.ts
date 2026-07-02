import { describe, expect, it } from "vitest";
import {
  attachmentDownloadPath,
  attachmentIdFromDownloadURL,
  contentReferencesAttachment,
} from "./attachment-url";

const ID = "11111111-2222-3333-4444-555555555555";

describe("attachmentDownloadPath", () => {
  it("returns the stable per-attachment download path", () => {
    expect(attachmentDownloadPath(ID)).toBe(`/api/attachments/${ID}/download`);
  });
});

describe("attachmentIdFromDownloadURL", () => {
  it("extracts the id from a site-relative path", () => {
    expect(attachmentIdFromDownloadURL(`/api/attachments/${ID}/download`)).toBe(ID);
  });

  it("strips a query string before matching", () => {
    expect(
      attachmentIdFromDownloadURL(`/api/attachments/${ID}/download?cache=1`),
    ).toBe(ID);
  });

  it("strips a fragment before matching", () => {
    expect(attachmentIdFromDownloadURL(`/api/attachments/${ID}/download#frag`)).toBe(ID);
  });

  it("accepts an absolute https URL", () => {
    expect(
      attachmentIdFromDownloadURL(`https://app.example.com/api/attachments/${ID}/download`),
    ).toBe(ID);
  });

  it("rejects URLs that do not start with /api/attachments/", () => {
    expect(attachmentIdFromDownloadURL(`/uploads/${ID}.png`)).toBeUndefined();
    expect(
      attachmentIdFromDownloadURL("https://cdn.example.com/photo.png"),
    ).toBeUndefined();
  });

  it("rejects URLs missing the /download suffix", () => {
    expect(attachmentIdFromDownloadURL(`/api/attachments/${ID}`)).toBeUndefined();
    expect(
      attachmentIdFromDownloadURL(`/api/attachments/${ID}/content`),
    ).toBeUndefined();
  });

  it("rejects when the segment between the prefix and suffix is not a UUID literal", () => {
    expect(
      attachmentIdFromDownloadURL("/api/attachments/not-a-uuid/download"),
    ).toBeUndefined();
    expect(
      attachmentIdFromDownloadURL("/api/attachments//download"),
    ).toBeUndefined();
  });

  it("rejects malformed absolute URLs", () => {
    expect(attachmentIdFromDownloadURL("https://")).toBeUndefined();
  });

  it("returns undefined for empty input", () => {
    expect(attachmentIdFromDownloadURL("")).toBeUndefined();
  });
});

describe("contentReferencesAttachment", () => {
  const att = {
    id: ID,
    url: "/uploads/workspaces/ws/legacy.png",
    download_url: "https://cdn.example.com/workspaces/ws/file.png?Signature=fresh",
    markdown_url: `https://multica-api.copilothub.ai/api/attachments/${ID}/download`,
  };

  it("matches when the markdown uses the stable download path", () => {
    const md = `body\n\n![file](${attachmentDownloadPath(ID)})\n`;
    expect(contentReferencesAttachment(md, att)).toBe(true);
  });

  it("matches when the markdown uses the legacy storage URL", () => {
    const md = `body\n\n![file](${att.url})\n`;
    expect(contentReferencesAttachment(md, att)).toBe(true);
  });

  it("matches when the markdown uses the response download_url", () => {
    const md = `body\n\n![file](${att.download_url})\n`;
    expect(contentReferencesAttachment(md, att)).toBe(true);
  });

  it("matches when the markdown uses an older signed download_url for the same object", () => {
    const stale = "https://cdn.example.com/workspaces/ws/file.png?Signature=stale";
    const md = `body\n\n![file](${stale})\n`;
    expect(contentReferencesAttachment(md, att)).toBe(true);
  });

  it("matches when the markdown uses the server-provided markdown_url", () => {
    const md = `body\n\n![file](${att.markdown_url})\n`;
    expect(contentReferencesAttachment(md, att)).toBe(true);
  });

  it("matches when both shapes are present (rollout-window mixed content)", () => {
    const md = `before\n\n![a](${attachmentDownloadPath(ID)})\n\n![b](${att.url})\n`;
    expect(contentReferencesAttachment(md, att)).toBe(true);
  });

  it("returns false when neither URL shape appears in the body", () => {
    expect(contentReferencesAttachment("plain text", att)).toBe(false);
  });

  it("returns false on empty content", () => {
    expect(contentReferencesAttachment("", att)).toBe(false);
  });

  it("does not crash when the attachment has no legacy url", () => {
    const md = `![file](${attachmentDownloadPath(ID)})`;
    expect(contentReferencesAttachment(md, { id: ID, url: "" })).toBe(true);
  });

  // Regression — issue DESCRIPTION editor binding (Desktop image render).
  //
  // The editor persists the durable `markdown_url`
  // (`<MULTICA_PUBLIC_URL>/api/attachments/<id>/download`) into the body,
  // NOT the raw storage `a.url`. The description composer used to bind
  // pending uploads with `md.includes(a.url)`, which never matched this
  // shape, so the upload was never linked via `attachment_ids`. After a
  // reload the attachment was absent from `issueAttachments`, the renderer
  // couldn't resolve it to a freshly-signed `download_url`, and the
  // auth-gated endpoint failed to load as a native <img> on Desktop/Electron
  // (cross-origin, no cookies). `contentReferencesAttachment` matches the
  // absolute-host download URL via its stable-path substring, so the
  // attachment now binds the same way comments do.
  it("matches the absolute-host markdown_url the editor actually persists", () => {
    const md = `body\n\n![pasted](${att.markdown_url})\n`;
    // The raw storage url is NOT present in the body — the old
    // `md.includes(a.url)` check would have returned false here.
    expect(md.includes(att.url)).toBe(false);
    expect(contentReferencesAttachment(md, att)).toBe(true);
  });
});
