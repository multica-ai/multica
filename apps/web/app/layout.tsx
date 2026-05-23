import type { Metadata, Viewport } from "next";
import Script from "next/script";
import { Inter, Geist_Mono, Source_Serif_4 } from "next/font/google";
import { ThemeProvider } from "@/components/theme-provider";
import { Toaster } from "@multica/ui/components/ui/sonner";
import { cn } from "@multica/ui/lib/utils";
import { WebProviders } from "@/components/web-providers";
import type { SupportedLocale } from "@multica/core/i18n";
import { RESOURCES } from "@multica/views/locales";
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

// HTML lang attribute uses BCP-47 region tags that screen readers and font
// stacks recognize widely. i18next keeps `zh-Hans` as its internal locale
// (script subtag is what we actually translate against), but the html element
// expects a region-flavoured tag for accessibility tooling and CJK fallback.
const HTML_LANG: Record<SupportedLocale, string> = {
  en: "en",
  "zh-Hans": "zh-CN",
  ko: "ko-KR",
  ja: "ja-JP",
  es: "es",
};

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const locale = await getRequestLocale();
  const resources = { [locale]: RESOURCES[locale] };

  return (
    <html
      lang={HTML_LANG[locale]}
      suppressHydrationWarning
      className={cn("antialiased font-sans h-full", inter.variable, geistMono.variable, sourceSerif.variable)}
    >
      <body className="h-full overflow-hidden">
        {/*
          react-grab: dev-only element inspector. Hold ⌘C (Mac) / Ctrl+C and click
          any element to copy its source path + line + component stack for pasting
          to an AI. Opt-in per developer: only loads when VITE_REACT_GRAB is set in
          a local, gitignored apps/web/.env.local — it never activates for anyone
          else. Both guards are read server-side, so the <Script> is omitted from
          the HTML entirely unless you opted in. The VITE_ prefix is shared with the
          desktop renderer (apps/desktop/src/renderer/src/main.tsx), where Vite only
          exposes VITE_-prefixed vars to client code, so one var name covers both
          apps. See https://www.react-grab.com/
        */}
        {process.env.NODE_ENV === "development" && process.env.VITE_REACT_GRAB && (
          <Script
            src="//unpkg.com/react-grab/dist/index.global.js"
            crossOrigin="anonymous"
            strategy="beforeInteractive"
          />
        )}
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
