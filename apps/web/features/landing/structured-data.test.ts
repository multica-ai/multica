// @vitest-environment node
import { describe, expect, it } from "vitest";
import { createLandingDict } from "./i18n/dictionary";
import { faqPageJsonLd, jsonLdScript, plainInlineText, siteJsonLd } from "./structured-data";

describe("siteJsonLd", () => {
  it("lists every platform and official profile", () => {
    const [organization, app] = siteJsonLd["@graph"];
    expect(app).toMatchObject({ operatingSystem: "Web, macOS, Windows, Linux, iOS" });
    expect(organization?.sameAs).toEqual([
      "https://github.com/multica-ai/multica",
      "https://x.com/MulticaAI",
      "https://discord.gg/W8gYBn226t",
    ]);
  });
});

describe("faqPageJsonLd", () => {
  it.each(["en", "zh-Hans", "ja", "ko"] as const)("mirrors the %s home page FAQ", (locale) => {
    const { faq } = createLandingDict(locale);
    const data = faqPageJsonLd(faq);

    expect(data["@type"]).toBe("FAQPage");
    expect(data.mainEntity.map((entry) => entry.name)).toEqual(faq.items.map((item) => item.question));
    for (const entry of data.mainEntity) {
      expect(entry.acceptedAnswer.text).not.toMatch(/\]\(/);
    }
  });
});

describe("plainInlineText", () => {
  it("keeps link labels and drops their targets", () => {
    expect(plainInlineText("See the [licensing FAQ](/licensing) for more.")).toBe("See the licensing FAQ for more.");
  });
});

describe("jsonLdScript", () => {
  it("cannot close the script tag it is embedded in", () => {
    const script = jsonLdScript({ text: "</script><script>alert(1)</script>" });
    expect(script).not.toContain("</script>");
    expect(JSON.parse(script)).toEqual({ text: "</script><script>alert(1)</script>" });
  });
});
