import { describe, expect, it } from "vitest";
import {
  extensionToLanguage,
  getPreviewKind,
  isPreviewable,
  type PreviewKind,
} from "./preview";

describe("getPreviewKind", () => {
  const cases: Array<[string, string, PreviewKind | null]> = [
    // Media types — typed correctly server-side
    ["application/pdf", "manual.pdf", "pdf"],
    ["video/mp4", "clip.mp4", "video"],
    ["audio/mpeg", "note.mp3", "audio"],

    // Markdown — both well-typed and sniffer-fallback paths
    ["text/markdown", "README", "markdown"],
    ["text/plain", "README.md", "markdown"],
    ["application/octet-stream", "notes.markdown", "markdown"],

    // HTML — both content-type and extension paths
    ["text/html", "page", "html"],
    ["application/octet-stream", "page.html", "html"],

    // Code / config — fallback to text after sniffer guesses "text/plain"
    ["text/plain", "main.go", "text"],
    ["application/octet-stream", "main.go", "text"],
    ["text/plain", "config.yml", "text"],
    ["application/javascript", "bundle.js", "text"],
    ["application/json", "data.json", "text"],

    // Plain text
    ["text/plain", "log.txt", "text"],

    // Build files without extension
    ["application/octet-stream", "Dockerfile", "text"],
    ["application/octet-stream", "Makefile", "text"],

    // Out of scope
    ["application/vnd.openxmlformats-officedocument.wordprocessingml.document", "report.docx", null],
    ["application/octet-stream", "blob.bin", null],
    ["application/zip", "archive.zip", null],
  ];

  for (const [ct, filename, want] of cases) {
    it(`(${ct}, ${filename}) → ${want}`, () => {
      expect(getPreviewKind(ct, filename)).toBe(want);
    });
  }

  // PDF should dispatch from extension alone when content_type is wrong.
  it("falls through to extension when content_type is mislabeled", () => {
    expect(getPreviewKind("application/octet-stream", "manual.pdf")).toBe("pdf");
  });
});

describe("isPreviewable", () => {
  it("is true for any non-null PreviewKind", () => {
    expect(isPreviewable("application/pdf", "x.pdf")).toBe(true);
    expect(isPreviewable("text/plain", "x.txt")).toBe(true);
  });

  it("is false for unsupported types", () => {
    expect(isPreviewable("application/zip", "x.zip")).toBe(false);
    expect(isPreviewable("application/octet-stream", "x.bin")).toBe(false);
  });
});

describe("extensionToLanguage", () => {
  it("maps common code extensions to hljs language tokens", () => {
    expect(extensionToLanguage("index.ts")).toBe("typescript");
    expect(extensionToLanguage("main.go")).toBe("go");
    expect(extensionToLanguage("script.py")).toBe("python");
    expect(extensionToLanguage("style.scss")).toBe("scss");
  });

  it("falls back to plaintext for non-code text files", () => {
    expect(extensionToLanguage("log.txt")).toBe("plaintext");
  });

  it("recognizes extension-less build files", () => {
    expect(extensionToLanguage("Dockerfile")).toBe("dockerfile");
    expect(extensionToLanguage("Makefile")).toBe("makefile");
  });

  it("returns undefined for unknown extensions", () => {
    expect(extensionToLanguage("blob.bin")).toBeUndefined();
    expect(extensionToLanguage("noextension")).toBeUndefined();
  });
});
