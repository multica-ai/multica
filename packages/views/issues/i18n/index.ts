"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { IssuesDict } from "./types";

const factories: Record<Locale, () => IssuesDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useIssuesT(): IssuesDict {
  return useDict<IssuesDict>(factories);
}

export type { IssuesDict } from "./types";
