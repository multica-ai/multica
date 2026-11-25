import type { Metadata } from "next";
import { PlaceholderPage } from "@/features/landing/site/site-page";
import { PLACEHOLDER_PAGES, liveMetadata, requireLiveRoute } from "@/features/landing/site/sitemap";

const page = PLACEHOLDER_PAGES.press;

export const metadata: Metadata = liveMetadata("/press", {
  title: page.title,
  description: page.summary,
  alternates: { canonical: "/press" },
  robots: { index: false, follow: true },
});

export default function Page() {
  requireLiveRoute("/press");
  return <PlaceholderPage eyebrow="Multica" page={page} />;
}
