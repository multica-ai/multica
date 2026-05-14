"use client";

import Link from "next/link";
import {
  createContext,
  useContext,
  type AnchorHTMLAttributes,
  type ReactNode,
} from "react";
import { i18n, type Lang } from "@/lib/i18n";
import { prefixLocale } from "@/lib/locale-link";

const DocsLocaleContext = createContext<Lang>(i18n.defaultLanguage as Lang);

// Wraps the rendered MDX subtree so descendant <LocaleLink>s and any
// editorial component using `useDocsLocale()` know which language the page
// was rendered in. Mounted at each docs page entry; never elsewhere.
export function DocsLocaleProvider({
  lang,
  children,
}: {
  lang: Lang;
  children: ReactNode;
}) {
  return (
    <DocsLocaleContext.Provider value={lang}>
      {children}
    </DocsLocaleContext.Provider>
  );
}

export function useDocsLocale(): Lang {
  return useContext(DocsLocaleContext);
}

// Drop-in replacement for the MDX-rendered `<a>` element. Keeps the same
// surface shape as the default `a` from `defaultMdxComponents` but routes
// internal links through the locale prefixer + next/link so client-side
// navigation stays inside the active locale.
export function LocaleLink({
  href,
  ...rest
}: AnchorHTMLAttributes<HTMLAnchorElement> & { href?: string }) {
  const lang = useDocsLocale();
  if (!href) return <a {...rest} />;
  const final = prefixLocale(href, lang);
  return <Link href={final} {...rest} />;
}
