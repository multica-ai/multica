import type { Metadata } from "next";
import { Toaster } from "@multica/ui/components/ui/sonner";
import { ThemeProvider } from "@/components/theme-provider";
import { Providers } from "./providers";
import { Nav } from "@/components/nav";
import "./globals.css";

export const metadata: Metadata = {
  title: "Multica Admin",
  description: "Internal admin dashboard for monitoring AI-agent workspaces.",
  robots: { index: false, follow: false },
};

// No auth in this pass (explicit user decision — see plan's "No auth"
// section). It currently reads directly from Postgres + LiteLLM with no
// access control at all, so middleware.ts refuses to serve any request in
// production unless ADMIN_ALLOW_UNSAFE_NO_AUTH=true is explicitly set —
// gate this app with real auth before flipping that switch.
export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="h-full" suppressHydrationWarning>
      <body className="h-full font-sans antialiased">
        <ThemeProvider>
          <Providers>
            <Nav />
            {children}
          </Providers>
          <Toaster />
        </ThemeProvider>
      </body>
    </html>
  );
}
