import { describe, expect, it } from "vitest";
import { isViewableAttachment, viewableKind } from "./viewable";

describe("viewableKind", () => {
  it.each([
    ["text/html", "anything", "html"],
    ["text/markdown", "anything", "markdown"],
    ["application/json", "anything", "json"],
    ["application/pdf", "anything", "pdf"],
    ["text/plain", "anything", "text"],
    ["text/html; charset=utf-8", "x.html", "html"],
  ] as const)("recognises %s by content_type", (ct, name, expected) => {
    expect(viewableKind(ct, name)).toBe(expected);
  });

  it.each([
    ["application/octet-stream", "plan.html", "html"],
    ["application/octet-stream", "PLAN.HTM", "html"],
    ["application/octet-stream", "notes.md", "markdown"],
    ["application/octet-stream", "data.JSON", "json"],
    ["application/octet-stream", "manual.PDF", "pdf"],
    ["application/octet-stream", "log.txt", "text"],
  ] as const)(
    "falls back to extension for %s/%s",
    (ct, name, expected) => {
      expect(viewableKind(ct, name)).toBe(expected);
    },
  );

  it.each([
    ["image/png", "x.png"],
    ["application/octet-stream", "binary.bin"],
    ["application/octet-stream", "noext"],
  ])("returns null for non-viewable %s/%s", (ct, name) => {
    expect(viewableKind(ct, name)).toBeNull();
  });
});

describe("isViewableAttachment", () => {
  it("returns true when viewableKind resolves", () => {
    expect(isViewableAttachment("text/html", "x.html")).toBe(true);
    expect(isViewableAttachment("application/octet-stream", "x.md")).toBe(true);
    expect(isViewableAttachment("application/pdf", "doc.pdf")).toBe(true);
  });

  it("returns false otherwise", () => {
    expect(isViewableAttachment("image/png", "x.png")).toBe(false);
    expect(isViewableAttachment("application/octet-stream", "doc.bin")).toBe(false);
  });
});
