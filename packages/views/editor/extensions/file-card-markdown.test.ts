import { describe, it, expect } from "vitest";
import { FileCardExtension } from "./file-card";
import { ImageExtension } from "./index";
import { preprocessFileCards } from "@multica/ui/markdown";

const fileCardRenderMarkdown = FileCardExtension.config.renderMarkdown as (
  node: { attrs: Record<string, string> },
) => string;

const tokenizer = FileCardExtension.config.markdownTokenizer!;
const tokenize = tokenizer.tokenize as (
  src: string,
) => { type: string; raw: string; attributes: Record<string, string> } | undefined;

const imageRenderMarkdown = ImageExtension.config.renderMarkdown as (
  node: { attrs: Record<string, string> },
) => string;

// ---------------------------------------------------------------------------
// ImageExtension.renderMarkdown
// ---------------------------------------------------------------------------
describe("ImageExtension.renderMarkdown", () => {
  it("escapes special chars in alt text", () => {
    const md = imageRenderMarkdown({
      attrs: { src: "https://cdn.example.com/img.png", alt: "6P4N\\`X[A~Z(S@XO}WE0FT_P.jpg" },
    });
    expect(md).toContain("\\\\");
    expect(md).toContain("\\[");
    expect(md).toContain("\\(");
    expect(md).toMatch(/^!\[.*\]\(https:\/\/cdn\.example\.com\/img\.png\)$/);
  });

  it("leaves normal alt text unchanged", () => {
    const md = imageRenderMarkdown({
      attrs: { src: "https://cdn.example.com/img.png", alt: "screenshot" },
    });
    expect(md).toBe("![screenshot](https://cdn.example.com/img.png)");
  });
});

// ---------------------------------------------------------------------------
// file-card tokenizer round-trip
// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------
// A placeholder is not content
// ---------------------------------------------------------------------------
// The document IS the persisted draft body, so anything renderMarkdown emits
// for an in-flight upload becomes text that outlives the upload. Both node
// types must stay silent until they hold a real URL.
describe("in-flight placeholders never serialise", () => {
  it("emits nothing for an uploading fileCard", () => {
    // Serialising this used to write `!file[x.pdf]()` into the draft — a line
    // the tokenizer below cannot parse back, so it survived as dead literal
    // text and shipped with the comment.
    expect(
      fileCardRenderMarkdown({
        attrs: { filename: "x.pdf", href: "", uploading: true } as never,
      }),
    ).toBe("");
  });

  it("emits nothing for a fileCard with no href, even when not marked uploading", () => {
    expect(
      fileCardRenderMarkdown({ attrs: { filename: "x.pdf", href: "" } as never }),
    ).toBe("");
  });

  it("emits nothing for an uploading image", () => {
    // Its src is a process-local blob: URL that dies on reload. This replaces
    // the BLOB_IMAGE_RE scrub ContentEditor used to run after the fact.
    expect(
      imageRenderMarkdown({
        attrs: { src: "blob:http://localhost/abc", alt: "x.png", uploading: true } as never,
      }),
    ).toBe("");
  });

  it("emits normally once the upload settled into a real URL", () => {
    expect(
      fileCardRenderMarkdown({
        attrs: { filename: "x.pdf", href: "/api/attachments/a1/download", uploading: false } as never,
      }),
    ).toBe("!file[x.pdf](/api/attachments/a1/download)");
    expect(
      imageRenderMarkdown({
        attrs: { src: "/api/attachments/a2/download", alt: "x.png", uploading: false } as never,
      }),
    ).toBe("![x.png](/api/attachments/a2/download)");
  });
});

describe("file-card tokenizer", () => {
  it("round-trips a filename with all special chars", () => {
    const filename = "report[final](v2)\\draft.pdf";
    const md = fileCardRenderMarkdown({
      attrs: { href: "https://cdn.example.com/f.pdf", filename },
    });
    const token = tokenize(md);
    expect(token).toBeDefined();
    expect(token!.attributes.filename).toBe(filename);
    expect(token!.attributes.href).toBe("https://cdn.example.com/f.pdf");
  });

  it("round-trips a normal filename", () => {
    const md = fileCardRenderMarkdown({
      attrs: { href: "https://cdn.example.com/readme.md", filename: "readme.md" },
    });
    const token = tokenize(md);
    expect(token).toBeDefined();
    expect(token!.attributes.filename).toBe("readme.md");
  });

  it("rejects an unterminated file card with escape-pair runs in linear time", () => {
    // Each "\a" pair is ambiguous under (?:\\.|[^\]]) — the pre-fix regex
    // enumerates 2^28 backtrack paths (~10s) before failing. The disjoint
    // char class must fail fast instead.
    const src = `!file[${"\\a".repeat(28)}](/uploads/x`;

    const t0 = performance.now();
    const token = tokenize(src);
    const elapsed = performance.now() - t0;

    expect(token).toBeUndefined();
    expect(elapsed).toBeLessThan(100);
  });
});

// ---------------------------------------------------------------------------
// preprocessFileCards
// ---------------------------------------------------------------------------
describe("preprocessFileCards", () => {
  it("converts escaped file-card syntax and unescapes the filename", () => {
    const input = "!file[notes\\[v2\\]\\(draft\\).txt](https://cdn.example.com/notes.txt)";
    const result = preprocessFileCards(input, "cdn.example.com");
    expect(result).toContain('data-type="fileCard"');
    expect(result).toContain('data-filename="notes[v2](draft).txt"');
    expect(result).toContain('data-href="https://cdn.example.com/notes.txt"');
  });

  it("converts a normal file-card syntax", () => {
    const input = "!file[readme.md](https://cdn.example.com/readme.md)";
    const result = preprocessFileCards(input, "cdn.example.com");
    expect(result).toContain('data-type="fileCard"');
    expect(result).toContain('data-filename="readme.md"');
  });

  it("rejects an unterminated file card with escape-pair runs in linear time", () => {
    // This path runs on every read-only comment/description render, so a
    // backtracking label regex here is reachable without opening the editor.
    const input = `!file[${"\\a".repeat(28)}](/uploads/x`;

    const t0 = performance.now();
    const result = preprocessFileCards(input, "cdn.example.com");
    const elapsed = performance.now() - t0;

    expect(result).toBe(input);
    expect(elapsed).toBeLessThan(100);
  });
});
