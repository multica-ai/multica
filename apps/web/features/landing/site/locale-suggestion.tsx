"use client";

import { useEffect, useState } from "react";
import { matchLocale } from "@multica/core/i18n";
import { createBrowserCookieLocaleAdapter } from "@multica/core/i18n/browser";
import { XIcon } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { HTML_LANG } from "@/lib/html-lang";
import { landingPathLocale, type LandingPathLocale } from "@/lib/landing-locale-path";
import { createLandingDict } from "../i18n/dictionary";
import { useLocale } from "../i18n";
import { Container, focusRing } from "./primitives";

const DISMISSED_KEY = "multica-locale-suggestion-dismissed";

function wasDismissed() {
  try {
    return window.localStorage.getItem(DISMISSED_KEY) !== null;
  } catch {
    return false;
  }
}

/**
 * A bar above the English pages offering the visitor's browser language when
 * the site is translated into it. It only asks: the English URL stays English
 * until the visitor picks a language, here or in the footer. It shows to
 * visitors who have not picked one and have not dismissed it, and never in
 * the server render, so crawlers and shared links always get the same page.
 */
export function LocaleSuggestion() {
  const { locale, setLocale } = useLocale();
  const [target, setTarget] = useState<Exclude<LandingPathLocale, "en"> | null>(null);
  const onEnglishUrl = landingPathLocale(locale) === "en";

  useEffect(() => {
    if (!onEnglishUrl) return;
    const adapter = createBrowserCookieLocaleAdapter();
    if (adapter.getUserChoice() || wasDismissed()) return;
    const preferred = landingPathLocale(matchLocale(adapter.getSystemPreferences()));
    if (preferred !== "en") setTarget(preferred);
  }, [onEnglishUrl]);

  if (!target || !onEnglishUrl) return null;
  const copy = createLandingDict(target).site.localeSuggestion;

  return (
    <div lang={HTML_LANG[target]} className="border-b border-site-rule bg-site-hover text-site-small text-site-ink">
      <Container className="relative flex min-h-10 items-center justify-center gap-x-3 gap-y-1 py-2 pr-12 max-sm:flex-wrap">
        <p className="text-site-muted">{copy.message}</p>
        <button
          type="button"
          onClick={() => setLocale(target)}
          className={cn("rounded-sm font-medium underline underline-offset-4 hover:text-site-ink-secondary", focusRing)}
        >
          {copy.action}
        </button>
        <button
          type="button"
          aria-label={copy.dismiss}
          onClick={() => {
            try {
              window.localStorage.setItem(DISMISSED_KEY, "1");
            } catch {
              // Without storage the bar comes back on the next visit.
            }
            setTarget(null);
          }}
          className={cn(
            "absolute right-site-gutter inline-flex size-7 items-center justify-center rounded-site-control text-site-muted transition-colors hover:bg-site-canvas hover:text-site-ink",
            focusRing,
          )}
        >
          <XIcon aria-hidden className="size-4" />
        </button>
      </Container>
    </div>
  );
}
