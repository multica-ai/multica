import { describe, expect, it } from "vitest";
import type { TFunction } from "i18next";
import { createI18n } from "@multica/core/i18n/react";
import enAutopilots from "../../../locales/en/autopilots.json";
import zhAutopilots from "../../../locales/zh-Hans/autopilots.json";
import { describeSchedule } from "./describe";
import { parseCron } from "./cron-mapping";
import { getDefaultScheduleConfig } from "./model";

// The description is the editor's plain-language readback of the structured
// model. Advanced-only expressions have no structured model to describe, so
// they render as the raw cron plus the server preview instead.

const TZ = "UTC";

function fixedT(
  locale: "en" | "zh-Hans",
  resources: Record<string, unknown>,
): TFunction<"autopilots"> {
  const i18n = createI18n(locale, { [locale]: { autopilots: resources } });
  return i18n.getFixedT(locale, "autopilots") as TFunction<"autopilots">;
}

describe("describeSchedule (en)", () => {
  const t = fixedT("en", enAutopilots);
  const describeExpr = (expr: string) => describeSchedule(t, parseCron(expr, TZ));

  // Clause order mirrors the editor's fields, top to bottom: time (with its
  // window, which belongs to the same dimension), then days.
  it.each([
    ["0 9 * * *", "At 09:00 · Every day"],
    ["30 18 * * 1-5", "At 18:30 · Mon–Fri"],
    ["0 9 * * 1,3,5", "At 09:00 · Mon, Wed, Fri"],
    ["15 * * * *", "Every hour at :15 · Every day"],
    ["0 */2 * * *", "Every 2 hours at :00 · Every day"],
    ["0 9-21/2 * * 2-4", "Every 2 hours · 09:00–21:00 · Tue–Thu"],
    ["30 9-21 * * *", "Every hour · 09:30–21:30 · Every day"],
    ["*/10 9-18 * * 1-5", "Every 10 minutes · 09:00–18:59 · Mon–Fri"],
    ["* * * * *", "Every minute · Every day"],
    ["30 10 15 * *", "At 10:30 · Day 15 of the month"],
  ])("%s → %s", (expr, expected) => {
    expect(describeExpr(expr)).toBe(expected);
  });

  it("returns null for advanced-only configs", () => {
    expect(describeExpr("0 9 1,15 * *")).toBeNull();
    expect(describeSchedule(t, { ...getDefaultScheduleConfig(TZ), raw: "@daily" })).toBeNull();
  });

  it("leaves no unresolved interpolation placeholders", () => {
    for (const expr of ["0 9-21/2 * * 2-4", "15 * * * *", "30 10 15 * *"]) {
      expect(describeExpr(expr)).not.toContain("{{");
    }
  });
});

describe("describeSchedule (zh-Hans)", () => {
  const t = fixedT("zh-Hans", zhAutopilots);
  const describeExpr = (expr: string) => describeSchedule(t, parseCron(expr, TZ));

  it.each([
    ["0 9 * * *", "09:00 · 每天"],
    ["0 9-21/2 * * 2-4", "每 2 小时 · 09:00–21:00 · 周二至周四"],
    ["0 9 * * 1,3,5", "09:00 · 周一、周三、周五"],
    ["30 10 15 * *", "10:30 · 每月 15 日"],
  ])("%s → %s", (expr, expected) => {
    expect(describeExpr(expr)).toBe(expected);
  });
});
