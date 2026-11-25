"use client";

import localeResources from "@multica/views/locales/pt-BR";
import type { LocaleBundleProps } from "../locale-bundle";

const resources = { "pt-BR": localeResources };

export default function Bundle({ children }: LocaleBundleProps) {
  return <>{children(resources)}</>;
}
