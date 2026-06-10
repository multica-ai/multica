import { describe, expect, it } from "vitest";
import { attachmentDownloadHref } from "./download-url";

const WS = "11bd8321-b6ac-4bee-ae41-6659a5064608";

describe("attachmentDownloadHref", () => {
  it("appends workspace_id to a bare server-relative download path", () => {
    expect(attachmentDownloadHref("/api/attachments/abc/download", WS)).toBe(
      `/api/attachments/abc/download?workspace_id=${WS}`,
    );
  });

  it("uses & when the path already has a query string", () => {
    expect(attachmentDownloadHref("/api/attachments/abc/download?x=1", WS)).toBe(
      `/api/attachments/abc/download?x=1&workspace_id=${WS}`,
    );
  });

  it("leaves absolute CDN/signed URLs untouched", () => {
    const signed = "https://cdn.example.test/file.png?Signature=s";
    expect(attachmentDownloadHref(signed, WS)).toBe(signed);
  });

  it("does not double-append when a workspace identifier is already present", () => {
    const withId = `/api/attachments/abc/download?workspace_id=${WS}`;
    expect(attachmentDownloadHref(withId, WS)).toBe(withId);
    const withSlug = "/api/attachments/abc/download?workspace_slug=acme";
    expect(attachmentDownloadHref(withSlug, WS)).toBe(withSlug);
  });

  it("returns the input unchanged when there is no workspace id", () => {
    expect(attachmentDownloadHref("/api/attachments/abc/download", "")).toBe(
      "/api/attachments/abc/download",
    );
  });

  it("handles an empty/undefined download url", () => {
    expect(attachmentDownloadHref("", WS)).toBe("");
    expect(attachmentDownloadHref(undefined, WS)).toBe("");
  });
});
