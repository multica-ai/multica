"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { ProjectsDict } from "./types";

const factories: Record<Locale, () => ProjectsDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useProjectsT(): ProjectsDict {
  return useDict<ProjectsDict>(factories);
}

export type { ProjectsDict } from "./types";
