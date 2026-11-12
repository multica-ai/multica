"use client";

import localeResources from "@multica/views/locales/zh-Hans";
import type { LocaleBundleProps } from "../locale-bundle";

const resources = { "zh-Hans": localeResources };

export default function Bundle({ children }: LocaleBundleProps) {
  return <>{children(resources)}</>;
}
