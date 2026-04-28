"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { AgentsDict } from "./types";

const factories: Record<Locale, () => AgentsDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useAgentsT(): AgentsDict {
  return useDict<AgentsDict>(factories);
}

export type { AgentsDict } from "./types";
