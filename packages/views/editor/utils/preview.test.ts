// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  extensionToLanguage,
  getPreviewKind,
  isPreviewable,
  structuredFormat,
  tableDelimiter,
  wrapsByDefault,
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
    ["application/javascript", "bundle.js", "text"],

    // Tables — the sniffer usually says text/plain, so the extension decides
    ["text/csv", "report.csv", "table"],
    ["text/plain", "report.csv", "table"],
    ["application/octet-stream", "export.tsv", "table"],
    ["text/tab-separated-values", "export", "table"],

    // Structured data — tree view
    ["application/json", "data.json", "structured"],
    ["text/plain", "config.yml", "structured"],
    ["application/octet-stream", "compose.yaml", "structured"],
    ["text/plain", "events.jsonl", "structured"],
    ["application/octet-stream", "events.ndjson", "structured"],
    ["application/x-yaml", "values", "structured"],

    // Plain text
    ["text/plain", "log.txt", "text"],

    // Build files without extension
    ["application/octet-stream", "Dockerfile", "text"],
    ["application/octet-stream", "Makefile", "text"],
    ["application/octet-stream", ".env", "text"],
    ["application/octet-stream", ".gitignore", "text"],
    ["application/octet-stream", "service.dockerfile", "text"],
    ["application/octet-stream", "rules.makefile", "text"],

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

describe("structuredFormat", () => {
  it("reads the format from the extension first", () => {
    expect(structuredFormat("text/plain", "a.json")).toBe("json");
    expect(structuredFormat("application/json", "a.jsonl")).toBe("jsonl");
    expect(structuredFormat("text/plain", "a.ndjson")).toBe("jsonl");
    expect(structuredFormat("text/plain", "a.yml")).toBe("yaml");
  });

  it("falls back to the content type for extension-less files", () => {
    expect(structuredFormat("application/json; charset=utf-8", "payload")).toBe("json");
    expect(structuredFormat("application/yaml", "values")).toBe("yaml");
    expect(structuredFormat("text/plain", "notes.txt")).toBeNull();
  });
});

describe("tableDelimiter", () => {
  it("is a tab for TSV and a comma otherwise", () => {
    expect(tableDelimiter("text/plain", "export.tsv")).toBe("\t");
    expect(tableDelimiter("text/tab-separated-values", "export")).toBe("\t");
    expect(tableDelimiter("text/csv", "report.csv")).toBe(",");
  });
});

describe("wrapsByDefault", () => {
  it("wraps prose and logs, keeps code lines intact", () => {
    expect(wrapsByDefault("server.log")).toBe(true);
    expect(wrapsByDefault("notes.txt")).toBe(true);
    expect(wrapsByDefault("LICENSE")).toBe(true);
    expect(wrapsByDefault("main.go")).toBe(false);
    expect(wrapsByDefault("page.html")).toBe(false);
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
    expect(extensionToLanguage(".env")).toBe("plaintext");
    expect(extensionToLanguage(".gitignore")).toBe("plaintext");
  });

  it("recognizes build file extensions allowed by the server", () => {
    expect(extensionToLanguage("service.dockerfile")).toBe("dockerfile");
    expect(extensionToLanguage("rules.makefile")).toBe("makefile");
    expect(extensionToLanguage("nested/.gitignore")).toBe("plaintext");
  });

  it("returns undefined for unknown extensions", () => {
    expect(extensionToLanguage("blob.bin")).toBeUndefined();
    expect(extensionToLanguage("noextension")).toBeUndefined();
  });
});

// Mirror of TestIsTextPreviewable in server/internal/handler/file_test.go,
// the text proxy's whitelist. Keep the two tables identical: a type only the
// client accepts opens to a 415, one only the server accepts never opens.
describe("text preview whitelist (mirrors the server)", () => {
  const TEXT_BACKED = new Set<PreviewKind>(["markdown", "html", "table", "structured", "text"]);
  const cases: Array<[string, string, string, boolean]> = [
    ["markdown by ext", "application/octet-stream", "README.md", true],
    ["markdown by mime", "text/markdown", "README", true],
    ["plain text", "text/plain", "log.txt", true],
    ["json by mime", "application/json", "data.json", true],
    ["yaml by ext", "application/octet-stream", "config.yml", true],
    ["csv by ext", "application/octet-stream", "report.csv", true],
    ["tsv by ext", "application/octet-stream", "export.tsv", true],
    ["json lines by ext", "application/octet-stream", "events.jsonl", true],
    ["ndjson by ext", "application/octet-stream", "events.ndjson", true],
    ["ndjson by mime", "application/x-ndjson", "events", true],
    ["log by ext", "application/octet-stream", "server.log", true],
    ["go source", "text/plain", "main.go", true],
    ["typescript", "application/octet-stream", "index.ts", true],
    ["html", "text/html", "page.html", true],
    ["dockerfile no ext", "application/octet-stream", "Dockerfile", true],
    ["makefile no ext", "application/octet-stream", "Makefile", true],
    ["env dotfile", "application/octet-stream", ".env", true],
    ["gitignore dotfile", "application/octet-stream", ".gitignore", true],
    ["dockerfile extension", "application/octet-stream", "service.dockerfile", true],
    ["makefile extension", "application/octet-stream", "rules.makefile", true],

    ["pdf rejected", "application/pdf", "doc.pdf", false],
    ["png rejected", "image/png", "shot.png", false],
    ["video rejected", "video/mp4", "clip.mp4", false],
    ["binary fallthrough", "application/octet-stream", "blob.bin", false],
    [
      "docx rejected",
      "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
      "report.docx",
      false,
    ],
  ];

  for (const [name, contentType, filename, want] of cases) {
    it(name, () => {
      const kind = getPreviewKind(contentType, filename);
      expect(kind !== null && TEXT_BACKED.has(kind)).toBe(want);
    });
  }
});
