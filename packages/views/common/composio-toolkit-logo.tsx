"use client";

import { useEffect, useState } from "react";
import { cn } from "@multica/ui/lib/utils";

const COMPOSIO_LOGO_BASE_URL = "https://logos.composio.dev/api";

export function composioToolkitLogoUrl(slug: string, theme?: "dark") {
  const normalized = slug.trim().toLowerCase();
  if (!normalized) return "";
  const base = `${COMPOSIO_LOGO_BASE_URL}/${encodeURIComponent(normalized)}`;
  return theme === "dark" ? `${base}?theme=dark` : base;
}

function uniqueNonEmpty(values: Array<string | null | undefined>) {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const value of values) {
    const normalized = value?.trim();
    if (!normalized || seen.has(normalized)) continue;
    seen.add(normalized);
    out.push(normalized);
  }
  return out;
}

export function ComposioToolkitLogo({
  slug,
  name,
  fallbackLogo,
  className,
}: {
  slug: string;
  name?: string;
  fallbackLogo?: string | null;
  className?: string;
}) {
  const label = name || slug;
  const initial = label.charAt(0).toUpperCase();
  const dynamicLightLogo = composioToolkitLogoUrl(slug);
  const dynamicDarkLogo = composioToolkitLogoUrl(slug, "dark");
  const lightSources = uniqueNonEmpty([fallbackLogo, dynamicLightLogo]);
  const darkSources = uniqueNonEmpty([dynamicDarkLogo, fallbackLogo, dynamicLightLogo]);
  const [failedLightSources, setFailedLightSources] = useState(0);
  const [failedDarkSources, setFailedDarkSources] = useState(0);

  useEffect(() => {
    setFailedLightSources(0);
    setFailedDarkSources(0);
  }, [slug, fallbackLogo]);

  const imgClassName = cn("h-8 w-8 shrink-0 rounded bg-muted object-contain", className);
  const fallbackClassName = cn(
    "h-8 w-8 shrink-0 items-center justify-center rounded bg-muted text-xs font-semibold text-muted-foreground",
    className,
  );
  const lightSrc = lightSources[failedLightSources];
  const darkSrc = darkSources[failedDarkSources];

  return (
    <>
      {lightSrc ? (
        <img
          src={lightSrc}
          alt=""
          className={cn(imgClassName, "dark:hidden")}
          onError={() => setFailedLightSources((n) => n + 1)}
        />
      ) : (
        <div className={cn(fallbackClassName, "flex dark:hidden")}>{initial}</div>
      )}
      {darkSrc ? (
        <img
          src={darkSrc}
          alt=""
          className={cn(imgClassName, "hidden dark:block")}
          onError={() => setFailedDarkSources((n) => n + 1)}
        />
      ) : (
        <div className={cn(fallbackClassName, "hidden dark:flex")}>{initial}</div>
      )}
    </>
  );
}
