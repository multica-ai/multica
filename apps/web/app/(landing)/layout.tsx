import { LocaleProvider } from "@/features/landing/i18n";
import { LocaleSuggestion } from "@/features/landing/site/locale-suggestion";
import { jsonLdScript, siteJsonLd } from "@/features/landing/structured-data";
import { getRequestLocale } from "@/lib/request-locale";

export default async function LandingLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const initialLocale = await getRequestLocale();

  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: jsonLdScript(siteJsonLd) }}
      />
      <div className="landing-light h-full overflow-x-hidden overflow-y-auto bg-white">
        <LocaleProvider initialLocale={initialLocale}>
          <LocaleSuggestion />
          {children}
        </LocaleProvider>
      </div>
    </>
  );
}
