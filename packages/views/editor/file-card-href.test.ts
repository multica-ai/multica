import { describe, expect, it } from "vitest";

import {
  FILE_CARD_URL_PATTERN,
  isAllowedFileCardHref,
  preprocessFileCards,
} from "@multica/ui/markdown";

describe("isAllowedFileCardHref", () => {
  it.each([
    ["/uploads/ok", true],
    ["/uploads/workspaces/abc/file.png", true],
    ["https://cdn.example.com/x", true],
    ["http://localhost:8080/uploads/x.png", true],
    ["HTTPS://CDN.EXAMPLE.COM/x", true],
  ])("accepts %s", (href, expected) => {
    expect(isAllowedFileCardHref(href)).toBe(expected);
  });

  it.each([
    ["javascript:alert(1)", false],
    ["JavaScript:alert(1)", false],
    ["data:text/html,xss", false],
    ["//evil.com/x", false],
    ["/../api/x", false],
    ["/api/x", false],
    ["/api/internal/x", false],
    ["", false],
    ["ftp://example.com/x", false],
    ["uploads/x", false],
  ])("rejects %s", (href, expected) => {
    expect(isAllowedFileCardHref(href)).toBe(expected);
  });
});

describe("FILE_CARD_URL_PATTERN", () => {
  // Mirror the parser usage: a fresh anchored regex composed from the pattern.
  const parser = new RegExp(
    `^!file\\[([^\\]]*)\\]\\((${FILE_CARD_URL_PATTERN.source})\\)$`,
  );

  it.each([
    "!file[doc.md](/uploads/x.md)",
    "!file[name](/uploads/workspaces/abc/019e.md)",
    "!file[doc.md](https://cdn.example.com/x.md)",
    "!file[doc.md](http://localhost:8080/uploads/x.md)",
  ])("parses %s", (input) => {
    expect(parser.test(input)).toBe(true);
  });

  it.each([
    "!file[evil.txt](javascript:alert(1))",
    "!file[evil.txt](data:text/html,xss)",
    "!file[evil.txt](//evil.com/x)",
    "!file[evil.txt](/../api/x)",
    "!file[evil.txt](/api/x)",
    "!file[doc.md](uploads/x.md)",
    "!file[doc.md](ftp://example.com/x)",
  ])("does not parse %s", (input) => {
    expect(parser.test(input)).toBe(false);
  });
});

describe("preprocessFileCards (integration)", () => {
  const cdn = "cdn.example.com";

  it("converts !file[…](/uploads/…) into a file-card div", () => {
    const out = preprocessFileCards("!file[doc.md](/uploads/x.md)", cdn);
    expect(out).toContain('data-type="fileCard"');
    expect(out).toContain('data-href="/uploads/x.md"');
    expect(out).toContain('data-filename="doc.md"');
  });

  it("leaves a protocol-relative href untouched (not parsed as file-card)", () => {
    const out = preprocessFileCards("!file[evil.txt](//evil.com/x)", cdn);
    expect(out).not.toContain('data-type="fileCard"');
    expect(out).toBe("!file[evil.txt](//evil.com/x)");
  });

  it("leaves javascript: untouched (not parsed as file-card)", () => {
    const out = preprocessFileCards(
      "!file[evil.txt](javascript:alert(1))",
      cdn,
    );
    expect(out).not.toContain('data-type="fileCard"');
  });

  it("leaves a non-/uploads relative path untouched", () => {
    const out = preprocessFileCards("!file[name](/api/internal/x)", cdn);
    expect(out).not.toContain('data-type="fileCard"');
  });
});
