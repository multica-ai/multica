import { beforeEach, describe, expect, it, vi } from "vitest";

const mockUseCasesSource = vi.hoisted(() => ({
  getPages: vi.fn(),
  getPage: vi.fn(),
}));

vi.mock("fumadocs-core/source", () => ({
  loader: vi.fn(() => mockUseCasesSource),
}));

vi.mock("@/.source", () => ({
  useCases: {
    toFumadocsSource: vi.fn(() => ({})),
  },
}));

import { mergeUseCasePagesWithEnglishFallback } from "./use-case-locale-fallback";
import {
  getUseCasePageForLocale,
  getUseCasePagesForLocale,
  useCasesSource,
} from "./use-cases-source";

beforeEach(() => {
  vi.mocked(useCasesSource.getPages).mockReset();
  vi.mocked(useCasesSource.getPage).mockReset();
});

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

describe("use case source locale fallback", () => {
  it("keeps localized and English-only pages in the production index wrapper", () => {
    const localizedPage = {
      slugs: ["localized"],
      data: { title: "Localized" },
    };
    const englishDuplicate = {
      slugs: ["localized"],
      data: { title: "English duplicate" },
    };
    const englishOnly = {
      slugs: ["english-only"],
      data: { title: "English only" },
    };

    vi.mocked(useCasesSource.getPages).mockImplementation((lang?: string) => {
      if (lang === "ko") {
        return [localizedPage] as ReturnType<typeof useCasesSource.getPages>;
      }
      if (lang === "en") {
        return [
          englishDuplicate,
          englishOnly,
        ] as ReturnType<typeof useCasesSource.getPages>;
      }
      return [] as ReturnType<typeof useCasesSource.getPages>;
    });

    expect(
      getUseCasePagesForLocale("ko").map((page) => page.slugs.join("/")),
    ).toEqual(["localized", "english-only"]);
  });

  it("falls back to English-only detail pages in the production detail wrapper", () => {
    const localizedPage = {
      slugs: ["localized"],
      data: { title: "Localized" },
    };
    const englishOnly = {
      slugs: ["english-only"],
      data: { title: "English only" },
    };

    vi.mocked(useCasesSource.getPage).mockImplementation(
      (slugs: string[] | undefined, lang?: string) => {
        const key = `${lang}:${slugs?.join("/") ?? ""}`;
        if (key === "ko:localized") {
          return localizedPage as ReturnType<typeof useCasesSource.getPage>;
        }
        if (key === "en:english-only") {
          return englishOnly as ReturnType<typeof useCasesSource.getPage>;
        }
        return undefined as ReturnType<typeof useCasesSource.getPage>;
      },
    );

    expect(getUseCasePageForLocale(["localized"], "ko")).toBe(localizedPage);
    expect(getUseCasePageForLocale(["english-only"], "ko")).toBe(englishOnly);
  });
});
