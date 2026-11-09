import type { Metadata } from "next";
import { HomePage } from "@/features/landing/site/home-page";
import { getHomeDownload } from "@/features/landing/utils/home-download";
import { RedirectIfAuthenticated } from "@/features/landing/components/redirect-if-authenticated";
import { SHARE_IMAGE } from "@/features/landing/share-image";

export const metadata: Metadata = {
  title: {
    absolute: "Multica — Project Management for Human + Agent Teams",
  },
  description:
    "Source-available platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills.",
  openGraph: {
    images: [SHARE_IMAGE],
    title: "Multica — Project Management for Human + Agent Teams",
    description:
      "Manage your human + agent workforce in one place.",
    url: "/",
  },
  alternates: {
    canonical: "/",
  },
};

export default async function LandingPage() {
  return (
    <>
      <RedirectIfAuthenticated />
      <HomePage download={await getHomeDownload()} />
    </>
  );
}
