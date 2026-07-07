import enCommon from "./en/common.json";
import zhHansCommon from "./zh-Hans/common.json";

export type SupportedLocale = "en" | "zh-Hans";

export const RESOURCES = {
  en: {
    common: enCommon,
  },
  "zh-Hans": {
    common: zhHansCommon,
  },
} satisfies Record<SupportedLocale, Record<string, Record<string, unknown>>>;
