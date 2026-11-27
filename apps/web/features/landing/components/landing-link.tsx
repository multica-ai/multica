"use client";

import NextLink from "next/link";
import type { ComponentProps } from "react";
import { isDocsHref } from "@/lib/docs-href";
import { localizeHref } from "@/lib/landing-locale-path";
import { useOptionalLocale } from "../i18n/context";

/**
 * `next/link` for the marketing site: links to marketing pages stay in the
 * language being read (`/download` → `/zh/download`); links to the app and
 * other sites pass through. Docs links render as native anchors instead: the
 * docs site is a separate Next.js app proxied under /docs, so a next/link
 * there prefetches the docs app's RSC payload and then requests the docs
 * app's chunks from this app's /_next/static, which 404s (see #9245).
 */
export default function Link({ href, ...props }: ComponentProps<typeof NextLink>) {
  const locale = useOptionalLocale();
  const localized = locale && typeof href === "string" ? localizeHref(locale, href) : href;
  if (typeof localized === "string" && isDocsHref(localized)) {
    // The site never passes next/link-only props (prefetch, replace, scroll,
    // onNavigate), but the spread must still drop them for the anchor types
    // to hold.
    const { prefetch, replace, scroll, onNavigate, ...anchorProps } = props;
    return <a href={localized} {...anchorProps} />;
  }
  return <NextLink href={localized} {...props} />;
}
