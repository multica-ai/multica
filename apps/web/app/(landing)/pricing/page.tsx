import type { Metadata } from "next";
import { PricingPage } from "@/features/landing/site/pricing-page";
import { PLACEHOLDER_PAGES } from "@/features/landing/site/sitemap";

export const metadata: Metadata = {
  title: PLACEHOLDER_PAGES.pricing.title,
  description: PLACEHOLDER_PAGES.pricing.summary,
  alternates: { canonical: "/pricing" },
  robots: { index: false, follow: true },
};

export default function Page() {
  return <PricingPage />;
}
