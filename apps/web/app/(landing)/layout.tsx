import { createRequire } from "node:module";
import { cookies, headers } from "next/headers";
import localFont from "next/font/local";
import { LocaleProvider } from "@/features/landing/i18n";
import type { Locale } from "@/features/landing/i18n";

// Bundled font files (no Google Fonts fetch at build time — required for offline / restricted Docker builds).
const require = createRequire(import.meta.url);

const instrumentSerif = localFont({
  src: require.resolve(
    "@fontsource/instrument-serif/files/instrument-serif-latin-400-normal.woff2",
  ),
  variable: "--font-serif",
  display: "swap",
});

const notoSerifSC = localFont({
  src: require.resolve(
    "@fontsource/noto-serif-sc/files/noto-serif-sc-latin-400-normal.woff2",
  ),
  variable: "--font-serif-zh",
  display: "swap",
});

const jsonLd = {
  "@context": "https://schema.org",
  "@graph": [
    {
      "@type": "Organization",
      name: "Multica",
      url: "https://www.multica.ai",
      sameAs: ["https://github.com/multica-ai/multica"],
    },
    {
      "@type": "SoftwareApplication",
      name: "Multica",
      applicationCategory: "ProjectManagement",
      operatingSystem: "Web",
      description:
        "Open-source project management platform that turns coding agents into real teammates.",
      offers: {
        "@type": "Offer",
        price: "0",
        priceCurrency: "USD",
      },
    },
  ],
};

async function getInitialLocale(): Promise<Locale> {
  // 1. User's explicit preference (cookie set when they switch language)
  const cookieStore = await cookies();
  const stored = cookieStore.get("multica-locale")?.value;
  if (stored === "en" || stored === "zh") return stored;

  // 2. Detect from Accept-Language header
  const headersList = await headers();
  const acceptLang = headersList.get("accept-language") ?? "";
  if (acceptLang.includes("zh")) return "zh";

  return "en";
}

export default async function LandingLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const initialLocale = await getInitialLocale();

  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }}
      />
      <div className={`${instrumentSerif.variable} ${notoSerifSC.variable} h-full overflow-x-hidden overflow-y-auto bg-white`}>
        <LocaleProvider initialLocale={initialLocale}>{children}</LocaleProvider>
      </div>
    </>
  );
}
