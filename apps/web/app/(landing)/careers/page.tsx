import type { Metadata } from "next";
import { PlaceholderPage } from "@/features/landing/site/site-page";
import { PLACEHOLDER_PAGES, liveMetadata, requireLiveRoute } from "@/features/landing/site/sitemap";

const page = PLACEHOLDER_PAGES.careers;

export const metadata: Metadata = liveMetadata("/careers", {
  title: page.title,
  description: page.summary,
  alternates: { canonical: "/careers" },
  robots: { index: false, follow: true },
});

export default function Page() {
  requireLiveRoute("/careers");
  return <PlaceholderPage eyebrow="Multica" page={page} />;
}
