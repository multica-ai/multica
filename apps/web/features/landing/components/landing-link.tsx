"use client";

import NextLink from "next/link";
import type { ComponentProps } from "react";
import { localizeHref } from "@/lib/landing-locale-path";
import { useOptionalLocale } from "../i18n/context";

/**
 * `next/link` for the marketing site: links to marketing pages stay in the
 * language being read (`/download` → `/zh/download`); links to the app, docs
 * and other sites pass through.
 */
export default function Link({ href, ...props }: ComponentProps<typeof NextLink>) {
  const locale = useOptionalLocale();
  return <NextLink href={locale && typeof href === "string" ? localizeHref(locale, href) : href} {...props} />;
}
