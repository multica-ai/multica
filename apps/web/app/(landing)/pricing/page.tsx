import type { Metadata } from "next";
import { PricingPage } from "@/features/landing/site/pricing-page";
import { liveMetadata, requireLiveRoute } from "@/features/landing/site/sitemap";
import { pageMetadata } from "@/features/landing/utils/page-metadata";

export async function generateMetadata(): Promise<Metadata> {
  return liveMetadata("/pricing", await pageMetadata("pricing", "/pricing"));
}

export default function Page() {
  requireLiveRoute("/pricing");
  return <PricingPage />;
}
