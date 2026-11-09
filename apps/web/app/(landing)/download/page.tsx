import type { Metadata } from "next";
import { fetchLatestRelease } from "@/features/landing/utils/github-release";
import { getRequestPlatform } from "@/features/landing/utils/request-platform";
import { isDownloadKey } from "@/features/landing/utils/download-target";
import { DownloadClient } from "./download-client";
import { SHARE_IMAGE } from "@/features/landing/share-image";

export const metadata: Metadata = {
  title: "Download Multica",
  description:
    "Download Multica for macOS, Windows, or Linux — or install the CLI for servers and remote dev boxes.",
  openGraph: {
    images: [SHARE_IMAGE],
    title: "Download Multica",
    description:
      "Get the Multica desktop app with a bundled daemon, or install the CLI for servers and remote dev boxes.",
    url: "/download",
  },
  alternates: {
    canonical: "/download",
  },
};

// The page renders per request so its first paint names the visitor's
// platform. The release itself comes from the fetch cache, refreshed at
// most every 5 minutes (see fetchLatestRelease).
export default async function DownloadPage({
  searchParams,
}: {
  searchParams: Promise<{ started?: string | string[] }>;
}) {
  const [release, platform, params] = await Promise.all([fetchLatestRelease(), getRequestPlatform(), searchParams]);
  const started = isDownloadKey(params.started) ? params.started : null;
  return <DownloadClient release={release} platform={platform} started={started} />;
}
