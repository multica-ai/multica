// @vitest-environment node

import i18next from "i18next";
import { describe, expect, it } from "vitest";
import en from "./locales/en.json";
import zhHans from "./locales/zh-Hans.json";
import nativeZhHans from "./native/zh-Hans.json";

function keyFor(source: string): string {
  const index = en.copy.indexOf(source);
  if (index < 0) throw new Error(`Missing test source: ${source}`);
  return `mobile:copy.${index}`;
}

describe("Simplified Chinese resources", () => {
  it("keeps mobile locale resources aligned", () => {
    expect(zhHans.copy).toHaveLength(en.copy.length);
    expect(zhHans.copy.every(Boolean)).toBe(true);
  });

  it("translates representative navigation, task, and interpolated copy", () => {
    const i18n = i18next.createInstance();
    i18n.init({
      lng: "zh-Hans",
      fallbackLng: "en",
      initAsync: false,
      resources: {
        en: { mobile: en },
        "zh-Hans": { mobile: zhHans },
      },
    });

    expect(i18n.t(keyFor("Issues"))).toBe("任务");
    expect(i18n.t(keyFor("Agent Runs"))).toBe("智能体运行记录");
    expect(
      i18n.t(keyFor('No results for "{{query}}"'), { query: "RKC-896" }),
    ).toBe("没有找到与“RKC-896”相关的结果");
  });

  it("localizes native iOS permission prompts", () => {
    expect(nativeZhHans.ios.NSPhotoLibraryUsageDescription).toContain("照片");
    expect(nativeZhHans.ios.NSCameraUsageDescription).toContain("相机");
  });
});
