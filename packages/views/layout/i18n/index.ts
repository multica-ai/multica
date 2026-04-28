"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { LayoutDict } from "./types";

const factories: Record<Locale, () => LayoutDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useLayoutT(): LayoutDict {
  return useDict<LayoutDict>(factories);
}

export type { LayoutDict } from "./types";
