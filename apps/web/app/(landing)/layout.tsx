import { cookies, headers } from "next/headers";
import { Instrument_Serif, Noto_Serif_SC } from "next/font/google";
import { LocaleProvider } from "@/features/landing/i18n";
import type { Locale } from "@/features/landing/i18n";

const instrumentSerif = Instrument_Serif({
  subsets: ["latin"],
  weight: "400",
  variable: "--font-serif",
});

const notoSerifSC = Noto_Serif_SC({
  subsets: ["latin"],
  weight: "400",
  variable: "--font-serif-zh",
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
      <div className={`${instrumentSerif.variable} ${notoSerifSC.variable} landing-light h-full overflow-x-hidden overflow-y-auto bg-white`}>
        <LocaleProvider initialLocale={initialLocale}>{children}</LocaleProvider>
      </div>
    </>
  );
}
