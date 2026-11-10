import type { Metadata } from "next";
import { PlaceholderPage } from "@/features/landing/site/site-page";
import { PLACEHOLDER_PAGES, liveMetadata, requireLiveRoute } from "@/features/landing/site/sitemap";

const page = PLACEHOLDER_PAGES.customers;

export const metadata: Metadata = liveMetadata("/customers", {
  title: page.title,
  description: page.summary,
  alternates: { canonical: "/customers" },
  robots: { index: false, follow: true },
});

export default function Page() {
  requireLiveRoute("/customers");
  return <PlaceholderPage eyebrow="Multica" page={page} />;
}
