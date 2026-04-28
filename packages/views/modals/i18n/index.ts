"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { ModalsDict } from "./types";

const factories: Record<Locale, () => ModalsDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useModalsT(): ModalsDict {
  return useDict<ModalsDict>(factories);
}

export type { ModalsDict } from "./types";
