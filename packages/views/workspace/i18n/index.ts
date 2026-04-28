"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { WorkspaceDict } from "./types";

const factories: Record<Locale, () => WorkspaceDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useWorkspaceT(): WorkspaceDict {
  return useDict<WorkspaceDict>(factories);
}

export type { WorkspaceDict } from "./types";
