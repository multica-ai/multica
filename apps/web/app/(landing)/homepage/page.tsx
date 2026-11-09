import type { Metadata } from "next";
import { HomePage } from "@/features/landing/site/home-page";
import { getHomeDownload } from "@/features/landing/utils/home-download";
import { SHARE_IMAGE } from "@/features/landing/share-image";

export const metadata: Metadata = {
  title: "Homepage",
  description:
    "Multica — source-available platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills.",
  openGraph: {
    images: [SHARE_IMAGE],
    title: "Multica — Project Management for Human + Agent Teams",
    description:
      "Manage your human + agent workforce in one place.",
    url: "/homepage",
  },
  alternates: {
    canonical: "/homepage",
  },
};

export default async function HomepagePage() {
  return <HomePage download={await getHomeDownload()} />;
}
