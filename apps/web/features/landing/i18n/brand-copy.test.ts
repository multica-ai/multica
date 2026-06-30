import { describe, expect, it } from "vitest";
import { createEnDict } from "./en";
import { createZhDict } from "./zh";

describe("landing brand copy", () => {
  it.each([
    ["English", createEnDict(true)],
    ["Simplified Chinese", createZhDict(true)],
  ])("uses CoStrict in current %s marketing copy", (_locale, dict) => {
    expect(dict.hero.subheading).toContain("CoStrict");
    expect(dict.hero.imageAlt).toContain("CoStrict");
    expect(dict.openSource.description).toContain("CoStrict");
    expect(dict.faq.items[0]?.question).toContain("CoStrict");
    expect(dict.about.title).toContain("CoStrict");
    expect(dict.about.nameLine.prefix).not.toContain("Multiplexed");
    expect(dict.changelog.subtitle).toContain("CoStrict");
    expect(dict.contactSales.pageDescription).toContain("CoStrict");
    expect(dict.contactSales.fields.useCase).toContain("CoStrict");
    expect(dict.contactSales.success.message).toContain("CoStrict");
    expect(dict.download.hero.macArm64.title).toContain("Multica");
    expect(dict.download.hero.macArm64.sub).toContain("CoStrict");
    expect(dict.download.cli.title).toContain("Multica CLI");
    expect(dict.download.cli.sub).toContain("CoStrict");
  });

  it("keeps the legal entity name unchanged", () => {
    expect(createEnDict(true).contactSales.consent.intro).toContain(
      "Multica, Inc.",
    );
    expect(createZhDict(true).contactSales.consent.intro).toContain(
      "Multica, Inc.",
    );
  });
});
