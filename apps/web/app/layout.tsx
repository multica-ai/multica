import type { Metadata, Viewport } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import { ThemeProvider } from "@/components/theme-provider";
import { Toaster } from "@/components/ui/sonner";
import { cn } from "@/lib/utils";
import { AuthInitializer } from "@/features/auth";
import { WSProvider } from "@/features/realtime";
import { ModalRegistry } from "@/features/modals";
import "./globals.css";

const geist = Geist({ subsets: ["latin"], variable: "--font-sans" });
const geistMono = Geist_Mono({ subsets: ["latin"], variable: "--font-mono" });

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  maximumScale: 5,
  userScalable: true,
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#ffffff" },
    { media: "(prefers-color-scheme: dark)", color: "#0a0a0a" },
  ],
};

export const metadata: Metadata = {
  metadataBase: new URL("https://multica.ai"),
  title: {
    default: "Multica — Turn Coding Agents into Real Teammates",
    template: "%s | Multica",
  },
  description:
    "Multica is an open-source platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills — manage your human + agent workforce in one place.",
  keywords: [
    "AI task management",
    "coding agents",
    "AI teammates",
    "agent workforce",
    "open source project management",
    "AI-native task management",
    "Linear alternative",
  ],
  icons: {
    icon: [{ url: "/favicon.svg", type: "image/svg+xml" }],
    shortcut: ["/favicon.svg"],
  },
  openGraph: {
    type: "website",
    locale: "en_US",
    url: "https://multica.ai",
    siteName: "Multica",
    title: "Multica — Turn Coding Agents into Real Teammates",
    description:
      "Assign tasks, track progress, compound skills — manage your human + agent workforce in one place.",
    images: [
      {
        url: "/og-image.png",
        width: 1200,
        height: 630,
        alt: "Multica — AI-native task management platform",
      },
    ],
  },
  twitter: {
    card: "summary_large_image",
    title: "Multica — Turn Coding Agents into Real Teammates",
    description:
      "Assign tasks, track progress, compound skills — manage your human + agent workforce in one place.",
    images: ["/og-image.png"],
  },
  alternates: {
    canonical: "/",
  },
  robots: {
    index: true,
    follow: true,
  },
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={cn("antialiased font-sans h-full", geist.variable, geistMono.variable)}
    >
      <body className="h-full overflow-hidden">
        <ThemeProvider>
          <AuthInitializer>
            <WSProvider>{children}</WSProvider>
          </AuthInitializer>
          <ModalRegistry />
          <Toaster />
        </ThemeProvider>
      </body>
    </html>
  );
}
