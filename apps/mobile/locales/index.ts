import enCommon from "./en/common.json";
import enSettings from "./en/settings.json";
import zhHansCommon from "./zh-Hans/common.json";
import zhHansSettings from "./zh-Hans/settings.json";

export type SupportedLocale = "en" | "zh-Hans";

export const RESOURCES = {
  en: {
    common: enCommon,
    settings: enSettings,
  },
  "zh-Hans": {
    common: zhHansCommon,
    settings: zhHansSettings,
  },
} satisfies Record<SupportedLocale, Record<string, Record<string, unknown>>>;
