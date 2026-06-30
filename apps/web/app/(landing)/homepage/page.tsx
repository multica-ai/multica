import type { Metadata } from "next";
import { MulticaLanding } from "@/features/landing/components/multica-landing";
import { WEB_BRAND_NAME } from "@/lib/brand";

export const metadata: Metadata = {
  title: "Homepage",
  description:
    `${WEB_BRAND_NAME} — open-source platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills.`,
  openGraph: {
    title: `${WEB_BRAND_NAME} — Project Management for Human + Agent Teams`,
    description:
      "Manage your human + agent workforce in one place.",
    url: "/homepage",
  },
  alternates: {
    canonical: "/homepage",
  },
};

export default function HomepagePage() {
  return <MulticaLanding />;
}
