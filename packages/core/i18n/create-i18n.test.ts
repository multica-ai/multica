// @vitest-environment node
import { expect, it } from "vitest";
import { createI18n } from "./create-i18n";

it("falls back from Traditional Chinese to Simplified Chinese before English", () => {
  const i18n = createI18n("zh-Hant", {
    "zh-Hant": { common: { title: "工作區" } },
    "zh-Hans": { common: { title: "工作区" }, engineering: { title: "工程" } },
    en: { common: { title: "Workspace", fallback: "English fallback" }, engineering: { title: "Engineering" } },
  });
  expect(i18n.t("title", { ns: "common" })).toBe("工作區");
  expect(i18n.t("title", { ns: "engineering" })).toBe("工程");
  expect(i18n.t("fallback", { ns: "common" })).toBe("English fallback");
});
