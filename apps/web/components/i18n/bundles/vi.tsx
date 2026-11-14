"use client";

import localeResources from "@multica/views/locales/vi";
import type { LocaleBundleProps } from "../locale-bundle";

const resources = { "vi": localeResources };

export default function Bundle({ children }: LocaleBundleProps) {
  return <>{children(resources)}</>;
}
