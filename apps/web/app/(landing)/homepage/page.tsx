import type { Metadata } from "next";
import { MulticaLanding } from "@/features/landing/components/multica-landing";

export const metadata: Metadata = {
  title: "Homepage",
  description:
    "Multica — open-source platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills.",
  openGraph: {
    title: "Multica — Project Management for Human + Agent Teams",
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
