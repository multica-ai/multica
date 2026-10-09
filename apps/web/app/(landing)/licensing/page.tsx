import type { Metadata } from "next";
import { LicensingPageClient } from "@/features/landing/components/licensing-page-client";

export const metadata: Metadata = {
  title: "Licensing",
  description:
    "When you can use Multica for free and when you need a commercial license, explained with common scenarios.",
  openGraph: {
    title: "Licensing — Multica",
    description:
      "Free to self-host inside your organization. Offering Multica to others as a hosted service needs a commercial license.",
    url: "/licensing",
  },
  alternates: {
    canonical: "/licensing",
  },
};

export default function LicensingPage() {
  return <LicensingPageClient />;
}
