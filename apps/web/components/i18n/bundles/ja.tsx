"use client";

import localeResources from "@multica/views/locales/ja";
import type { LocaleBundleProps } from "../locale-bundle";

const resources = { "ja": localeResources };

export default function Bundle({ children }: LocaleBundleProps) {
  return <>{children(resources)}</>;
}
