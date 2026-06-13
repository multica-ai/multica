import type { Viewport } from "next";
import { MobileShell } from "@/features/mobile/mobile-shell";

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
};

export default function Layout({ children }: { children: React.ReactNode }) {
  return <MobileShell>{children}</MobileShell>;
}
