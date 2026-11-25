import type { Metadata } from "next";
import { pageMetadata } from "@/features/landing/utils/page-metadata";
import { HomePage } from "@/features/landing/site/home-page";
import { getHomeDownload } from "@/features/landing/utils/home-download";
import { RedirectIfAuthenticated } from "@/features/landing/components/redirect-if-authenticated";
import { createLandingDict } from "@/features/landing/i18n/dictionary";
import { faqPageJsonLd, jsonLdScript } from "@/features/landing/structured-data";
import { getRequestLocale } from "@/lib/request-locale";

export function generateMetadata(): Promise<Metadata> {
  return pageMetadata("home", "/");
}

export default async function LandingPage() {
  const { faq } = createLandingDict(await getRequestLocale());

  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: jsonLdScript(faqPageJsonLd(faq)) }}
      />
      <RedirectIfAuthenticated />
      <HomePage download={await getHomeDownload()} />
    </>
  );
}
