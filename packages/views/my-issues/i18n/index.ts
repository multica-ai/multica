"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { MyIssuesDict } from "./types";

const factories: Record<Locale, () => MyIssuesDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useMyIssuesT(): MyIssuesDict {
  return useDict<MyIssuesDict>(factories);
}

export type { MyIssuesDict } from "./types";
