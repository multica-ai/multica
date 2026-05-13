import { describe, expect, it } from "vitest";

import { COUNTRY_OPTIONS, findCityCode, findRegionCode, loadCityOptions, loadRegionOptions, localizedName, localizedSort } from "./geo";

describe("CRM geo helpers", () => {
  it("loads countries, regions, and cities through browser-safe JSON data imports", async () => {
    expect(COUNTRY_OPTIONS.length).toBeGreaterThan(200);
    expect(COUNTRY_OPTIONS.some((country) => country.code === "CN")).toBe(true);
    expect(COUNTRY_OPTIONS.some((country) => country.code === "US")).toBe(true);

    const usRegions = await loadRegionOptions("US");
    expect(usRegions.length).toBeGreaterThan(50);
    expect(usRegions).toContainEqual({ code: "CA", name: { en: "California", zh: "California" }, cities: [] });

    const californiaCities = await loadCityOptions("US", "CA");
    expect(californiaCities.length).toBeGreaterThan(1000);
    expect(californiaCities.some((city) => city.name.en === "Acalanes Ridge")).toBe(true);
    expect(new Set(californiaCities.map((city) => city.code)).size).toBe(californiaCities.length);
  });
  it("sorts localized options by pinyin for Chinese and by first letters for English", () => {
    const options = [
      { code: "BJ", name: { en: "Beijing", zh: "北京" } },
      { code: "GD", name: { en: "Guangdong", zh: "广东" } },
      { code: "AH", name: { en: "Anhui", zh: "安徽" } },
      { code: "ZJ", name: { en: "Zhejiang", zh: "浙江" } },
    ];

    expect(localizedSort(options, "zh-Hans").map((option) => localizedName(option.name, "zh-Hans"))).toEqual([
      "安徽",
      "北京",
      "广东",
      "浙江",
    ]);
    expect(localizedSort(options, "en").map((option) => option.name.en)).toEqual([
      "Anhui",
      "Beijing",
      "Guangdong",
      "Zhejiang",
    ]);
  });

  it("matches stored country, region, and city display values back to cascading option codes", async () => {
    expect(await findRegionCode("CN", "Guangdong")).toBe("GD");
    expect(await findRegionCode("CN", "广东")).toBe("GD");
    const shenzhenCode = await findCityCode("CN", "GD", "Shenzhen");
    expect(shenzhenCode).not.toBe("");
    const guangdongCities = await loadCityOptions("CN", "GD");
    expect(guangdongCities.find((city) => city.code === shenzhenCode)?.name.en).toBe("Shenzhen");
  });

});
