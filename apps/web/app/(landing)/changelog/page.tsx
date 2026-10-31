import type { Metadata } from "next";
import { ChangelogPageClient } from "@/features/landing/components/changelog-page-client";
import { SHARE_IMAGE } from "@/features/landing/share-image";

export const metadata: Metadata = {
  title: "Changelog",
  description:
    "See what's new in Multica — latest features, improvements, and fixes.",
  openGraph: {
    images: [SHARE_IMAGE],
    title: "Changelog | Multica",
    description: "Latest updates and releases from Multica.",
    url: "/changelog",
  },
  alternates: {
    canonical: "/changelog",
  },
};

export default function ChangelogPage() {
  return <ChangelogPageClient />;
}
