"use client";

import localeResources from "@multica/views/locales/id";
import type { LocaleBundleProps } from "../locale-bundle";

const resources = { "id": localeResources };

export default function Bundle({ children }: LocaleBundleProps) {
  return <>{children(resources)}</>;
}
