import type { Metadata } from "next";
import { pageMetadata } from "@/features/landing/utils/page-metadata";
import { HomePage } from "@/features/landing/site/home-page";
import { getHomeDownload } from "@/features/landing/utils/home-download";
import { RedirectIfAuthenticated } from "@/features/landing/components/redirect-if-authenticated";

export function generateMetadata(): Promise<Metadata> {
  return pageMetadata("home", "/");
}

export default async function LandingPage() {
  return (
    <>
      <RedirectIfAuthenticated />
      <HomePage download={await getHomeDownload()} />
    </>
  );
}
