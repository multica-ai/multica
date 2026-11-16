import type { Metadata } from "next";
import { pageMetadata } from "@/features/landing/utils/page-metadata";
import { HomePage } from "@/features/landing/site/home-page";
import { getHomeDownload } from "@/features/landing/utils/home-download";

export function generateMetadata(): Promise<Metadata> {
  return pageMetadata("home", "/homepage");
}

export default async function HomepagePage() {
  return <HomePage download={await getHomeDownload()} />;
}
