import type { Metadata } from "next";
import { ChangelogPageClient } from "@/features/landing/components/changelog-page-client";
import { WEB_BRAND_NAME } from "@/lib/brand";

export const metadata: Metadata = {
  title: "Changelog",
  description:
    `See what's new in ${WEB_BRAND_NAME} — latest features, improvements, and fixes.`,
  openGraph: {
    title: `Changelog | ${WEB_BRAND_NAME}`,
    description: `Latest updates and releases from ${WEB_BRAND_NAME}.`,
    url: "/changelog",
  },
  alternates: {
    canonical: "/changelog",
  },
};

export default function ChangelogPage() {
  return <ChangelogPageClient />;
}
