import type { Metadata } from "next";
import { PricingPage } from "@/features/landing/site/pricing-page";
import { PLACEHOLDER_PAGES, liveMetadata, requireLiveRoute } from "@/features/landing/site/sitemap";

export const metadata: Metadata = liveMetadata("/pricing", {
  title: PLACEHOLDER_PAGES.pricing.title,
  description: PLACEHOLDER_PAGES.pricing.summary,
  alternates: { canonical: "/pricing" },
  robots: { index: false, follow: true },
});

export default function Page() {
  requireLiveRoute("/pricing");
  return <PricingPage />;
}
