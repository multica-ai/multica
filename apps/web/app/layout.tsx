import type { Metadata, Viewport } from "next";
import Script from "next/script";
import { ThemeProvider } from "@/components/theme-provider";
import { Toaster } from "@multica/ui/components/ui/sonner";
import { WebProviders } from "@/components/web-providers";
import { RESOURCES } from "@multica/views/locales";
import { getRequestLocale } from "@/lib/request-locale";
import { HTML_LANG } from "@/lib/html-lang";
import { SITE_TITLE, TITLE_TEMPLATE } from "@/platform/document-title";
import {
  resolveBrowserApiBaseUrl,
  resolveBrowserWsUrl,
} from "@/config/runtime-urls";
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
    default: SITE_TITLE,
    template: TITLE_TEMPLATE,
  },
  description:
    "Open-source platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills.",
  icons: {
    icon: [{ url: "/favicon.svg", type: "image/svg+xml" }],
    shortcut: ["/favicon.svg"],
    // iOS never reads the manifest's icons for the home screen; it needs its
    // own opaque, full-bleed square and rounds the corners itself.
    apple: [{ url: "/icons/apple-touch-icon.png", sizes: "180x180" }],
  },
  // Home-screen behaviour: launch without browser chrome, and label the icon
  // "Multica" rather than the long SEO <title>. `capable` renders the
  // standardised `mobile-web-app-capable` tag — Next 16 no longer emits the
  // deprecated apple-prefixed spelling, so iOS standalone rides on the
  // manifest's `display` instead (honoured since iOS 16.4).
  appleWebApp: {
    capable: true,
    title: "Multica",
    // `default` keeps the web view below the status bar. Going edge-to-edge
    // (`black-translucent` + viewport-fit=cover) needs env(safe-area-inset-*)
    // padding, which no surface in the app has yet.
    statusBarStyle: "default",
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

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const locale = await getRequestLocale();
  const resources = { [locale]: RESOURCES[locale] };
  const apiBaseUrl = resolveBrowserApiBaseUrl(process.env);
  const wsUrl = resolveBrowserWsUrl(process.env);

  return (
    <html
      lang={HTML_LANG[locale]}
      suppressHydrationWarning
      className="antialiased font-sans h-full"
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
          <WebProviders
            locale={locale}
            resources={resources}
            apiBaseUrl={apiBaseUrl}
            wsUrl={wsUrl}
          >
            {children}
          </WebProviders>
          <Toaster />
        </ThemeProvider>
      </body>
    </html>
  );
}
