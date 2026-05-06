import type { Metadata, Viewport } from "next";
import { headers } from "next/headers";
import { ThemeProvider } from "@/components/theme-provider";
import { Toaster } from "@multica/ui/components/ui/sonner";
import { cn } from "@multica/ui/lib/utils";
import { WebProviders } from "@/components/web-providers";
import {
  DEFAULT_LOCALE,
  SUPPORTED_LOCALES,
  type SupportedLocale,
} from "@multica/core/i18n";
import { RESOURCES } from "@multica/views/locales";
import "./globals.css";

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#ffffff" },
    { media: "(prefers-color-scheme: dark)", color: "#05070b" },
  ],
};

export const metadata: Metadata = {
  metadataBase: new URL("https://www.multica.ai"),
  title: {
    default: "Multica — Project Management for Human + Agent Teams",
    template: "%s | Multica",
  },
  description:
    "Open-source platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills.",
  icons: {
    icon: [{ url: "/favicon.svg", type: "image/svg+xml" }],
    shortcut: ["/favicon.svg"],
  },
  openGraph: {
    type: "website",
    siteName: "Multica",
    locale: "en_US",
  },
  twitter: {
    card: "summary_large_image",
    site: "@multica_hq",
    creator: "@multica_hq",
  },
  alternates: {
    canonical: "/",
  },
  robots: {
    index: true,
    follow: true,
  },
};

function isSupportedLocale(value: string | null): value is SupportedLocale {
  return value !== null && (SUPPORTED_LOCALES as readonly string[]).includes(value);
}

// HTML lang attribute uses BCP-47 region tags that screen readers and font
// stacks recognize widely. i18next keeps `zh-Hans` as its internal locale
// (script subtag is what we actually translate against), but the html element
// expects a region-flavoured tag for accessibility tooling and CJK fallback.
const HTML_LANG: Record<SupportedLocale, string> = {
  en: "en",
  "zh-Hans": "zh-CN",
};

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const h = await headers();
  const headerLocale = h.get("x-multica-locale");
  const locale: SupportedLocale = isSupportedLocale(headerLocale)
    ? headerLocale
    : DEFAULT_LOCALE;
  const resources = { [locale]: RESOURCES[locale] };

  return (
    <html
      lang={HTML_LANG[locale]}
      suppressHydrationWarning
      className={cn("antialiased font-sans h-full")}
    >
      <body className="h-full overflow-hidden">
        <ThemeProvider>
          <WebProviders locale={locale} resources={resources}>
            {children}
          </WebProviders>
          <Toaster />
        </ThemeProvider>
      </body>
    </html>
  );
}
