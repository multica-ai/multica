"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { SettingsDict } from "./types";

const factories: Record<Locale, () => SettingsDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useSettingsT(): SettingsDict {
  return useDict<SettingsDict>(factories);
}

export type { SettingsDict } from "./types";
