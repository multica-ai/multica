import { describe, expect, it } from "vitest";
import { mergeUseCasePagesWithEnglishFallback } from "./use-case-locale-fallback";

describe("mergeUseCasePagesWithEnglishFallback", () => {
  it("keeps localized pages ahead of English fallback pages", () => {
    const localizedPages = [
      { slugs: ["localized"], data: { title: "Localized" } },
    ];
    const englishPages = [
      { slugs: ["localized"], data: { title: "English duplicate" } },
      { slugs: ["english-only"], data: { title: "English only" } },
    ];

    expect(
      mergeUseCasePagesWithEnglishFallback(localizedPages, englishPages),
    ).toEqual([
      { slugs: ["localized"], data: { title: "Localized" } },
      { slugs: ["english-only"], data: { title: "English only" } },
    ]);
  });

  it("dedupes nested slugs by full path", () => {
    const localizedPages = [{ slugs: ["teams", "ops"] }];
    const englishPages = [
      { slugs: ["teams", "ops"] },
      { slugs: ["teams", "support"] },
    ];

    expect(
      mergeUseCasePagesWithEnglishFallback(localizedPages, englishPages).map(
        (page) => page.slugs.join("/"),
      ),
    ).toEqual(["teams/ops", "teams/support"]);
  });
});
