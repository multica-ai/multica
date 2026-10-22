import type { Metadata } from "next";
import { PlaceholderPage } from "@/features/landing/site/site-page";
import { PLACEHOLDER_PAGES } from "@/features/landing/site/sitemap";

const page = PLACEHOLDER_PAGES.security;

export const metadata: Metadata = {
  title: page.title,
  description: page.summary,
  alternates: { canonical: "/security" },
  robots: { index: false, follow: true },
};

export default function Page() {
  return <PlaceholderPage eyebrow="Multica" page={page} />;
}
