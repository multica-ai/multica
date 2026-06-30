import type { Metadata } from "next";
import { AboutPageClient } from "@/features/landing/components/about-page-client";
import { WEB_BRAND_NAME } from "@/lib/brand";

export const metadata: Metadata = {
  title: "About",
  description:
    `Learn about ${WEB_BRAND_NAME} — multiplexed information and computing agent. An open-source project management platform for human + agent teams.`,
  openGraph: {
    title: `About ${WEB_BRAND_NAME}`,
    description:
      `The story behind ${WEB_BRAND_NAME} and why we're building project management for human + agent teams.`,
    url: "/about",
  },
  alternates: {
    canonical: "/about",
  },
};

export default function AboutPage() {
  return <AboutPageClient />;
}
