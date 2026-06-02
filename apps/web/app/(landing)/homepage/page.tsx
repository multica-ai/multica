import type { Metadata } from "next";
import { WalltsLanding } from "@/features/landing/components/wallts-landing";

export const metadata: Metadata = {
  title: "Homepage",
  description:
    "Wallts — open-source platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills.",
  openGraph: {
    title: "Wallts — Project Management for Human + Agent Teams",
    description:
      "Manage your human + agent workforce in one place.",
    url: "/homepage",
  },
  alternates: {
    canonical: "/homepage",
  },
};

export default function HomepagePage() {
  return <WalltsLanding />;
}
