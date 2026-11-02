// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseInlineLinks } from "./inline-links";
import { createLandingDict } from "../i18n/dictionary";

describe("parseInlineLinks", () => {
  it("returns plain text untouched", () => {
    expect(parseInlineLinks("No links here.")).toEqual([
      { type: "text", text: "No links here." },
    ]);
  });

  it("splits links at any position in the sentence", () => {
    expect(
      parseInlineLinks("See [Licensing](/licensing) or [Discord](https://x.y)."),
    ).toEqual([
      { type: "text", text: "See " },
      { type: "link", label: "Licensing", href: "/licensing" },
      { type: "text", text: " or " },
      { type: "link", label: "Discord", href: "https://x.y" },
      { type: "text", text: "." },
    ]);
    expect(parseInlineLinks("[授权说明](/licensing)里写清楚了。")).toEqual([
      { type: "link", label: "授权说明", href: "/licensing" },
      { type: "text", text: "里写清楚了。" },
    ]);
  });
});

describe("trust pages copy", () => {
  const dicts = (["en", "zh-Hans", "ja", "ko"] as const).map((locale) =>
    createLandingDict(locale, true),
  );

  it("links the privacy consent to the privacy policy in every locale", () => {
    for (const t of dicts) {
      expect(t.contactSales.consent.privacyLinkHref).toBe("/privacy");
    }
  });

  it("keeps the placeholder legal entity out of every locale", () => {
    for (const t of dicts) {
      expect(JSON.stringify(t)).not.toContain("Multica, Inc");
    }
  });

  it("keeps every locale's FAQ in step, including the licensing link", () => {
    const [en, ...others] = dicts;
    for (const t of others) {
      expect(t.faq.items).toHaveLength(en!.faq.items.length);
    }
    for (const t of dicts) {
      expect(t.faq.items.some((i) => i.answer.includes("](/licensing)"))).toBe(
        true,
      );
    }
  });

  it("translates the licensing and privacy pages in every locale", () => {
    const [en, ...others] = dicts;
    for (const t of others) {
      expect(t.licensing.title).not.toBe(en!.licensing.title);
      expect(t.privacy.title).not.toBe(en!.privacy.title);
      expect(t.licensing.scenarios.items.map((i) => i.required)).toEqual(
        en!.licensing.scenarios.items.map((i) => i.required),
      );
      expect(t.privacy.sections).toHaveLength(en!.privacy.sections.length);
    }
  });
});
