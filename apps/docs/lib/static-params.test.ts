import { describe, expect, it } from "vitest";
import { docsSlugStaticParams, type DocsStaticParam } from "./static-params";

describe("docsSlugStaticParams", () => {
  it("adds Korean fallback params for every English slug page", () => {
    const params: DocsStaticParam[] = [
      { lang: "en", slug: [] },
      { lang: "en", slug: ["agents"] },
      { lang: "en", slug: ["cli", "reference"] },
      { lang: "zh", slug: ["agents"] },
    ];

    expect(docsSlugStaticParams(params)).toEqual([
      { lang: "en", slug: ["agents"] },
      { lang: "en", slug: ["cli", "reference"] },
      { lang: "zh", slug: ["agents"] },
      { lang: "ko", slug: ["agents"] },
      { lang: "ko", slug: ["cli", "reference"] },
    ]);
  });

  it("keeps existing localized params and does not duplicate Korean pages", () => {
    const params: DocsStaticParam[] = [
      { lang: "en", slug: ["agents"] },
      { lang: "ko", slug: ["agents"] },
      { lang: "zh", slug: ["guides", "quickstart"] },
    ];

    expect(docsSlugStaticParams(params)).toEqual([
      { lang: "en", slug: ["agents"] },
      { lang: "ko", slug: ["agents"] },
      { lang: "zh", slug: ["guides", "quickstart"] },
    ]);
  });
});
