import { describe, expect, it } from "vitest";
import { docsHrefForLocale } from "./docs-href";

describe("docsHrefForLocale", () => {
  it("routes each supported locale to the matching docs entry", () => {
    expect(docsHrefForLocale("en")).toBe("/docs");
    expect(docsHrefForLocale("zh-Hans")).toBe("/docs/zh");
    expect(docsHrefForLocale("ko")).toBe("/docs/ko");
    expect(docsHrefForLocale("ja")).toBe("/docs/ja");
  });
});
