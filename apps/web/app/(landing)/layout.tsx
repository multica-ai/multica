import { Instrument_Serif, Noto_Serif_SC } from "next/font/google";
import { LocaleProvider } from "@/features/landing/i18n";
import { getRequestLocale } from "@/lib/request-locale";
import { getServerGithubConfig } from "@/lib/github-config";

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

function buildJsonLd(githubWebUrl: string) {
  return {
    "@context": "https://schema.org",
    "@graph": [
      {
        "@type": "Organization",
        name: "Multica",
        url: "https://www.multica.ai",
        sameAs: [githubWebUrl],
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
}

export default async function LandingLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const initialLocale = await getRequestLocale();
  const jsonLd = buildJsonLd(getServerGithubConfig().webUrl);

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
