import type { Metadata } from "next";
import { HomePage } from "@/features/landing/site/home-page";

export const metadata: Metadata = {
  title: "Homepage",
  description:
    "Multica — source-available platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills.",
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
  return <HomePage />;
}
