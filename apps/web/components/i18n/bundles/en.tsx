"use client";

import localeResources from "@multica/views/locales/en";
import type { LocaleBundleProps } from "../locale-bundle";

const resources = { "en": localeResources };

export default function Bundle({ children }: LocaleBundleProps) {
  return <>{children(resources)}</>;
}
