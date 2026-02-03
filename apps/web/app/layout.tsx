import type { Metadata } from "next";
import { Geist, Geist_Mono, Inter, Playfair_Display } from "next/font/google";
import { useGatewayStore } from "@multica/store";
import "@multica/ui/globals.css";
import {
  SidebarProvider,
  SidebarInset,
} from "@multica/ui/components/ui/sidebar";
import { AppSidebar } from "@multica/ui/components/app-sidebar";
import { ThemeProvider } from "@multica/ui/components/theme-provider";
import { Toaster } from "@multica/ui/components/ui/sonner";
import { HubSidebar } from "@multica/ui/components/hub-sidebar";
import { ServiceWorkerRegister } from "./sw-register";

const gatewayUrl = process.env.NEXT_PUBLIC_GATEWAY_URL;
if (gatewayUrl) {
  useGatewayStore.getState().setGatewayUrl(gatewayUrl);
}

const inter = Inter({ subsets: ["latin"], variable: "--font-sans" });

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

const playfair = Playfair_Display({
  variable: "--font-brand",
  subsets: ["latin"],
  weight: ["400"],
});

export const metadata: Metadata = {
  title: "Multica",
  description: "Distributed AI agent framework",
  appleWebApp: {
    capable: true,
    statusBarStyle: "black-translucent",
    title: "Multica",
  },
  icons: {
    apple: "/icon-192x192.png",
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" className={inter.variable} suppressHydrationWarning>
      <body
        className={`${geistSans.variable} ${geistMono.variable} ${playfair.variable} antialiased`}
      >
        <ThemeProvider
          attribute="class"
          defaultTheme="system"
          enableSystem
          disableTransitionOnChange
        >
          <SidebarProvider>
            <AppSidebar>
              <HubSidebar />
            </AppSidebar>
            <SidebarInset>
              <div className="flex h-dvh overflow-hidden">{children}</div>
            </SidebarInset>
          </SidebarProvider>
        </ThemeProvider>
        <Toaster />
        <ServiceWorkerRegister />
      </body>
    </html>
  );
}
