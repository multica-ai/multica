import { describe, expect, it } from "vitest";
import {
  attachmentDownloadPath,
  attachmentIdFromDownloadURL,
  attachmentIdFromLocalUploadURL,
  attachmentIdFromStorageURL,
  attachmentMediaURL,
  contentReferencesAttachment,
  isAttachmentCapabilityURL,
  stripChannelMediaMarkers,
} from "./attachment-url";

const ID = "11111111-2222-3333-4444-555555555555";

describe("attachmentDownloadPath", () => {
  it("returns the stable per-attachment download path", () => {
    expect(attachmentDownloadPath(ID)).toBe(`/api/attachments/${ID}/download`);
  });
});

describe("stripChannelMediaMarkers", () => {
  it("removes provenance while retaining the visible channel image", () => {
    const image = `![](${attachmentDownloadPath(ID)})`;
    const marker = `<!-- multica:channel-media:${ID} -->`;

    expect(stripChannelMediaMarkers(`${image}\n\n${marker}`)).toBe(`${image}\n\n`);
  });

  it("leaves unrelated HTML comments untouched", () => {
    expect(stripChannelMediaMarkers("before <!-- user note --> after")).toBe(
      "before <!-- user note --> after",
    );
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

describe("attachmentIdFromStorageURL", () => {
  const WS = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee";
  const CDN = "static.multica.ai";

  it("extracts the attachment id from a workspace object on the CDN", () => {
    expect(attachmentIdFromStorageURL(`https://${CDN}/workspaces/${WS}/${ID}.jpg`, CDN)).toBe(ID);
  });

  it("accepts an object without an extension and ignores query strings", () => {
    expect(attachmentIdFromStorageURL(`https://${CDN}/workspaces/${WS}/${ID}?x=1`, CDN)).toBe(ID);
  });

  it("ignores URLs on another host", () => {
    expect(
      attachmentIdFromStorageURL(`https://example.com/workspaces/${WS}/${ID}.jpg`, CDN),
    ).toBeUndefined();
  });

  it("ignores non-workspace paths and an unset CDN domain", () => {
    expect(
      attachmentIdFromStorageURL(`https://${CDN}/users/${WS}/${ID}.jpg`, CDN),
    ).toBeUndefined();
    expect(
      attachmentIdFromStorageURL(`https://${CDN}/workspaces/${WS}/${ID}.jpg`, ""),
    ).toBeUndefined();
    expect(attachmentIdFromStorageURL(`/workspaces/${WS}/${ID}.jpg`, CDN)).toBeUndefined();
  });
});

describe("isAttachmentCapabilityURL / attachmentMediaURL (MUL-8859)", () => {
  const capability = `https://multica.example.test/api/attachments/${ID}/signed-download?Expires=1&Signature=a`;

  it("recognizes the server's capability URL, absolute or site-relative", () => {
    expect(isAttachmentCapabilityURL(capability)).toBe(true);
    expect(isAttachmentCapabilityURL(`/api/attachments/${ID}/signed-download?exp=1&sig=a`)).toBe(true);
    expect(isAttachmentCapabilityURL(`/api/attachments/${ID}/download`)).toBe(false);
    expect(isAttachmentCapabilityURL("https://cdn.example.test/x.png?Signature=a")).toBe(false);
    expect(isAttachmentCapabilityURL("")).toBe(false);
  });

  it("skips a capability download_url for the durable markdown_url", () => {
    const markdown = `https://multica.example.test/api/attachments/${ID}/download`;
    expect(attachmentMediaURL({ url: "/uploads/x.png", download_url: capability, markdown_url: markdown })).toBe(markdown);
    expect(attachmentMediaURL({ url: "/uploads/x.png", download_url: capability })).toBe("/uploads/x.png");
  });

  it("keeps any other download_url first", () => {
    const signed = "https://cdn.example.test/x.png?Signature=a";
    expect(attachmentMediaURL({ url: "u", download_url: signed, markdown_url: "m" })).toBe(signed);
  });
});

describe("attachmentIdFromLocalUploadURL", () => {
  const WS = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee";

  it("extracts the id from site-relative and absolute local-disk paths", () => {
    expect(attachmentIdFromLocalUploadURL(`/uploads/workspaces/${WS}/${ID}.png`)).toBe(ID);
    expect(
      attachmentIdFromLocalUploadURL(`https://api.example.test/uploads/workspaces/${WS}/${ID}?x=1`),
    ).toBe(ID);
  });

  it("ignores other shapes", () => {
    expect(attachmentIdFromLocalUploadURL(`/uploads/users/${WS}/${ID}.png`)).toBeUndefined();
    expect(attachmentIdFromLocalUploadURL(`/uploads/workspaces/${WS}/lark/${ID}.png`)).toBeUndefined();
    expect(attachmentIdFromLocalUploadURL(`/workspaces/${WS}/${ID}.png`)).toBeUndefined();
    expect(attachmentIdFromLocalUploadURL("")).toBeUndefined();
  });
});
