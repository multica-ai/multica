import type { Metadata } from "next";
import { getRequestLocale } from "@/lib/request-locale";
import { landingAlternates, localizeHref } from "@/lib/landing-locale-path";
import { createLandingDict } from "../i18n/dictionary";
import { toLandingDictionaryLocale, type LandingDictionaryLocale, type MetaPage } from "../i18n/types";
import { SHARE_IMAGE } from "../share-image";

const OG_LOCALE: Record<LandingDictionaryLocale, string> = { en: "en_US", zh: "zh_CN", ko: "ko_KR", ja: "ja_JP" };

/**
 * A marketing page's title, description and share card in the language its
 * URL is in, pointing at its own URL and at its other languages.
 */
export async function pageMetadata(page: MetaPage, path: string): Promise<Metadata> {
  const requested = await getRequestLocale();
  const locale = toLandingDictionaryLocale(requested);
  const meta = createLandingDict(requested).meta[page];
  const url = localizeHref(requested, path);
  return {
    title: page === "home" ? { absolute: meta.title } : meta.title,
    description: meta.description,
    openGraph: {
      images: [SHARE_IMAGE],
      title: meta.ogTitle,
      description: meta.ogDescription,
      url,
      locale: OG_LOCALE[locale],
    },
    alternates: { canonical: url, languages: landingAlternates(path) },
  };
}
