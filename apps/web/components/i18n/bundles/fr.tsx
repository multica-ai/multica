"use client";

import localeResources from "@multica/views/locales/fr";
import type { LocaleBundleProps } from "../locale-bundle";

const resources = { "fr": localeResources };

export default function Bundle({ children }: LocaleBundleProps) {
  return <>{children(resources)}</>;
}
