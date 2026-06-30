import { describe, expect, it } from "vitest";
import { createI18n } from "./create-i18n";

const resources = {
  en: {
    common: {
      welcome: "Welcome to {{productName}}",
    },
  },
};

describe("createI18n", () => {
  it("defaults shared product copy to Multica", () => {
    const i18n = createI18n("en", resources);

    expect(i18n.t("common:welcome")).toBe("Welcome to Multica");
  });

  it("allows a host app to override shared product copy", () => {
    const i18n = createI18n("en", resources, { productName: "CoStrict" });

    expect(i18n.t("common:welcome")).toBe("Welcome to CoStrict");
  });
});
