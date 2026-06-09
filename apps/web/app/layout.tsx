import type { Metadata, Viewport } from "next";
import { Inter, Geist_Mono, Source_Serif_4 } from "next/font/google";
// CEREBRO-PATCH(web-serwist): service-worker provider for installed PWA
import { SerwistProvider } from "@serwist/next/react";
import { ThemeProvider } from "@/components/theme-provider";
import { Toaster } from "@multica/ui/components/ui/sonner";
import { cn } from "@multica/ui/lib/utils";
import { WebProviders } from "@/components/web-providers";
import type { SupportedLocale } from "@multica/core/i18n";
import { RESOURCES } from "@multica/views/locales";
import { CEREBRO_AGENT_AVATAR_RESOURCES } from "@multica/cerebro-agent-avatar/locales";
import { getRequestLocale } from "@/lib/request-locale";
import "./globals.css";

// Inter is the Latin UI face. next/font produces a hashed family (`__Inter_xxx`)
// plus a synthetic size-adjusted fallback face to prevent FOUT layout shift —
// both are exposed under the `--font-inter` CSS variable.
//
// The full `--font-sans` stack (Inter + the per-locale CJK fallback chain) is
// assembled in static CSS in ./globals.css, not here: it must be overridable per
// `<html lang>` (Japanese Kanji are Han ideographs and need a Japanese-first CJK
// stack), and a hashed family name can only be referenced from CSS via a variable.
// Keeping the CJK chain in CSS also keeps it CSP-safe and in sync with the desktop
// app, which defines the same chain in apps/desktop/src/renderer/src/globals.css.
const inter = Inter({
  subsets: ["latin"],
  variable: "--font-inter",
});
// Mono font has no explicit CJK fallback: CJK chars in code blocks are inherently
// non-aligned with a mono grid (Chinese is proportional), so listing CJK fonts
// here would falsely signal alignment guarantees. Browser default fallback handles
// the rare mixed case correctly.
const geistMono = Geist_Mono({
  subsets: ["latin"],
  variable: "--font-mono",
  fallback: ["ui-monospace", "SFMono-Regular", "Menlo", "Consolas", "monospace"],
});
// Editorial serif used for onboarding headlines. Italic support for h1 em
// accents (e.g. "...on one shared board."). Only loaded on routes that
// render the font; layout-shift-prevention handled by next/font's synthetic
// fallback metrics, same as Inter.
const sourceSerif = Source_Serif_4({
  subsets: ["latin"],
  style: ["normal", "italic"],
  variable: "--font-serif",
  fallback: [
    "ui-serif",
    "Iowan Old Style",
    "Apple Garamond",
    "Baskerville",
    "Times New Roman",
    "serif",
  ],
});

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

// HTML lang attribute uses BCP-47 region tags that screen readers and font
// stacks recognize widely. i18next keeps `zh-Hans` as its internal locale
// (script subtag is what we actually translate against), but the html element
// expects a region-flavoured tag for accessibility tooling and CJK fallback.
const HTML_LANG: Record<SupportedLocale, string> = {
  en: "en",
  "zh-Hans": "zh-CN",
  ko: "ko-KR",
  ja: "ja-JP",
};

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const locale = await getRequestLocale();
  // CEREBRO-PATCH(web-agent-avatar-locale): inject cerebro agent-avatar i18n resources
  const resources = {
    [locale]: {
      ...RESOURCES[locale],
      "cerebro-agent-avatar": CEREBRO_AGENT_AVATAR_RESOURCES[locale],
    },
  };

  return (
    <html
      lang={HTML_LANG[locale]}
      suppressHydrationWarning
      className={cn("antialiased font-sans h-full", inter.variable, geistMono.variable, sourceSerif.variable)}
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
