import type { Metadata, Viewport } from "next";
import { headers } from "next/headers";
import { SerwistProvider } from "@serwist/next/react";
import { ThemeProvider } from "@/components/theme-provider";
import { Toaster } from "@multica/ui/components/ui/sonner";
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
  // Lock the layout viewport to 1× so iOS does not auto-zoom when the device
  // is rotated portrait → landscape → portrait (JEH-1735). Without this, the
  // installed PWA returns from a rotation visibly zoomed in, which breaks the
  // "behaves like a native app" expectation. iOS system-level accessibility
  // zoom still works independently of these flags.
  maximumScale: 1,
  userScalable: false,
  // Cover the entire device viewport including notch / home indicator on iOS
  // so installed-PWA pages can opt into safe-area-inset-* via CSS.
  viewportFit: "cover",
  // Shrink the layout viewport when the on-screen keyboard opens (instead of
  // the default "resizes-visual" which keeps layout viewport tall and makes
  // the keyboard overlap content). Combined with our h-svh layout this keeps
  // the page header pinned and the input row visible above the keyboard.
  interactiveWidget: "resizes-content",
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
  manifest: "/manifest.webmanifest",
  applicationName: "Multica",
  appleWebApp: {
    capable: true,
    title: "Multica",
    // "default" gives a translucent status bar that picks up theme-color.
    statusBarStyle: "default",
  },
  icons: {
    icon: [
      { url: "/favicon.svg", type: "image/svg+xml" },
      { url: "/icons/icon-192.png", type: "image/png", sizes: "192x192" },
      { url: "/icons/icon-512.png", type: "image/png", sizes: "512x512" },
    ],
    apple: [{ url: "/icons/apple-touch-icon.png", sizes: "180x180" }],
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
      className="h-full font-sans antialiased"
    >
      <body className="h-full overflow-hidden">
        {/* CEREBRO-PATCH(web-serwist): wrap with SerwistProvider for service worker */}
        <SerwistProvider
          swUrl="/sw.js"
          disable={process.env.NODE_ENV === "development"}
          reloadOnOnline
        >
          <ThemeProvider>
            <WebProviders locale={locale} resources={resources}>
              {children}
            </WebProviders>
            <Toaster />
          </ThemeProvider>
        </SerwistProvider>
      </body>
    </html>
  );
}
