// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  extensionToLanguage,
  getPreviewKind,
  isPreviewable,
  structuredFormat,
  tableDelimiter,
  wrapsByDefault,
  type PreviewKind,
} from "./preview";
import {
  TEXT_PREVIEW_BASENAMES,
  TEXT_PREVIEW_CONTENT_TYPE_PREFIXES,
  TEXT_PREVIEW_CONTENT_TYPES,
  TEXT_PREVIEW_EXTENSIONS,
} from "./text-preview-whitelist.generated";

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

describe("generated text preview whitelist", () => {
  const TEXT_BACKED = new Set<PreviewKind>(["markdown", "html", "table", "structured", "text"]);

  function expectTextBacked(contentType: string, filename: string) {
    const kind = getPreviewKind(contentType, filename);
    expect(kind !== null && TEXT_BACKED.has(kind)).toBe(true);
  }

  it("accepts every generated content-type prefix", () => {
    for (const prefix of TEXT_PREVIEW_CONTENT_TYPE_PREFIXES) {
      expectTextBacked(`${prefix}plain`, "payload.bin");
    }
  });

  it("accepts every generated exact content type", () => {
    for (const contentType of TEXT_PREVIEW_CONTENT_TYPES) {
      expectTextBacked(contentType, "payload");
    }
  });

  it("accepts every generated extension", () => {
    for (const extension of TEXT_PREVIEW_EXTENSIONS) {
      expectTextBacked("application/octet-stream", `source.${extension}`);
    }
  });

  it("accepts every generated basename", () => {
    for (const basename of TEXT_PREVIEW_BASENAMES) {
      expectTextBacked("application/octet-stream", basename);
    }
  });

  it.each([
    ["application/pdf", "doc.pdf"],
    ["image/png", "shot.png"],
    ["video/mp4", "clip.mp4"],
    ["application/octet-stream", "blob.bin"],
    [
      "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
      "report.docx",
    ],
  ])("rejects non-text proxy input (%s, %s)", (contentType, filename) => {
    const kind = getPreviewKind(contentType, filename);
    expect(kind !== null && TEXT_BACKED.has(kind)).toBe(false);
  });
});

describe("text preview whitelist removals", () => {
  afterEach(() => vi.restoreAllMocks());

  it.each<[string, PreviewKind]>([
    ["md", "markdown"],
    ["markdown", "markdown"],
    ["html", "html"],
    ["htm", "html"],
    ["csv", "table"],
    ["tsv", "table"],
    ["json", "structured"],
    ["jsonl", "structured"],
    ["ndjson", "structured"],
    ["yaml", "structured"],
    ["yml", "structured"],
    ["go", "text"],
  ])("hides .%s previews when the extension is removed", (extension, kind) => {
    const filename = `source.${extension}`;
    expect(getPreviewKind("application/octet-stream", filename)).toBe(kind);

    const remaining = new Set(TEXT_PREVIEW_EXTENSIONS);
    remaining.delete(extension);
    vi.spyOn(TEXT_PREVIEW_EXTENSIONS, "has").mockImplementation((value) => remaining.has(value));

    expect(getPreviewKind("application/octet-stream", filename)).toBeNull();
    // A matching MIME still grants access, just as it does on the server.
    expect(getPreviewKind("text/plain", filename)).toBe(kind);
  });

  it.each([
    "application/json",
    "application/x-ndjson",
    "application/yaml",
    "application/x-yaml",
  ])("hides extension-less %s previews when the MIME is removed", (contentType) => {
    expect(getPreviewKind(contentType, "payload")).toBe("structured");

    const remaining = new Set(TEXT_PREVIEW_CONTENT_TYPES);
    remaining.delete(contentType);
    vi.spyOn(TEXT_PREVIEW_CONTENT_TYPES, "has").mockImplementation((value) => remaining.has(value));

    expect(getPreviewKind(contentType, "payload")).toBeNull();
    expect(getPreviewKind(contentType, "payload.json")).toBe("structured");
  });

  it.each([
    "text/markdown", "text/html", "text/csv", "text/tab-separated-values",
    "text/yaml", "text/x-yaml", "text/plain",
  ])("hides extension-less %s previews when the text/ prefix is removed", (contentType) => {
    const remaining = new Set(TEXT_PREVIEW_CONTENT_TYPE_PREFIXES);
    remaining.delete("text/");
    vi.spyOn(TEXT_PREVIEW_CONTENT_TYPE_PREFIXES, Symbol.iterator)
      .mockImplementation(() => remaining.values());

    expect(getPreviewKind(contentType, "payload")).toBeNull();
  });

  it.each<[string, string, PreviewKind]>([
    ["application/pdf", "page.html", "pdf"],
    ["video/mp4", "report.csv", "video"],
    ["audio/mpeg", "README.md", "audio"],
    ["image/png", "data.json", "image"],
    ["application/octet-stream", "manual.pdf", "pdf"],
    ["application/octet-stream", "clip.mp4", "video"],
    ["application/octet-stream", "note.mp3", "audio"],
    ["application/octet-stream", "shot.png", "image"],
  ])("keeps %s / %s media previews independent of the text whitelist", (contentType, filename, kind) => {
    vi.spyOn(TEXT_PREVIEW_CONTENT_TYPE_PREFIXES, Symbol.iterator)
      .mockImplementation(() => new Set<string>().values());
    vi.spyOn(TEXT_PREVIEW_CONTENT_TYPES, "has").mockReturnValue(false);
    vi.spyOn(TEXT_PREVIEW_EXTENSIONS, "has").mockReturnValue(false);
    vi.spyOn(TEXT_PREVIEW_BASENAMES, "has").mockReturnValue(false);

    expect(getPreviewKind(contentType, filename)).toBe(kind);
  });
});
