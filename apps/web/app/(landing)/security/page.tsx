import type { Metadata } from "next";
import { PlaceholderPage } from "@/features/landing/site/site-page";
import { PLACEHOLDER_PAGES, liveMetadata, requireLiveRoute } from "@/features/landing/site/sitemap";

const page = PLACEHOLDER_PAGES.security;

export const metadata: Metadata = liveMetadata("/security", {
  title: page.title,
  description: page.summary,
  alternates: { canonical: "/security" },
  robots: { index: false, follow: true },
});

export default function Page() {
  requireLiveRoute("/security");
  return <PlaceholderPage eyebrow="Multica" page={page} />;
}
