import type { Metadata } from "next";
import { fetchLatestRelease } from "@/features/landing/utils/github-release";
import { WEB_BRAND_NAME } from "@/lib/brand";
import { DownloadClient } from "./download-client";

// Vercel ISR: the server fetch inside fetchLatestRelease carries
// `next: { revalidate: 300 }`, which makes GitHub API cost at most
// one request per region per 5 minutes. Page-level revalidate mirrors
// that window so the first paint also refreshes every 5 minutes.
export const revalidate = 300;

export const metadata: Metadata = {
  title: `Download Desktop for ${WEB_BRAND_NAME}`,
  description:
    `${WEB_BRAND_NAME} currently uses the Multica Desktop client and CLI. Download for macOS, Windows, or Linux.`,
  openGraph: {
    title: `Download Desktop for ${WEB_BRAND_NAME}`,
    description:
      `${WEB_BRAND_NAME} currently uses the Multica Desktop client with a bundled daemon, plus the Multica CLI for servers and remote dev boxes.`,
    url: "/download",
  },
  alternates: {
    canonical: "/download",
  },
};

export default async function DownloadPage() {
  const release = await fetchLatestRelease();
  return <DownloadClient release={release} />;
}
