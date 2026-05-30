const DEFAULT_SITE_URL = "";

let siteUrl = DEFAULT_SITE_URL;

export function setSiteUrl(url: string) {
  siteUrl = url || DEFAULT_SITE_URL;
}

export function getSiteUrl(): string {
  return siteUrl;
}

export function getDocsUrl(locale: string, slug: string): string {
  const prefix = locale === "en" ? "/docs" : `/docs/${locale}`;
  return `${siteUrl}${prefix}/${slug}`;
}
