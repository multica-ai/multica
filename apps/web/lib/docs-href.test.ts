// @vitest-environment node
import { describe, expect, it } from "vitest";
import { docsHrefForLocale, isDocsHref } from "./docs-href";

describe("docsHrefForLocale", () => {
  it("routes each supported locale to the matching docs entry", () => {
    expect(docsHrefForLocale("en")).toBe("/docs");
    expect(docsHrefForLocale("zh-Hans")).toBe("/docs/zh");
    expect(docsHrefForLocale("zh-Hant")).toBe("/docs/zh-Hant");
    expect(docsHrefForLocale("ko")).toBe("/docs/ko");
    expect(docsHrefForLocale("ja")).toBe("/docs/ja");
    expect(docsHrefForLocale("fr")).toBe("/docs/fr");
  });
});

describe("isDocsHref", () => {
  it("matches the docs app's paths", () => {
    expect(isDocsHref("/docs")).toBe(true);
    expect(isDocsHref("/docs/zh")).toBe(true);
    expect(isDocsHref("/docs/zh/channels")).toBe(true);
    expect(isDocsHref("/docs/cli#run")).toBe(true);
  });

  it("does not match same-app or unrelated hrefs", () => {
    expect(isDocsHref("/")).toBe(false);
    expect(isDocsHref("/download")).toBe(false);
    expect(isDocsHref("/zh/download")).toBe(false);
    expect(isDocsHref("/docsx")).toBe(false);
    expect(isDocsHref("/documentation")).toBe(false);
    expect(isDocsHref("https://example.com/docs")).toBe(false);
    expect(isDocsHref("mailto:docs@example.com")).toBe(false);
  });
});
