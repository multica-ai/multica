import { cookies, headers } from "next/headers";
import { Instrument_Serif, Noto_Serif_SC } from "next/font/google";
import { LOCALE_COOKIE } from "@multica/core/i18n";
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
      name: "Asymbl",
      url: "https://asymbl.com",
    },
    {
      "@type": "SoftwareApplication",
      name: "Forge",
      applicationCategory: "ProjectManagement",
      operatingSystem: "Web",
      description:
        "Asymbl Forge — AI-native project workspace where agents are first-class teammates.",
    },
  ],
};

async function getInitialLocale(): Promise<Locale> {
  // 1. User's explicit preference (cookie set when they switch language)
  const cookieStore = await cookies();
  const stored = cookieStore.get(LOCALE_COOKIE)?.value;
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
